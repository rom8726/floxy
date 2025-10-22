package floxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

type Engine struct {
	txManager TxManager
	store     Store
	handlers  map[string]StepHandler
	mu        sync.RWMutex
}

func NewEngine(txManager TxManager, store Store) *Engine {
	return &Engine{
		txManager: txManager,
		store:     store,
		handlers:  make(map[string]StepHandler),
	}
}

func (engine *Engine) RegisterHandler(handler StepHandler) {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	engine.handlers[handler.Name()] = handler
}

func (engine *Engine) RegisterWorkflow(ctx context.Context, def *WorkflowDefinition) error {
	if err := engine.validateDefinition(def); err != nil {
		return fmt.Errorf("invalid workflow definition: %w", err)
	}

	return engine.store.SaveWorkflowDefinition(ctx, def)
}

func (engine *Engine) Start(ctx context.Context, workflowID string, input json.RawMessage) (int64, error) {
	var instanceID int64

	err := engine.txManager.ReadCommitted(ctx, func(ctx context.Context) error {
		def, err := engine.store.GetWorkflowDefinition(ctx, workflowID)
		if err != nil {
			return fmt.Errorf("get workflow definition: %w", err)
		}

		instance, err := engine.store.CreateInstance(ctx, workflowID, input)
		if err != nil {
			return fmt.Errorf("create instance: %w", err)
		}

		_ = engine.store.LogEvent(ctx, instance.ID, nil, EventWorkflowStarted, map[string]any{
			KeyWorkflowID: workflowID,
		})

		if err := engine.store.UpdateInstanceStatus(ctx, instance.ID, StatusRunning, nil, nil); err != nil {
			return fmt.Errorf("update status: %w", err)
		}

		startStep := def.Definition.Start
		if startStep == "" {
			return errors.New("no start step defined")
		}

		if err := engine.enqueueNextSteps(ctx, instance.ID, []string{startStep}, input); err != nil {
			return fmt.Errorf("enqueue start step: %w", err)
		}

		instanceID = instance.ID

		return nil
	})
	if err != nil {
		return 0, err
	}

	return instanceID, nil
}

func (engine *Engine) ExecuteNext(ctx context.Context, workerID string) (empty bool, err error) {
	err = engine.txManager.ReadCommitted(ctx, func(ctx context.Context) error {
		item, err := engine.store.DequeueStep(ctx, workerID)
		if err != nil {
			return fmt.Errorf("dequeue step: %w", err)
		}

		if item == nil {
			empty = true

			return nil
		}

		defer func() {
			_ = engine.store.RemoveFromQueue(ctx, item.ID)
		}()

		instance, err := engine.store.GetInstance(ctx, item.InstanceID)
		if err != nil {
			return fmt.Errorf("get instance: %w", err)
		}

		var step *WorkflowStep
		if item.StepID == nil {
			step, err = engine.createFirstStep(ctx, instance)
			if err != nil {
				return fmt.Errorf("create first step: %w", err)
			}
		} else {
			steps, err := engine.store.GetStepsByInstance(ctx, instance.ID)
			if err != nil {
				return fmt.Errorf("get steps: %w", err)
			}

			for _, s := range steps {
				if s.ID == *item.StepID {
					step = s
					break
				}
			}

			if step == nil {
				return fmt.Errorf("step not found: %d", *item.StepID)
			}
		}

		return engine.executeStep(ctx, instance, step)
	})
	if err != nil {
		return empty, err
	}

	return empty, nil
}

func (engine *Engine) executeStep(ctx context.Context, instance *WorkflowInstance, step *WorkflowStep) error {
	def, err := engine.store.GetWorkflowDefinition(ctx, instance.WorkflowID)
	if err != nil {
		return fmt.Errorf("get workflow definition: %w", err)
	}

	stepDef, ok := def.Definition.Steps[step.StepName]
	if !ok {
		return fmt.Errorf("step definition not found: %s", step.StepName)
	}

	if err := engine.store.UpdateStep(ctx, step.ID, StepStatusRunning, nil, nil); err != nil {
		return fmt.Errorf("update step status: %w", err)
	}

	_ = engine.store.LogEvent(ctx, instance.ID, &step.ID, EventStepStarted, map[string]any{
		KeyStepName: step.StepName,
		KeyStepType: stepDef.Type,
	})

	var output json.RawMessage
	var stepErr error

	switch stepDef.Type {
	case StepTypeTask:
		output, stepErr = engine.executeTask(ctx, instance, step, stepDef)
	case StepTypeFork:
		output, stepErr = engine.executeFork(ctx, instance, step, stepDef)
	case StepTypeJoin:
		output, stepErr = engine.executeJoin(ctx, instance, step, stepDef)
	case StepTypeParallel:
		output, stepErr = engine.executeFork(ctx, instance, step, stepDef)
	case StepTypeSavePoint:
		output = step.Input
	default:
		stepErr = fmt.Errorf("unsupported step type: %s", stepDef.Type)
	}

	if stepErr != nil {
		return engine.handleStepFailure(ctx, instance, step, stepDef, stepErr)
	}

	return engine.handleStepSuccess(ctx, instance, step, stepDef, output)
}

