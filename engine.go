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
	def, err := engine.store.GetWorkflowDefinition(ctx, workflowID)
	if err != nil {
		return 0, fmt.Errorf("get workflow definition: %w", err)
	}

	instance, err := engine.store.CreateInstance(ctx, workflowID, input)
	if err != nil {
		return 0, fmt.Errorf("create instance: %w", err)
	}

	_ = engine.store.LogEvent(ctx, instance.ID, nil, "workflow_started", map[string]any{
		"workflow_id": workflowID,
	})

	if err := engine.store.UpdateInstanceStatus(ctx, instance.ID, StatusRunning, nil, nil); err != nil {
		return 0, fmt.Errorf("update status: %w", err)
	}

	startStep := def.Definition.Start
	if startStep == "" {
		return 0, errors.New("no start step defined")
	}

	if err := engine.enqueueNextSteps(ctx, instance.ID, []string{startStep}, input); err != nil {
		return 0, fmt.Errorf("enqueue start step: %w", err)
	}

	return instance.ID, nil
}

func (engine *Engine) ExecuteNext(ctx context.Context, workerID string) error {
	return engine.txManager.ReadCommitted(ctx, func(ctx context.Context) error {
		item, err := engine.store.DequeueStep(ctx, workerID)
		if err != nil {
			return fmt.Errorf("dequeue step: %w", err)
		}

		if item == nil {
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

	_ = engine.store.LogEvent(ctx, instance.ID, &step.ID, "step_started", map[string]any{
		"step_name": step.StepName,
		"step_type": stepDef.Type,
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
		variables:  make(map[string]any),
	}

	return handler.Execute(ctx, execCtx, step.Input)
}

func (engine *Engine) executeFork(
	ctx context.Context,
	instance *WorkflowInstance,
	step *WorkflowStep,
	stepDef *StepDefinition,
) (json.RawMessage, error) {
	_ = engine.store.LogEvent(ctx, instance.ID, &step.ID, "fork_started", map[string]any{
		"parallel_steps": stepDef.Parallel,
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

			_ = engine.store.LogEvent(ctx, instance.ID, &step.ID, "join_state_created", map[string]any{
				"join_step":   nextStepName,
				"waiting_for": waitFor,
				"strategy":    strategy,
			})
		}
	}

	return json.Marshal(map[string]any{
		"status":         "forked",
		"parallel_steps": stepDef.Parallel,
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

	_ = engine.store.LogEvent(ctx, instance.ID, &step.ID, "join_check", map[string]any{
		"waiting_for": joinState.WaitingFor,
		"completed":   joinState.Completed,
		"failed":      joinState.Failed,
		"is_ready":    joinState.IsReady,
	})

	if !joinState.IsReady {
		return nil, fmt.Errorf("join not ready: waiting for %v", joinState.WaitingFor)
	}

	results := make(map[string]any)
	results["completed"] = joinState.Completed
	results["failed"] = joinState.Failed
	results["strategy"] = joinState.JoinStrategy

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
	results["outputs"] = outputs

	if len(joinState.Failed) > 0 && joinState.JoinStrategy == JoinStrategyAll {
		results["status"] = "failed"
		failedData, _ := json.Marshal(results)

		return failedData, fmt.Errorf("join failed: %d steps failed", len(joinState.Failed))
	}

	results["status"] = "success"

	_ = engine.store.LogEvent(ctx, instance.ID, &step.ID, "join_completed", results)

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

	_ = engine.store.LogEvent(ctx, instance.ID, &step.ID, "step_completed", map[string]any{
		"step_name": step.StepName,
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

	if step.RetryCount < step.MaxRetries {
		if err := engine.store.UpdateStep(ctx, step.ID, StepStatusFailed, nil, &errMsg); err != nil {
			return fmt.Errorf("update step: %w", err)
		}

		_ = engine.store.LogEvent(ctx, instance.ID, &step.ID, "step_retry", map[string]any{
			"step_name":   step.StepName,
			"retry_count": step.RetryCount + 1,
			"error":       errMsg,
		})

		return engine.store.EnqueueStep(ctx, instance.ID, &step.ID, 0, 0)
	}

	if err := engine.store.UpdateStep(ctx, step.ID, StepStatusFailed, nil, &errMsg); err != nil {
		return fmt.Errorf("update step: %w", err)
	}

	_ = engine.store.LogEvent(ctx, instance.ID, &step.ID, "step_failed", map[string]any{
		"step_name": step.StepName,
		"error":     errMsg,
	})

	if err := engine.notifyJoinSteps(ctx, instance.ID, step.StepName, false); err != nil {
		return fmt.Errorf("notify join steps: %w", err)
	}

	if stepDef.OnFailure != "" {
		return engine.enqueueNextSteps(ctx, instance.ID, []string{stepDef.OnFailure}, step.Input)
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

		_ = engine.store.LogEvent(ctx, instanceID, nil, "join_updated", map[string]any{
			"join_step":      stepName,
			"completed_step": completedStepName,
			"success":        success,
			"is_ready":       isReady,
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

				_ = engine.store.LogEvent(ctx, instanceID, &joinStep.ID, "join_ready", map[string]any{
					"join_step": stepName,
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

	_ = engine.store.LogEvent(ctx, instance.ID, nil, "workflow_completed", map[string]any{
		"workflow_id": instance.WorkflowID,
	})

	return nil
}

func (engine *Engine) GetStatus(ctx context.Context, instanceID int64) (*WorkflowInstance, error) {
	return engine.store.GetInstance(ctx, instanceID)
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
