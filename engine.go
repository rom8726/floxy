package floxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
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

			for _, currStep := range steps {
				currStep := currStep
				if currStep.ID == *item.StepID {
					step = &currStep

					break
				}
			}

			if step == nil {
				return fmt.Errorf("step not found: %d", *item.StepID)
			}
		}

		// Check if this is a compensation
		if step.Status == StepStatusCompensation {
			return engine.executeCompensationStep(ctx, instance, step)
		}

		return engine.executeStep(ctx, instance, step)
	})
	if err != nil {
		return empty, err
	}

	return empty, nil
}

func (engine *Engine) MakeHumanDecision(
	ctx context.Context,
	stepID int64,
	decidedBy string,
	decision HumanDecision,
	comment *string,
) error {
	return engine.txManager.ReadCommitted(ctx, func(ctx context.Context) error {
		step, err := engine.store.GetStepByID(ctx, stepID)
		if err != nil {
			return fmt.Errorf("get step: %w", err)
		}

		if step.StepType != StepTypeHuman {
			return fmt.Errorf("step %d is not a human step", stepID)
		}

		if step.Status != StepStatusWaitingDecision {
			return fmt.Errorf("step %d is not waiting for decision (current status: %s)", stepID, step.Status)
		}

		decisionRecord := &HumanDecisionRecord{
			InstanceID: step.InstanceID,
			StepID:     stepID,
			DecidedBy:  decidedBy,
			Decision:   decision,
			Comment:    comment,
			DecidedAt:  time.Now(),
		}

		if err := engine.store.CreateHumanDecision(ctx, decisionRecord); err != nil {
			return fmt.Errorf("create human decision: %w", err)
		}

		var newStatus StepStatus
		switch decision {
		case HumanDecisionConfirmed:
			newStatus = StepStatusConfirmed
		case HumanDecisionRejected:
			newStatus = StepStatusRejected
		}

		if err := engine.store.UpdateStepStatus(ctx, stepID, newStatus); err != nil {
			return fmt.Errorf("update step status: %w", err)
		}

		_ = engine.store.LogEvent(ctx, step.InstanceID, &stepID, EventStepCompleted, map[string]any{
			KeyStepName:  step.StepName,
			KeyDecision:  decision,
			KeyDecidedBy: decidedBy,
		})

		// If decision is confirmed, continue workflow execution
		if decision == HumanDecisionConfirmed {
			// Set workflow to running status if it was paused
			instance, err := engine.store.GetInstance(ctx, step.InstanceID)
			if err != nil {
				return fmt.Errorf("get instance: %w", err)
			}

			if instance.Status == StatusPending {
				if err := engine.store.UpdateInstanceStatus(ctx, step.InstanceID, StatusRunning, nil, nil); err != nil {
					return fmt.Errorf("update instance status: %w", err)
				}
			}

			// Continue execution of next steps
			return engine.continueWorkflowAfterHumanDecision(ctx, instance, step)
		} else {
			// If decision is rejected, stop workflow
			return engine.store.UpdateInstanceStatus(ctx, step.InstanceID, StatusAborted, nil, nil)
		}
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

	if stepDef.Timeout != 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, stepDef.Timeout)
		defer cancel()
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
	next := true

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
	case StepTypeCondition:
		output, next, stepErr = engine.executeCondition(ctx, instance, step, stepDef)
	case StepTypeHuman:
		var aborted bool
		output, aborted, stepErr = engine.executeHuman(ctx, instance, step, stepDef)
		if stepErr == nil && aborted {
			return nil
		}
	default:
		stepErr = fmt.Errorf("unsupported step type: %s", stepDef.Type)
	}

	if stepErr != nil {
		return engine.handleStepFailure(ctx, instance, step, stepDef, stepErr)
	}

	return engine.handleStepSuccess(ctx, instance, step, stepDef, output, next)
}