func (engine *Engine) executeTask(
	ctx context.Context,
	instance *WorkflowInstance,
	step *WorkflowStep,
	stepDef *StepDefinition,
) (json.RawMessage, error) {
	engine.mu.RLock()
	handler, ok := engine.handlers[stepDef.Handler]
	engine.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("handler not found: %s", stepDef.Handler)
	}

	execCtx := &executionContext{
		instanceID: instance.ID,
		stepName:   step.StepName,
		retryCount: step.RetryCount,
		variables:  stepDef.Metadata,
	}

	return handler.Execute(ctx, execCtx, step.Input)
}

func (engine *Engine) executeFork(
	ctx context.Context,
	instance *WorkflowInstance,
	step *WorkflowStep,
	stepDef *StepDefinition,
) (json.RawMessage, error) {
	_ = engine.store.LogEvent(ctx, instance.ID, &step.ID, EventForkStarted, map[string]any{
		KeyParallelSteps: stepDef.Parallel,
	})

	def, err := engine.store.GetWorkflowDefinition(ctx, instance.WorkflowID)
	if err != nil {
		return nil, fmt.Errorf("get workflow definition: %w", err)
	}

	for _, parallelStepName := range stepDef.Parallel {
		parallelStepDef, ok := def.Definition.Steps[parallelStepName]
		if !ok {
			return nil, fmt.Errorf("parallel step definition not found: %s", parallelStepName)
		}

		parallelStep := &WorkflowStep{
			InstanceID: instance.ID,
			StepName:   parallelStepName,
			StepType:   parallelStepDef.Type,
			Status:     StepStatusPending,
			Input:      step.Input,
			MaxRetries: parallelStepDef.MaxRetries,
		}

		if err := engine.store.CreateStep(ctx, parallelStep); err != nil {
			return nil, fmt.Errorf("create fork step %s: %w", parallelStepName, err)
		}

		if err := engine.store.EnqueueStep(ctx, instance.ID, &parallelStep.ID, 0, 0); err != nil {
			return nil, fmt.Errorf("enqueue fork step %s: %w", parallelStepName, err)
		}
	}

	for _, nextStepName := range stepDef.Next {
		nextStepDef, ok := def.Definition.Steps[nextStepName]

		if ok && nextStepDef.Type == StepTypeJoin {
			strategy := nextStepDef.JoinStrategy
			if strategy == "" {
				strategy = JoinStrategyAll
			}

			waitFor := nextStepDef.WaitFor
			if len(waitFor) == 0 {
				waitFor = stepDef.Parallel
			}

			err := engine.store.CreateJoinState(ctx, instance.ID, nextStepName, waitFor, strategy)
			if err != nil {
				return nil, fmt.Errorf("create join state: %w", err)
			}

			_ = engine.store.LogEvent(ctx, instance.ID, &step.ID, EventJoinStateCreated, map[string]any{
				KeyJoinStep:   nextStepName,
				KeyWaitingFor: waitFor,
				KeyStrategy:   strategy,
			})
		}
	}

	return json.Marshal(map[string]any{
		KeyStatus:        "forked",
		KeyParallelSteps: stepDef.Parallel,
	})
}

func (engine *Engine) executeJoin(
	ctx context.Context,
	instance *WorkflowInstance,
	step *WorkflowStep,
	_ *StepDefinition,
) (json.RawMessage, error) {
	joinState, err := engine.store.GetJoinState(ctx, instance.ID, step.StepName)
	if err != nil {
		return nil, fmt.Errorf("get join state: %w", err)
	}

	_ = engine.store.LogEvent(ctx, instance.ID, &step.ID, EventJoinCheck, map[string]any{
		KeyWaitingFor: joinState.WaitingFor,
		KeyCompleted:  joinState.Completed,
		KeyFailed:     joinState.Failed,
		KeyIsReady:    joinState.IsReady,
	})

	if !joinState.IsReady {
		return nil, fmt.Errorf("join not ready: waiting for %v", joinState.WaitingFor)
	}

	results := make(map[string]any)
	results[KeyCompleted] = joinState.Completed
	results[KeyFailed] = joinState.Failed
	results[KeyStrategy] = joinState.JoinStrategy

	steps, err := engine.store.GetStepsByInstance(ctx, instance.ID)
	if err != nil {
		return nil, fmt.Errorf("get steps: %w", err)
	}

	outputs := make(map[string]json.RawMessage)
	for _, s := range steps {
		for _, waitFor := range joinState.WaitingFor {
			if s.StepName == waitFor && s.Status == StepStatusCompleted {
				outputs[s.StepName] = s.Output
			}
		}
	}
	results[KeyOutputs] = outputs

	if len(joinState.Failed) > 0 && joinState.JoinStrategy == JoinStrategyAll {
		results[KeyStatus] = "failed"
		failedData, _ := json.Marshal(results)

		return failedData, fmt.Errorf("join failed: %d steps failed", len(joinState.Failed))
	}

	results[KeyStatus] = "success"

	_ = engine.store.LogEvent(ctx, instance.ID, &step.ID, EventJoinCompleted, results)

	return json.Marshal(results)
}

func (engine *Engine) handleStepSuccess(
	ctx context.Context,
	instance *WorkflowInstance,
	step *WorkflowStep,
	stepDef *StepDefinition,
	output json.RawMessage,
) error {
	if err := engine.store.UpdateStep(ctx, step.ID, StepStatusCompleted, output, nil); err != nil {
		return fmt.Errorf("update step: %w", err)
	}

	_ = engine.store.LogEvent(ctx, instance.ID, &step.ID, EventStepCompleted, map[string]any{
		KeyStepName: step.StepName,
	})

	if err := engine.notifyJoinSteps(ctx, instance.ID, step.StepName, true); err != nil {
		return fmt.Errorf("notify join steps: %w", err)
	}

	if len(stepDef.Next) == 0 {
		if !engine.hasUnfinishedSteps(ctx, instance.ID) {
			return engine.completeWorkflow(ctx, instance, output)
		}

		return nil
	}

	def, err := engine.store.GetWorkflowDefinition(ctx, instance.WorkflowID)
	if err != nil {
		return fmt.Errorf("get workflow definition: %w", err)
	}

	for _, nextStepName := range stepDef.Next {
		nextStepDef, ok := def.Definition.Steps[nextStepName]
		if !ok {
			return fmt.Errorf("next step definition not found: %s", nextStepName)
		}

		if nextStepDef.Type == StepTypeJoin {
			continue
		}

		if err := engine.enqueueNextSteps(ctx, instance.ID, []string{nextStepName}, output); err != nil {
			return err
		}
	}

	return nil
}

func (engine *Engine) handleStepFailure(
	ctx context.Context,
	instance *WorkflowInstance,
	step *WorkflowStep,
	stepDef *StepDefinition,
	stepErr error,
) error {
	errMsg := stepErr.Error()

	if (step.RetryCount == 0 && stepDef.MaxRetries > 0) ||
		(step.RetryCount > 0 && step.RetryCount < step.MaxRetries && !stepDef.NoIdempotent) {
		step.RetryCount++

		if err := engine.store.UpdateStep(ctx, step.ID, StepStatusFailed, nil, &errMsg); err != nil {
			return fmt.Errorf("update step: %w", err)
		}

		_ = engine.store.LogEvent(ctx, instance.ID, &step.ID, EventStepRetry, map[string]any{
			KeyStepName:   step.StepName,
			KeyRetryCount: step.RetryCount,
			KeyError:      errMsg,
		})

		return engine.store.EnqueueStep(ctx, instance.ID, &step.ID, 0, 0)
	}

	if err := engine.store.UpdateStep(ctx, step.ID, StepStatusFailed, nil, &errMsg); err != nil {
		return fmt.Errorf("update step: %w", err)
	}

	_ = engine.store.LogEvent(ctx, instance.ID, &step.ID, EventStepFailed, map[string]any{
		KeyStepName: step.StepName,
		KeyError:    errMsg,
	})

	if err := engine.notifyJoinSteps(ctx, instance.ID, step.StepName, false); err != nil {
		return fmt.Errorf("notify join steps: %w", err)
	}

	// Try to rollback to save point before handling failure
	def, err := engine.store.GetWorkflowDefinition(ctx, instance.WorkflowID)
	if err == nil {
		if rollbackErr := engine.rollbackToSavePoint(ctx, instance.ID, step, def); rollbackErr != nil {
			// Log rollback error but continue with failure handling
			_ = engine.store.LogEvent(ctx, instance.ID, &step.ID, EventStepFailed, map[string]any{
				KeyStepName: step.StepName,
				KeyError:    fmt.Sprintf("rollback failed: %v", rollbackErr),
			})
		}
	}

	return engine.store.UpdateInstanceStatus(ctx, instance.ID, StatusFailed, nil, &errMsg)
}