func (engine *Engine) executeCompensationStep(ctx context.Context, instance *WorkflowInstance, step *WorkflowStep) error {
	def, err := engine.store.GetWorkflowDefinition(ctx, instance.WorkflowID)
	if err != nil {
		return fmt.Errorf("get workflow definition: %w", err)
	}

	stepDef, ok := def.Definition.Steps[step.StepName]
	if !ok {
		return fmt.Errorf("step definition not found: %s", step.StepName)
	}

	if stepDef.Timeout != 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, stepDef.Timeout)
		defer cancel()
	}

	onFailureStep, ok := def.Definition.Steps[stepDef.OnFailure]
	if !ok {
		// No compensation handler, mark as rolled back
		if err := engine.store.UpdateStep(ctx, step.ID, StepStatusRolledBack, step.Input, nil); err != nil {
			return fmt.Errorf("update step status: %w", err)
		}
		return nil
	}

	handler, exists := engine.handlers[onFailureStep.Handler]
	if !exists {
		// Handler not found, mark as rolled back
		if err := engine.store.UpdateStep(ctx, step.ID, StepStatusRolledBack, step.Input, nil); err != nil {
			return fmt.Errorf("update step status: %w", err)
		}
		return nil
	}

	variables := make(map[string]any, len(onFailureStep.Metadata)+1)
	for k, v := range onFailureStep.Metadata {
		variables[k] = v
	}
	variables["reason"] = "compensation"

	stepCtx := executionContext{
		instanceID:     step.InstanceID,
		stepName:       step.StepName,
		idempotencyKey: step.IdempotencyKey,
		retryCount:     step.CompensationRetryCount,
		variables:      variables,
	}

	// Execute the compensation handler
	_, compensationErr := handler.Execute(ctx, &stepCtx, step.Input)
	if compensationErr != nil {
		// Compensation failed, check if we can retry
		if step.CompensationRetryCount < onFailureStep.MaxRetries {
			// Retry compensation
			newRetryCount := step.CompensationRetryCount + 1
			if err := engine.store.UpdateStepCompensationRetry(ctx, step.ID, newRetryCount, StepStatusCompensation); err != nil {
				return fmt.Errorf("update compensation retry: %w", err)
			}

			// Re-enqueue for retry
			if err := engine.store.EnqueueStep(ctx, step.InstanceID, &step.ID, 0, stepDef.Delay); err != nil {
				return fmt.Errorf("enqueue compensation retry: %w", err)
			}

			_ = engine.store.LogEvent(ctx, step.InstanceID, &step.ID, EventStepFailed, map[string]any{
				KeyStepName:   step.StepName,
				KeyStepType:   step.StepType,
				KeyError:      compensationErr.Error(),
				KeyRetryCount: newRetryCount,
				KeyReason:     "compensation_retry",
			})

			return nil
		} else {
			// Max retries exceeded, mark as failed
			errorMsg := compensationErr.Error()
			if err := engine.store.UpdateStep(ctx, step.ID, StepStatusFailed, step.Input, &errorMsg); err != nil {
				return fmt.Errorf("update step status: %w", err)
			}

			_ = engine.store.LogEvent(ctx, step.InstanceID, &step.ID, EventStepFailed, map[string]any{
				KeyStepName: step.StepName,
				KeyStepType: step.StepType,
				KeyError:    "compensation max retries exceeded",
			})

			return nil
		}
	}

	// Compensation successful, mark as rolled back
	if err := engine.store.UpdateStep(ctx, step.ID, StepStatusRolledBack, step.Input, nil); err != nil {
		return fmt.Errorf("update step status: %w", err)
	}

	_ = engine.store.LogEvent(ctx, step.InstanceID, &step.ID, EventStepCompleted, map[string]any{
		KeyStepName:   step.StepName,
		KeyStepType:   step.StepType,
		KeyRetryCount: step.CompensationRetryCount,
		KeyReason:     "compensation_success",
	})

	return nil
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
		instanceID:     instance.ID,
		stepName:       step.StepName,
		idempotencyKey: step.IdempotencyKey,
		retryCount:     step.RetryCount,
		variables:      stepDef.Metadata,
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

		if err := engine.store.EnqueueStep(ctx, instance.ID, &parallelStep.ID, 0, parallelStepDef.Delay); err != nil {
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

func (engine *Engine) executeCondition(
	ctx context.Context,
	instance *WorkflowInstance,
	step *WorkflowStep,
	stepDef *StepDefinition,
) (json.RawMessage, bool, error) {
	var inputData map[string]any
	_ = json.Unmarshal(step.Input, &inputData)

	stepCtx := executionContext{
		instanceID:     step.InstanceID,
		stepName:       step.StepName,
		idempotencyKey: step.IdempotencyKey,
		variables:      inputData,
	}

	result, err := evaluateCondition(stepDef.Condition, &stepCtx)
	if err != nil {
		_ = engine.store.LogEvent(ctx, instance.ID, &step.ID, EventConditionCheck, map[string]any{
			KeyStepName: step.StepName,
			KeyError:    err.Error(),
		})

		return nil, false, fmt.Errorf("evaluate condition: %w", err)
	}

	_ = engine.store.LogEvent(ctx, instance.ID, &step.ID, EventConditionCheck, map[string]any{
		KeyStepName: step.StepName,
		KeyResult:   result,
	})

	return step.Input, result, nil
}

func (engine *Engine) executeHuman(
	ctx context.Context,
	instance *WorkflowInstance,
	step *WorkflowStep,
	stepDef *StepDefinition,
) (json.RawMessage, bool, error) {
	// Check if there's already a decision for this step
	decision, err := engine.store.GetHumanDecision(ctx, step.ID)
	if err != nil && !errors.Is(err, ErrEntityNotFound) {
		return nil, false, fmt.Errorf("get human decision: %w", err)
	}

	if decision != nil {
		// Decision already made, process it
		return engine.processHumanDecision(ctx, instance, step, decision)
	}

	// No decision yet, set step to waiting state
	step.Status = StepStatusWaitingDecision
	if err := engine.store.UpdateStepStatus(ctx, step.ID, StepStatusWaitingDecision); err != nil {
		return nil, false, fmt.Errorf("update step status to waiting_decision: %w", err)
	}

	if err := engine.store.EnqueueStep(ctx, instance.ID, &step.ID, 1, stepDef.Delay); err != nil {
		return nil, false, fmt.Errorf("enqueue step: %w", err)
	}

	_ = engine.store.LogEvent(ctx, instance.ID, &step.ID, EventStepStarted, map[string]any{
		KeyStepName: step.StepName,
		KeyStepType: stepDef.Type,
		KeyReason:   "waiting_for_human_decision",
	})

	// Parse input data and add waiting status
	var inputData map[string]any
	if err := json.Unmarshal(step.Input, &inputData); err != nil {
		// If input is not valid JSON, create empty map
		inputData = make(map[string]any)
	}

	// Add waiting status to the data
	inputData["status"] = "waiting_decision"
	inputData["message"] = "Step is waiting for human decision"

	// Encode back to JSON
	output, err := json.Marshal(inputData)
	if err != nil {
		return nil, false, fmt.Errorf("marshal output: %w", err)
	}

	return output, false, nil
}

func (engine *Engine) processHumanDecision(
	ctx context.Context,
	instance *WorkflowInstance,
	step *WorkflowStep,
	decision *HumanDecisionRecord,
) (json.RawMessage, bool, error) {
	// Update step status based on decision
	var newStatus StepStatus

	switch decision.Decision {
	case HumanDecisionConfirmed:
		newStatus = StepStatusConfirmed

		_ = engine.store.LogEvent(ctx, instance.ID, &step.ID, EventStepCompleted, map[string]any{
			KeyStepName:  step.StepName,
			KeyDecision:  decision.Decision,
			KeyDecidedBy: decision.DecidedBy,
		})

	case HumanDecisionRejected:
		newStatus = StepStatusRejected

		errMsg := "Step was rejected by human"
		if err := engine.store.UpdateInstanceStatus(ctx, instance.ID, StatusAborted, nil, &errMsg); err != nil {
			return nil, false, fmt.Errorf("update instance status: %w", err)
		}

		_ = engine.store.LogEvent(ctx, instance.ID, &step.ID, EventStepFailed, map[string]any{
			KeyStepName:  step.StepName,
			KeyDecision:  decision.Decision,
			KeyDecidedBy: decision.DecidedBy,
		})
	}

	// Update step status
	if err := engine.store.UpdateStepStatus(ctx, step.ID, newStatus); err != nil {
		return nil, false, fmt.Errorf("update step status: %w", err)
	}

	// Parse input data and add decision information
	var inputData map[string]any
	if err := json.Unmarshal(step.Input, &inputData); err != nil {
		// If input is not valid JSON, create empty map
		inputData = make(map[string]any)
	}

	// Add decision information to the data
	inputData["status"] = string(decision.Decision)
	inputData["decided_by"] = decision.DecidedBy
	if decision.Comment != nil {
		inputData["comment"] = *decision.Comment
	}
	inputData["decided_at"] = decision.DecidedAt

	// Encode back to JSON
	output, err := json.Marshal(inputData)
	if err != nil {
		return nil, false, fmt.Errorf("marshal output: %w", err)
	}

	return output, newStatus == StepStatusRejected, nil
}

func (engine *Engine) continueWorkflowAfterHumanDecision(
	ctx context.Context,
	instance *WorkflowInstance,
	step *WorkflowStep,
) error {
	// Get workflow definition
	def, err := engine.store.GetWorkflowDefinition(ctx, instance.WorkflowID)
	if err != nil {
		return fmt.Errorf("get workflow definition: %w", err)
	}

	stepDef, ok := def.Definition.Steps[step.StepName]
	if !ok {
		return fmt.Errorf("step definition not found: %s", step.StepName)
	}

	// Continue execution of next steps
	output := json.RawMessage(`{"status": "confirmed"}`)
	return engine.handleStepSuccess(ctx, instance, step, stepDef, output, true)
}

func (engine *Engine) handleStepSuccess(
	ctx context.Context,
	instance *WorkflowInstance,
	step *WorkflowStep,
	stepDef *StepDefinition,
	output json.RawMessage,
	next bool,
) error {
	// For human steps waiting for decision, don't update status
	if stepDef.Type == StepTypeHuman && step.Status == StepStatusWaitingDecision {
		// Don't continue execution, wait for human decision
		return nil
	}

	if err := engine.store.UpdateStep(ctx, step.ID, StepStatusCompleted, output, nil); err != nil {
		return fmt.Errorf("update step: %w", err)
	}

	_ = engine.store.LogEvent(ctx, instance.ID, &step.ID, EventStepCompleted, map[string]any{
		KeyStepName: step.StepName,
	})

	if err := engine.notifyJoinSteps(ctx, instance.ID, step.StepName, true); err != nil {
		return fmt.Errorf("notify join steps: %w", err)
	}

	if (next && len(stepDef.Next) == 0) || (!next && stepDef.Else == "") {
		if !engine.hasUnfinishedSteps(ctx, instance.ID) {
			return engine.completeWorkflow(ctx, instance, output)
		}

		return nil
	}

	def, err := engine.store.GetWorkflowDefinition(ctx, instance.WorkflowID)
	if err != nil {
		return fmt.Errorf("get workflow definition: %w", err)
	}

	if next {
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
	} else {
		if err := engine.enqueueNextSteps(ctx, instance.ID, []string{stepDef.Else}, output); err != nil {
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

		if err := engine.store.UpdateStep(ctx, step.ID, StepStatusFailed, nil, &errMsg); err != nil {
			return fmt.Errorf("update step: %w", err)
		}

		_ = engine.store.LogEvent(ctx, instance.ID, &step.ID, EventStepRetry, map[string]any{
			KeyStepName:   step.StepName,
			KeyRetryCount: step.RetryCount + 1,
			KeyError:      errMsg,
		})

		return engine.store.EnqueueStep(ctx, instance.ID, &step.ID, 0, stepDef.Delay)
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
		if rollbackErr := engine.rollbackToSavePointOrRoot(ctx, instance.ID, step, def); rollbackErr != nil {
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

		// Additional check: don't consider join ready if there are still pending/running steps
		// in parallel branches that could affect the join result
		if isReady {
			hasPendingSteps := engine.hasPendingStepsInParallelBranches(ctx, instanceID, stepDef, steps)
			if hasPendingSteps {
				isReady = false
				// Update the join state to reflect that it's not ready
				_, _ = engine.store.UpdateJoinState(ctx, instanceID, stepName, completedStepName, success)
			}
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

		if err := engine.store.EnqueueStep(ctx, instanceID, &step.ID, 0, stepDef.Delay); err != nil {
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

func (engine *Engine) GetSteps(ctx context.Context, instanceID int64) ([]WorkflowStep, error) {
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

func (engine *Engine) rollbackToSavePointOrRoot(
	ctx context.Context,
	instanceID int64,
	failedStep *WorkflowStep,
	def *WorkflowDefinition,
) error {
	savePointName := engine.findNearestSavePoint(failedStep.StepName, def) // nearest save point or empty string (root)

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
		if stepDef.Prev == "" {
			for _, stepDefCurr := range def.Definition.Steps {
				if stepDefCurr.Prev != "" {
					stepDef = stepDefCurr

					break
				}
			}
		}

		if stepDef.Type == StepTypeSavePoint {
			return stepName
		}

		stepName = stepDef.Prev
	}

	return rootStepName
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
		step := step
		stepMap[step.StepName] = &step
	}
	if _, exists := stepMap[failedStep.StepName]; !exists {
		stepMap[failedStep.StepName] = failedStep
	}

	return engine.rollbackStepChain(ctx, failedStep.StepName, savePointName, def, stepMap, false)
}

func (engine *Engine) rollbackStepChain(
	ctx context.Context,
	currentStep, savePointName string,
	def *WorkflowDefinition,
	stepMap map[string]*WorkflowStep,
	isParallel bool,
) error {
	if currentStep == savePointName {
		return nil // Reached save point
	}

	stepDef, ok := def.Definition.Steps[currentStep]
	if !ok {
		return fmt.Errorf("step definition not found: %s", currentStep)
	}

	// First, traverse to the end of the chain (depth-first)
	// Handle parallel steps (fork branches)
	if stepDef.Type == StepTypeFork || stepDef.Type == StepTypeParallel {
		for _, parallelStepName := range stepDef.Parallel {
			if err := engine.rollbackStepChain(ctx, parallelStepName, savePointName, def, stepMap, true); err != nil {
				return err
			}
		}
	}

	// For parallel branches, traverse all subsequent steps in the chain
	if isParallel {
		// For condition steps, we need to determine which branch was executed
		if stepDef.Type == StepTypeCondition {
			// Check if the condition step was executed and determine which branch was taken
			if step, exists := stepMap[currentStep]; exists && step.Status == StepStatusCompleted {
				// Determine which branch was executed by checking which subsequent steps exist
				executedBranch := engine.determineExecutedBranch(stepDef, stepMap)

				if executedBranch == "next" {
					// Rollback the Next branch
					for _, nextStepName := range stepDef.Next {
						if err := engine.rollbackStepChain(ctx, nextStepName, savePointName, def, stepMap, true); err != nil {
							return err
						}
					}
				} else if executedBranch == "else" && stepDef.Else != "" {
					// Rollback the Else branch
					if err := engine.rollbackStepChain(ctx, stepDef.Else, savePointName, def, stepMap, true); err != nil {
						return err
					}
				}
				// If no branch was executed, skip rollback for this condition step
			}
		} else {
			// For non-condition steps, traverse all next steps
			for _, nextStepName := range stepDef.Next {
				if err := engine.rollbackStepChain(ctx, nextStepName, savePointName, def, stepMap, true); err != nil {
					return err
				}
			}
		}
	}

	// Continue with a previous step (traverse backwards)
	if stepDef.Prev != "" && !isParallel {
		if err := engine.rollbackStepChain(ctx, stepDef.Prev, savePointName, def, stepMap, isParallel); err != nil {
			return err
		}
	}

	// Now do the actual rollback (after traversing to the end)
	if step, exists := stepMap[currentStep]; exists &&
		(step.Status == StepStatusCompleted || step.Status == StepStatusFailed) {
		if err := engine.rollbackStep(ctx, step, def); err != nil {
			return fmt.Errorf("rollback step %s: %w", currentStep, err)
		}
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
		// No compensation handler, mark as rolled back directly
		if err := engine.store.UpdateStep(ctx, step.ID, StepStatusRolledBack, step.Input, nil); err != nil {
			return fmt.Errorf("update step status: %w", err)
		}

		return nil
	}

	// Check if we need to retry compensation
	if step.CompensationRetryCount >= onFailureStep.MaxRetries {
		// Max compensation retries exceeded, mark as failed
		if err := engine.store.UpdateStep(ctx, step.ID, StepStatusFailed, step.Input, nil); err != nil {
			return fmt.Errorf("update step status: %w", err)
		}
		_ = engine.store.LogEvent(ctx, step.InstanceID, &step.ID, EventStepFailed, map[string]any{
			KeyStepName: step.StepName,
			KeyStepType: step.StepType,
			KeyError:    "compensation max retries exceeded",
		})

		return nil
	}

	// Increment compensation retry count and update status to compensation
	newRetryCount := step.CompensationRetryCount + 1
	if err := engine.store.UpdateStepCompensationRetry(ctx, step.ID, newRetryCount, StepStatusCompensation); err != nil {
		return fmt.Errorf("update step compensation retry: %w", err)
	}

	// Enqueue compensation step for execution
	if err := engine.store.EnqueueStep(ctx, step.InstanceID, &step.ID, 0, stepDef.Delay); err != nil {
		return fmt.Errorf("enqueue compensation step: %w", err)
	}

	_ = engine.store.LogEvent(ctx, step.InstanceID, &step.ID, EventStepStarted, map[string]any{
		KeyStepName:   step.StepName,
		KeyStepType:   step.StepType,
		KeyRetryCount: newRetryCount,
		KeyReason:     "compensation",
	})

	return nil
}

func (engine *Engine) determineExecutedBranch(
	stepDef *StepDefinition,
	stepMap map[string]*WorkflowStep,
) string {
	// Check if any steps from the Next branch were executed
	for _, nextStepName := range stepDef.Next {
		if step, exists := stepMap[nextStepName]; exists &&
			(step.Status == StepStatusCompleted ||
				step.Status == StepStatusFailed ||
				step.Status == StepStatusCompensation) {
			return "next"
		}
	}

	// Check if any steps from the Else branch were executed
	if stepDef.Else != "" {
		if step, exists := stepMap[stepDef.Else]; exists &&
			(step.Status == StepStatusCompleted ||
				step.Status == StepStatusFailed ||
				step.Status == StepStatusCompensation) {
			return "else"
		}
	}

	// If no branch was executed, return an empty string
	return ""
}

// hasPendingStepsInParallelBranches checks if there are any pending/running steps
// in parallel branches that could affect the join result
func (engine *Engine) hasPendingStepsInParallelBranches(
	ctx context.Context,
	instanceID int64,
	joinStepDef *StepDefinition,
	allSteps []WorkflowStep,
) bool {
	// Get the fork step that created the parallel branches
	forkStepName := ""
	for _, waitFor := range joinStepDef.WaitFor {
		// Find the fork step that created this parallel branch
		// by looking for steps that have this step in their Parallel array
		for _, step := range allSteps {
			if step.StepName == waitFor {
				// This is a step from a parallel branch
				// We need to find the fork step that created this branch
				forkStepName = engine.findForkStepForParallelStep(ctx, instanceID, waitFor)

				break
			}
		}
		if forkStepName != "" {
			break
		}
	}

	if forkStepName == "" {
		return false
	}

	// Get workflow definition to find the fork step
	instance, err := engine.store.GetInstance(ctx, instanceID)
	if err != nil {
		return false
	}

	def, err := engine.store.GetWorkflowDefinition(ctx, instance.WorkflowID)
	if err != nil {
		return false
	}

	forkStepDef, ok := def.Definition.Steps[forkStepName]
	if !ok || forkStepDef.Type != StepTypeFork {
		return false
	}

	// Check if there are any pending/running steps in the parallel branches
	// that are not in the WaitFor list (i.e., dynamically created steps)
	for _, step := range allSteps {
		if step.Status == StepStatusPending || step.Status == StepStatusRunning {
			// Check if this step belongs to one of the parallel branches
			if engine.isStepInParallelBranch(step.StepName, forkStepDef, def) {
				// Check if this step is not in the WaitFor list
				isInWaitFor := false
				for _, waitFor := range joinStepDef.WaitFor {
					if step.StepName == waitFor {
						isInWaitFor = true

						break
					}
				}
				if !isInWaitFor {
					return true
				}
			}
		}
	}

	return false
}

// findForkStepForParallelStep finds the fork step that created the given parallel step
func (engine *Engine) findForkStepForParallelStep(ctx context.Context, instanceID int64, parallelStepName string) string {
	instance, err := engine.store.GetInstance(ctx, instanceID)
	if err != nil {
		return ""
	}

	def, err := engine.store.GetWorkflowDefinition(ctx, instance.WorkflowID)
	if err != nil {
		return ""
	}

	// Look for fork steps that have this step in their Parallel array
	for stepName, stepDef := range def.Definition.Steps {
		if stepDef.Type == StepTypeFork {
			for _, parallelStep := range stepDef.Parallel {
				if parallelStep == parallelStepName {
					return stepName
				}
			}
		}
	}

	return ""
}

// isStepInParallelBranch checks if a step belongs to one of the parallel branches
func (engine *Engine) isStepInParallelBranch(stepName string, forkStepDef *StepDefinition, def *WorkflowDefinition) bool {
	// Check if this step is a direct parallel step
	for _, parallelStep := range forkStepDef.Parallel {
		if stepName == parallelStep {
			return true
		}
	}

	// Check if this step is a descendant of any parallel step
	for _, parallelStep := range forkStepDef.Parallel {
		if engine.isStepDescendantOf(stepName, parallelStep, def) {
			return true
		}
	}

	return false
}

// isStepDescendantOf checks if a step is a descendant of another step
func (engine *Engine) isStepDescendantOf(stepName, ancestorStepName string, def *WorkflowDefinition) bool {
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

		if stepName == ancestorStepName {
			return true
		}

		// Check if this step is a descendant through Next or Else
		if stepDef.Prev != "" {
			stepName = stepDef.Prev
		} else {
			break
		}
	}

	return false
}