func (engine *Engine) notifyJoinSteps(
	ctx context.Context,
	instanceID int64,
	completedStepName string,
	success bool,
) error {
	instance, err := engine.store.GetInstance(ctx, instanceID)
	if err != nil {
		return err
	}

	def, err := engine.store.GetWorkflowDefinition(ctx, instance.WorkflowID)
	if err != nil {
		return err
	}

	steps, err := engine.store.GetStepsByInstance(ctx, instanceID)
	if err != nil {
		return err
	}

	for stepName, stepDef := range def.Definition.Steps {
		if stepDef.Type != StepTypeJoin {
			continue
		}

		waitingForThis := false
		for _, waitFor := range stepDef.WaitFor {
			if waitFor == completedStepName {
				waitingForThis = true
				break
			}
		}

		if !waitingForThis {
			continue
		}

		isReady, err := engine.store.UpdateJoinState(ctx, instanceID, stepName, completedStepName, success)
		if err != nil {
			return fmt.Errorf("update join state for %s: %w", stepName, err)
		}

		_ = engine.store.LogEvent(ctx, instanceID, nil, EventJoinUpdated, map[string]any{
			KeyJoinStep:      stepName,
			KeyCompletedStep: completedStepName,
			KeySuccess:       success,
			KeyIsReady:       isReady,
		})

		if isReady {
			joinStepExists := false
			for _, s := range steps {
				if s.StepName == stepName {
					joinStepExists = true

					break
				}
			}

			if !joinStepExists {
				var joinInput json.RawMessage
				for _, s := range steps {
					if s.StepName == completedStepName {
						joinInput = s.Input

						break
					}
				}

				joinStep := &WorkflowStep{
					InstanceID: instanceID,
					StepName:   stepName,
					StepType:   StepTypeJoin,
					Status:     StepStatusPending,
					Input:      joinInput,
					MaxRetries: 0,
				}

				if err := engine.store.CreateStep(ctx, joinStep); err != nil {
					return fmt.Errorf("create join step: %w", err)
				}

				if err := engine.store.EnqueueStep(ctx, instanceID, &joinStep.ID, 0, 0); err != nil {
					return fmt.Errorf("enqueue join step: %w", err)
				}

				_ = engine.store.LogEvent(ctx, instanceID, &joinStep.ID, EventJoinReady, map[string]any{
					KeyJoinStep: stepName,
				})
			}
		}
	}

	return nil
}

func (engine *Engine) hasUnfinishedSteps(ctx context.Context, instanceID int64) bool {
	steps, err := engine.store.GetStepsByInstance(ctx, instanceID)
	if err != nil {
		return false
	}

	for _, step := range steps {
		if step.Status == StepStatusPending || step.Status == StepStatusRunning {
			return true
		}
	}

	return false
}

func (engine *Engine) enqueueNextSteps(
	ctx context.Context,
	instanceID int64,
	nextSteps []string,
	input json.RawMessage,
) error {
	instance, err := engine.store.GetInstance(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("get instance: %w", err)
	}

	def, err := engine.store.GetWorkflowDefinition(ctx, instance.WorkflowID)
	if err != nil {
		return fmt.Errorf("get workflow definition: %w", err)
	}

	for _, nextStepName := range nextSteps {
		stepDef, ok := def.Definition.Steps[nextStepName]
		if !ok {
			return fmt.Errorf("step definition not found: %s", nextStepName)
		}

		step := &WorkflowStep{
			InstanceID: instanceID,
			StepName:   nextStepName,
			StepType:   stepDef.Type,
			Status:     StepStatusPending,
			Input:      input,
			MaxRetries: stepDef.MaxRetries,
		}

		if err := engine.store.CreateStep(ctx, step); err != nil {
			return fmt.Errorf("create step: %w", err)
		}

		if err := engine.store.EnqueueStep(ctx, instanceID, &step.ID, 0, 0); err != nil {
			return fmt.Errorf("enqueue step: %w", err)
		}
	}

	return nil
}

func (engine *Engine) createFirstStep(ctx context.Context, instance *WorkflowInstance) (*WorkflowStep, error) {
	def, err := engine.store.GetWorkflowDefinition(ctx, instance.WorkflowID)
	if err != nil {
		return nil, err
	}

	startStepDef, ok := def.Definition.Steps[def.Definition.Start]
	if !ok {
		return nil, fmt.Errorf("start step definition not found: %s", def.Definition.Start)
	}

	step := &WorkflowStep{
		InstanceID: instance.ID,
		StepName:   def.Definition.Start,
		StepType:   startStepDef.Type,
		Status:     StepStatusPending,
		Input:      instance.Input,
		MaxRetries: startStepDef.MaxRetries,
	}

	if err := engine.store.CreateStep(ctx, step); err != nil {
		return nil, err
	}

	return step, nil
}

func (engine *Engine) completeWorkflow(ctx context.Context, instance *WorkflowInstance, output json.RawMessage) error {
	if err := engine.store.UpdateInstanceStatus(ctx, instance.ID, StatusCompleted, output, nil); err != nil {
		return fmt.Errorf("update instance status: %w", err)
	}

	_ = engine.store.LogEvent(ctx, instance.ID, nil, EventWorkflowCompleted, map[string]any{
		KeyWorkflowID: instance.WorkflowID,
	})

	return nil
}

func (engine *Engine) GetStatus(ctx context.Context, instanceID int64) (WorkflowStatus, error) {
	instance, err := engine.store.GetInstance(ctx, instanceID)
	if err != nil {
		return "", fmt.Errorf("get instance: %w", err)
	}

	return instance.Status, nil
}

func (engine *Engine) GetSteps(ctx context.Context, instanceID int64) ([]*WorkflowStep, error) {
	return engine.store.GetStepsByInstance(ctx, instanceID)
}

func (engine *Engine) validateDefinition(def *WorkflowDefinition) error {
	if def.Name == "" {
		return fmt.Errorf("workflow name is required")
	}

	if def.Definition.Start == "" {
		return fmt.Errorf("start step is required")
	}

	if len(def.Definition.Steps) == 0 {
		return fmt.Errorf("at least one step is required")
	}

	if _, ok := def.Definition.Steps[def.Definition.Start]; !ok {
		return fmt.Errorf("start step not found: %s", def.Definition.Start)
	}

	for stepName, stepDef := range def.Definition.Steps {
		for _, nextStep := range stepDef.Next {
			if _, ok := def.Definition.Steps[nextStep]; !ok {
				return fmt.Errorf("step %s references unknown step: %s", stepName, nextStep)
			}
		}

		if stepDef.OnFailure != "" {
			if _, ok := def.Definition.Steps[stepDef.OnFailure]; !ok {
				return fmt.Errorf("step %s references unknown compensation step: %s",
					stepName, stepDef.OnFailure)
			}
		}

		for _, parallelStep := range stepDef.Parallel {
			if _, ok := def.Definition.Steps[parallelStep]; !ok {
				return fmt.Errorf("step %s references unknown parallel step: %s", stepName, parallelStep)
			}
		}
	}

	return nil
}

func (engine *Engine) rollbackToSavePoint(
	ctx context.Context,
	instanceID int64,
	failedStep *WorkflowStep,
	def *WorkflowDefinition,
) error {
	savePointName := engine.findNearestSavePoint(failedStep.StepName, def)
	if savePointName == "" {
		return engine.rollbackAllSteps(ctx, instanceID, failedStep, def)
	}

	return engine.rollbackStepsToSavePoint(ctx, instanceID, failedStep, savePointName, def)
}

func (engine *Engine) findNearestSavePoint(stepName string, def *WorkflowDefinition) string {
	visited := make(map[string]bool)

	for stepName != "" {
		if visited[stepName] {
			break // Prevent infinite loops
		}
		visited[stepName] = true

		stepDef, ok := def.Definition.Steps[stepName]
		if !ok {
			break
		}

		if stepDef.Type == StepTypeSavePoint {
			return stepName
		}

		stepName = stepDef.Prev
	}

	return ""
}

func (engine *Engine) rollbackAllSteps(
	ctx context.Context,
	instanceID int64,
	failedStep *WorkflowStep,
	def *WorkflowDefinition,
) error {
	steps, err := engine.store.GetStepsByInstance(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("get steps by instance: %w", err)
	}

	for _, step := range steps {
		if step.Status == StepStatusCompleted || step.Status == StepStatusFailed {
			if err := engine.rollbackStep(ctx, step, def); err != nil {
				return fmt.Errorf("rollback step %s: %w", step.StepName, err)
			}
		}
	}

	return nil
}

func (engine *Engine) rollbackStepsToSavePoint(
	ctx context.Context,
	instanceID int64,
	failedStep *WorkflowStep,
	savePointName string,
	def *WorkflowDefinition,
) error {
	steps, err := engine.store.GetStepsByInstance(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("get steps by instance: %w", err)
	}

	stepMap := make(map[string]*WorkflowStep)
	for _, step := range steps {
		stepMap[step.StepName] = step
	}
	if _, exists := stepMap[failedStep.StepName]; !exists {
		stepMap[failedStep.StepName] = failedStep
	}

	return engine.rollbackStepChain(ctx, failedStep.StepName, savePointName, def, stepMap)
}

func (engine *Engine) rollbackStepChain(
	ctx context.Context,
	currentStep, savePointName string,
	def *WorkflowDefinition,
	stepMap map[string]*WorkflowStep,
) error {
	if currentStep == savePointName {
		return nil // Reached save point
	}

	stepDef, ok := def.Definition.Steps[currentStep]
	if !ok {
		return fmt.Errorf("step definition not found: %s", currentStep)
	}

	if step, exists := stepMap[currentStep]; exists &&
		(step.Status == StepStatusCompleted || step.Status == StepStatusFailed) {
		if err := engine.rollbackStep(ctx, step, def); err != nil {
			return fmt.Errorf("rollback step %s: %w", currentStep, err)
		}
	}

	// Handle parallel steps (fork branches)
	if stepDef.Type == StepTypeFork || stepDef.Type == StepTypeParallel {
		for _, parallelStepName := range stepDef.Parallel {
			if err := engine.rollbackStepChain(ctx, parallelStepName, savePointName, def, stepMap); err != nil {
				return err
			}
		}
	}

	// Continue with a previous step
	if stepDef.Prev != "" {
		return engine.rollbackStepChain(ctx, stepDef.Prev, savePointName, def, stepMap)
	}

	return nil
}

func (engine *Engine) rollbackStep(ctx context.Context, step *WorkflowStep, def *WorkflowDefinition) error {
	stepDef, ok := def.Definition.Steps[step.StepName]
	if !ok {
		return fmt.Errorf("step definition not found: %s", step.StepName)
	}

	onFailureStep, ok := def.Definition.Steps[stepDef.OnFailure]
	if !ok {
		return nil
	}

	handler, exists := engine.handlers[onFailureStep.Handler]
	if !exists {
		return nil
	}

	variables := make(map[string]string, len(onFailureStep.Metadata)+1)
	for k, v := range onFailureStep.Metadata {
		variables[k] = v
	}
	variables["reason"] = "error"

	stepCtx := &executionContext{
		instanceID: step.InstanceID,
		stepName:   step.StepName,
		retryCount: step.RetryCount,
		variables:  variables,
	}

	// Execute the handler in compensation mode
	_, err := handler.Execute(ctx, stepCtx, step.Input)
	if err != nil {
		return fmt.Errorf("execute compensation for step %q: %w", step.StepName, err)
	}

	// Update step status to rolled back
	if err := engine.store.UpdateStep(ctx, step.ID, StepStatusRolledBack, step.Input, nil); err != nil {
		return fmt.Errorf("update step status: %w", err)
	}

	_ = engine.store.LogEvent(ctx, step.InstanceID, &step.ID, EventStepFailed, map[string]any{
		KeyStepName: step.StepName,
		KeyStepType: step.StepType,
		KeyError:    "step rolled back",
	})

	return nil
}
