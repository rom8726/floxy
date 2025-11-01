package floxy

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestNewEngine(t *testing.T) {
	mockTxManager := NewMockTxManager(t)
	mockStore := NewMockStore(t)
	mockStore.EXPECT().GetCancelRequest(mock.Anything, mock.Anything).Return(nil, ErrEntityNotFound).Maybe()

	engine := NewEngine(nil, WithEngineTxManager(mockTxManager), WithEngineStore(mockStore))
	defer engine.Shutdown()

	assert.NotNil(t, engine)
	assert.Equal(t, mockTxManager, engine.txManager)
	assert.Equal(t, mockStore, engine.store)
	assert.NotNil(t, engine.handlers)
	assert.Empty(t, engine.handlers)
}

func TestEngine_RegisterHandler(t *testing.T) {
	mockTxManager := NewMockTxManager(t)
	mockStore := NewMockStore(t)
	mockStore.EXPECT().GetCancelRequest(mock.Anything, mock.Anything).Return(nil, ErrEntityNotFound).Maybe()

	engine := NewEngine(nil, WithEngineTxManager(mockTxManager), WithEngineStore(mockStore))
	defer engine.Shutdown()

	mockHandler := NewMockStepHandler(t)
	mockHandler.EXPECT().Name().Return("test-handler")

	engine.RegisterHandler(mockHandler)

	assert.Len(t, engine.handlers, 1)
	//assert.Equal(t, mockHandler, engine.handlers["test-handler"])
}

func TestEngine_RegisterWorkflow_ValidDefinition(t *testing.T) {
	mockTxManager := NewMockTxManager(t)
	mockStore := NewMockStore(t)
	mockStore.EXPECT().GetCancelRequest(mock.Anything, mock.Anything).Return(nil, ErrEntityNotFound).Maybe()

	engine := NewEngine(nil, WithEngineTxManager(mockTxManager), WithEngineStore(mockStore))
	defer engine.Shutdown()

	definition := &WorkflowDefinition{
		ID:   "test-workflow",
		Name: "Test Workflow",
		Definition: GraphDefinition{
			Start: "step1",
			Steps: map[string]*StepDefinition{
				"step1": {
					Name:       "step1",
					Type:       StepTypeTask,
					Handler:    "test-handler",
					MaxRetries: 3,
				},
			},
		},
	}

	mockStore.EXPECT().SaveWorkflowDefinition(mock.Anything, definition).Return(nil)

	err := engine.RegisterWorkflow(context.Background(), definition)

	assert.NoError(t, err)
}

func TestEngine_RegisterWorkflow_InvalidDefinition(t *testing.T) {
	mockTxManager := NewMockTxManager(t)
	mockStore := NewMockStore(t)
	mockStore.EXPECT().GetCancelRequest(mock.Anything, mock.Anything).Return(nil, ErrEntityNotFound).Maybe()

	engine := NewEngine(nil, WithEngineTxManager(mockTxManager), WithEngineStore(mockStore))
	defer engine.Shutdown()

	tests := []struct {
		name       string
		definition *WorkflowDefinition
		expectErr  string
	}{
		{
			name: "empty name",
			definition: &WorkflowDefinition{
				ID:   "test-workflow",
				Name: "",
				Definition: GraphDefinition{
					Start: "step1",
					Steps: map[string]*StepDefinition{
						"step1": {
							Name:       "step1",
							Type:       StepTypeTask,
							Handler:    "test-handler",
							MaxRetries: 3,
						},
					},
				},
			},
			expectErr: "workflow name is required",
		},
		{
			name: "empty start step",
			definition: &WorkflowDefinition{
				ID:   "test-workflow",
				Name: "Test Workflow",
				Definition: GraphDefinition{
					Start: "",
					Steps: map[string]*StepDefinition{
						"step1": {
							Name:       "step1",
							Type:       StepTypeTask,
							Handler:    "test-handler",
							MaxRetries: 3,
						},
					},
				},
			},
			expectErr: "start step is required",
		},
		{
			name: "no steps",
			definition: &WorkflowDefinition{
				ID:   "test-workflow",
				Name: "Test Workflow",
				Definition: GraphDefinition{
					Start: "step1",
					Steps: map[string]*StepDefinition{},
				},
			},
			expectErr: "at least one step is required",
		},
		{
			name: "start step not found",
			definition: &WorkflowDefinition{
				ID:   "test-workflow",
				Name: "Test Workflow",
				Definition: GraphDefinition{
					Start: "step1",
					Steps: map[string]*StepDefinition{
						"step2": {
							Name:       "step2",
							Type:       StepTypeTask,
							Handler:    "test-handler",
							MaxRetries: 3,
						},
					},
				},
			},
			expectErr: "start step not found: step1",
		},
		{
			name: "unknown next step",
			definition: &WorkflowDefinition{
				ID:   "test-workflow",
				Name: "Test Workflow",
				Definition: GraphDefinition{
					Start: "step1",
					Steps: map[string]*StepDefinition{
						"step1": {
							Name:       "step1",
							Type:       StepTypeTask,
							Handler:    "test-handler",
							MaxRetries: 3,
							Next:       []string{"unknown-step"},
						},
					},
				},
			},
			expectErr: "step step1 references unknown step: unknown-step",
		},
		{
			name: "unknown on_failure step",
			definition: &WorkflowDefinition{
				ID:   "test-workflow",
				Name: "Test Workflow",
				Definition: GraphDefinition{
					Start: "step1",
					Steps: map[string]*StepDefinition{
						"step1": {
							Name:       "step1",
							Type:       StepTypeTask,
							Handler:    "test-handler",
							MaxRetries: 3,
							OnFailure:  "unknown-step",
						},
					},
				},
			},
			expectErr: "step step1 references unknown compensation step: unknown-step",
		},
		{
			name: "unknown parallel step",
			definition: &WorkflowDefinition{
				ID:   "test-workflow",
				Name: "Test Workflow",
				Definition: GraphDefinition{
					Start: "step1",
					Steps: map[string]*StepDefinition{
						"step1": {
							Name:       "step1",
							Type:       StepTypeFork,
							Handler:    "test-handler",
							MaxRetries: 3,
							Parallel:   []string{"unknown-step"},
						},
					},
				},
			},
			expectErr: "step step1 references unknown parallel step: unknown-step",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := engine.RegisterWorkflow(context.Background(), tt.definition)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectErr)
		})
	}
}

func TestEngine_Start_Success(t *testing.T) {
	mockTxManager := NewMockTxManager(t)
	mockStore := NewMockStore(t)
	mockStore.EXPECT().GetCancelRequest(mock.Anything, mock.Anything).Return(nil, ErrEntityNotFound).Maybe()
	engine := NewEngine(nil, WithEngineTxManager(mockTxManager), WithEngineStore(mockStore))
	defer engine.Shutdown()

	workflowID := "test-workflow"
	input := json.RawMessage(`{"key": "value"}`)

	definition := &WorkflowDefinition{
		ID:   workflowID,
		Name: "Test Workflow",
		Definition: GraphDefinition{
			Start: "step1",
			Steps: map[string]*StepDefinition{
				"step1": {
					Name:       "step1",
					Type:       StepTypeTask,
					Handler:    "test-handler",
					MaxRetries: 3,
				},
			},
		},
	}

	instance := &WorkflowInstance{
		ID:         123,
		WorkflowID: workflowID,
		Status:     StatusPending,
		Input:      input,
	}

	mockTxManager.EXPECT().ReadCommitted(mock.Anything, mock.Anything).Run(func(ctx context.Context, fn func(ctx context.Context) error) {
		fn(ctx)
	}).Return(nil)

	mockStore.EXPECT().GetWorkflowDefinition(mock.Anything, workflowID).Return(definition, nil)
	mockStore.EXPECT().CreateInstance(mock.Anything, workflowID, input).Return(instance, nil)
	mockStore.EXPECT().LogEvent(mock.Anything, instance.ID, mock.Anything, EventWorkflowStarted, mock.Anything).Return(nil)
	mockStore.EXPECT().UpdateInstanceStatus(mock.Anything, instance.ID, StatusRunning, mock.Anything, mock.Anything).Return(nil)
	mockStore.EXPECT().GetInstance(mock.Anything, instance.ID).Return(instance, nil)
	mockStore.EXPECT().GetWorkflowDefinition(mock.Anything, instance.WorkflowID).Return(definition, nil)
	mockStore.EXPECT().CreateStep(mock.Anything, mock.MatchedBy(func(step *WorkflowStep) bool {
		return step.InstanceID == instance.ID && step.StepName == "step1"
	})).Return(nil)
	mockStore.EXPECT().EnqueueStep(mock.Anything, instance.ID, mock.Anything, PriorityNormal, mock.Anything).Return(nil)

	instanceID, err := engine.Start(context.Background(), workflowID, input)

	assert.NoError(t, err)
	assert.Equal(t, int64(123), instanceID)
}

func TestEngine_Start_GetDefinitionError(t *testing.T) {
	mockTxManager := NewMockTxManager(t)
	mockStore := NewMockStore(t)
	mockStore.EXPECT().GetCancelRequest(mock.Anything, mock.Anything).Return(nil, ErrEntityNotFound).Maybe()
	engine := NewEngine(nil, WithEngineTxManager(mockTxManager), WithEngineStore(mockStore))
	defer engine.Shutdown()

	workflowID := "test-workflow"
	input := json.RawMessage(`{"key": "value"}`)

	mockTxManager.EXPECT().ReadCommitted(mock.Anything, mock.Anything).Run(func(ctx context.Context, fn func(ctx context.Context) error) {
		fn(ctx)
	}).Return(errors.New("get workflow definition: definition not found"))

	mockStore.EXPECT().GetWorkflowDefinition(mock.Anything, workflowID).Return(nil, errors.New("definition not found"))

	instanceID, err := engine.Start(context.Background(), workflowID, input)

	assert.Error(t, err)
	assert.Equal(t, int64(0), instanceID)
	assert.Contains(t, err.Error(), "get workflow definition")
}

func TestEngine_Start_CreateInstanceError(t *testing.T) {
	mockTxManager := NewMockTxManager(t)
	mockStore := NewMockStore(t)
	mockStore.EXPECT().GetCancelRequest(mock.Anything, mock.Anything).Return(nil, ErrEntityNotFound).Maybe()
	engine := NewEngine(nil, WithEngineTxManager(mockTxManager), WithEngineStore(mockStore))
	defer engine.Shutdown()

	workflowID := "test-workflow"
	input := json.RawMessage(`{"key": "value"}`)

	definition := &WorkflowDefinition{
		ID:   workflowID,
		Name: "Test Workflow",
		Definition: GraphDefinition{
			Start: "step1",
			Steps: map[string]*StepDefinition{
				"step1": {
					Name:       "step1",
					Type:       StepTypeTask,
					Handler:    "test-handler",
					MaxRetries: 3,
				},
			},
		},
	}

	mockTxManager.EXPECT().ReadCommitted(mock.Anything, mock.Anything).Run(func(ctx context.Context, fn func(ctx context.Context) error) {
		fn(ctx)
	}).Return(errors.New("create instance: create instance failed"))

	mockStore.EXPECT().GetWorkflowDefinition(mock.Anything, workflowID).Return(definition, nil)
	mockStore.EXPECT().CreateInstance(mock.Anything, workflowID, input).Return(nil, errors.New("create instance failed"))

	instanceID, err := engine.Start(context.Background(), workflowID, input)

	assert.Error(t, err)
	assert.Equal(t, int64(0), instanceID)
	assert.Contains(t, err.Error(), "create instance")
}

func TestEngine_Start_NoStartStep(t *testing.T) {
	mockTxManager := NewMockTxManager(t)
	mockStore := NewMockStore(t)
	mockStore.EXPECT().GetCancelRequest(mock.Anything, mock.Anything).Return(nil, ErrEntityNotFound).Maybe()
	engine := NewEngine(nil, WithEngineTxManager(mockTxManager), WithEngineStore(mockStore))
	defer engine.Shutdown()

	workflowID := "test-workflow"
	input := json.RawMessage(`{"key": "value"}`)

	definition := &WorkflowDefinition{
		ID:   workflowID,
		Name: "Test Workflow",
		Definition: GraphDefinition{
			Start: "",
			Steps: map[string]*StepDefinition{
				"step1": {
					Name:       "step1",
					Type:       StepTypeTask,
					Handler:    "test-handler",
					MaxRetries: 3,
				},
			},
		},
	}

	instance := &WorkflowInstance{
		ID:         123,
		WorkflowID: workflowID,
		Status:     StatusPending,
		Input:      input,
	}

	mockTxManager.EXPECT().ReadCommitted(mock.Anything, mock.Anything).Run(func(ctx context.Context, fn func(ctx context.Context) error) {
		fn(ctx)
	}).Return(errors.New("no start step defined"))

	mockStore.EXPECT().GetWorkflowDefinition(mock.Anything, workflowID).Return(definition, nil)
	mockStore.EXPECT().CreateInstance(mock.Anything, workflowID, input).Return(instance, nil)
	mockStore.EXPECT().LogEvent(mock.Anything, instance.ID, mock.Anything, EventWorkflowStarted, mock.Anything).Return(nil)
	mockStore.EXPECT().UpdateInstanceStatus(mock.Anything, instance.ID, StatusRunning, mock.Anything, mock.Anything).Return(nil)

	instanceID, err := engine.Start(context.Background(), workflowID, input)

	assert.Error(t, err)
	assert.Equal(t, int64(0), instanceID)
	assert.Contains(t, err.Error(), "no start step defined")
}

func TestEngine_ExecuteNext_NoQueueItem(t *testing.T) {
	mockTxManager := NewMockTxManager(t)
	mockStore := NewMockStore(t)
	mockStore.EXPECT().GetCancelRequest(mock.Anything, mock.Anything).Return(nil, ErrEntityNotFound).Maybe()
	engine := NewEngine(nil, WithEngineTxManager(mockTxManager), WithEngineStore(mockStore))
	defer engine.Shutdown()

	workerID := "worker-1"

	mockTxManager.EXPECT().ReadCommitted(mock.Anything, mock.Anything).Run(func(ctx context.Context, fn func(ctx context.Context) error) {
		mockStore.EXPECT().DequeueStep(mock.Anything, workerID).Return(nil, nil)
		fn(ctx)
	}).Return(nil)

	empty, err := engine.ExecuteNext(context.Background(), workerID)

	assert.NoError(t, err)
	assert.True(t, empty)
}

func TestEngine_ExecuteNext_DequeueError(t *testing.T) {
	mockTxManager := NewMockTxManager(t)
	mockStore := NewMockStore(t)
	mockStore.EXPECT().GetCancelRequest(mock.Anything, mock.Anything).Return(nil, ErrEntityNotFound).Maybe()
	engine := NewEngine(nil, WithEngineTxManager(mockTxManager), WithEngineStore(mockStore))
	defer engine.Shutdown()

	workerID := "worker-1"

	mockTxManager.EXPECT().ReadCommitted(mock.Anything, mock.Anything).Run(func(ctx context.Context, fn func(ctx context.Context) error) {
		mockStore.EXPECT().DequeueStep(mock.Anything, workerID).Return(nil, errors.New("dequeue failed"))
		fn(ctx)
	}).Return(errors.New("dequeue step: dequeue failed"))

	empty, err := engine.ExecuteNext(context.Background(), workerID)

	assert.Error(t, err)
	assert.False(t, empty)
	assert.Contains(t, err.Error(), "dequeue step")
}

func TestEngine_ExecuteNext_StepNotFound(t *testing.T) {
	mockTxManager := NewMockTxManager(t)
	mockStore := NewMockStore(t)
	mockStore.EXPECT().GetCancelRequest(mock.Anything, mock.Anything).Return(nil, ErrEntityNotFound).Maybe()
	engine := NewEngine(nil, WithEngineTxManager(mockTxManager), WithEngineStore(mockStore))
	defer engine.Shutdown()

	workerID := "worker-1"
	instanceID := int64(123)
	stepID := int64(456)

	queueItem := &QueueItem{
		ID:         789,
		InstanceID: instanceID,
		StepID:     &stepID,
	}

	instance := &WorkflowInstance{
		ID:         instanceID,
		WorkflowID: "test-workflow",
		Status:     StatusRunning,
	}

	steps := []WorkflowStep{}

	mockTxManager.EXPECT().ReadCommitted(mock.Anything, mock.Anything).Run(func(ctx context.Context, fn func(ctx context.Context) error) {
		mockStore.EXPECT().DequeueStep(mock.Anything, workerID).Return(queueItem, nil)
		mockStore.EXPECT().RemoveFromQueue(mock.Anything, queueItem.ID).Return(nil)
		mockStore.EXPECT().GetInstance(mock.Anything, instanceID).Return(instance, nil)
		mockStore.EXPECT().GetStepsByInstance(mock.Anything, instanceID).Return(steps, nil)
		fn(ctx)
	}).Return(errors.New("step not found: 456"))

	empty, err := engine.ExecuteNext(context.Background(), workerID)

	assert.Error(t, err)
	assert.False(t, empty)
	assert.Contains(t, err.Error(), "step not found")
}

func TestEngine_ExecuteTask_Success(t *testing.T) {
	mockTxManager := NewMockTxManager(t)
	mockStore := NewMockStore(t)
	mockStore.EXPECT().GetCancelRequest(mock.Anything, mock.Anything).Return(nil, ErrEntityNotFound).Maybe()
	engine := NewEngine(nil, WithEngineTxManager(mockTxManager), WithEngineStore(mockStore))
	defer engine.Shutdown()

	instanceID := int64(123)
	stepID := int64(456)

	instance := &WorkflowInstance{
		ID:         instanceID,
		WorkflowID: "test-workflow",
		Status:     StatusRunning,
	}

	step := &WorkflowStep{
		ID:         stepID,
		InstanceID: instanceID,
		StepName:   "step1",
		StepType:   StepTypeTask,
		Status:     StepStatusPending,
		Input:      json.RawMessage(`{"key": "value"}`),
		MaxRetries: 3,
		RetryCount: 0,
	}

	stepDef := &StepDefinition{
		Name:       "step1",
		Type:       StepTypeTask,
		Handler:    "test-handler",
		MaxRetries: 3,
	}

	mockHandler := NewMockStepHandler(t)
	mockHandler.EXPECT().Name().Return("test-handler")
	engine.RegisterHandler(mockHandler)

	mockHandler.EXPECT().Execute(mock.Anything, mock.MatchedBy(func(ctx StepContext) bool {
		return ctx.InstanceID() == instanceID && ctx.StepName() == "step1" && ctx.RetryCount() == 0
	}), step.Input).Return(json.RawMessage(`{"result": "success"}`), nil)

	output, err := engine.executeTask(context.Background(), instance, step, stepDef)

	assert.NoError(t, err)
	assert.Equal(t, json.RawMessage(`{"result": "success"}`), output)
}

func TestEngine_ExecuteTask_HandlerNotFound(t *testing.T) {
	mockTxManager := NewMockTxManager(t)
	mockStore := NewMockStore(t)
	mockStore.EXPECT().GetCancelRequest(mock.Anything, mock.Anything).Return(nil, ErrEntityNotFound).Maybe()
	engine := NewEngine(nil, WithEngineTxManager(mockTxManager), WithEngineStore(mockStore))
	defer engine.Shutdown()

	instanceID := int64(123)
	stepID := int64(456)

	instance := &WorkflowInstance{
		ID:         instanceID,
		WorkflowID: "test-workflow",
		Status:     StatusRunning,
	}

	step := &WorkflowStep{
		ID:         stepID,
		InstanceID: instanceID,
		StepName:   "step1",
		StepType:   StepTypeTask,
		Status:     StepStatusPending,
		Input:      json.RawMessage(`{"key": "value"}`),
		MaxRetries: 3,
		RetryCount: 0,
	}

	stepDef := &StepDefinition{
		Name:       "step1",
		Type:       StepTypeTask,
		Handler:    "unknown-handler",
		MaxRetries: 3,
	}

	output, err := engine.executeTask(context.Background(), instance, step, stepDef)

	assert.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "handler not found")
}

func TestEngine_ExecuteSavePoint_Success(t *testing.T) {
	mockTxManager := NewMockTxManager(t)
	mockStore := NewMockStore(t)
	mockStore.EXPECT().GetCancelRequest(mock.Anything, mock.Anything).Return(nil, ErrEntityNotFound).Maybe()
	engine := NewEngine(nil, WithEngineTxManager(mockTxManager), WithEngineStore(mockStore))
	defer engine.Shutdown()

	instanceID := int64(123)
	stepID := int64(456)

	instance := &WorkflowInstance{
		ID:         instanceID,
		WorkflowID: "test-workflow",
		Status:     StatusRunning,
	}

	step := WorkflowStep{
		ID:         stepID,
		InstanceID: instanceID,
		StepName:   "savepoint1",
		StepType:   StepTypeSavePoint,
		Status:     StepStatusPending,
		Input:      json.RawMessage(`{"key": "value"}`),
		MaxRetries: 3,
		RetryCount: 0,
	}

	stepDef := &StepDefinition{
		Name:       "savepoint1",
		Type:       StepTypeSavePoint,
		MaxRetries: 3,
	}

	definition := &WorkflowDefinition{
		ID:   "test-workflow",
		Name: "Test Workflow",
		Definition: GraphDefinition{
			Start: "savepoint1",
			Steps: map[string]*StepDefinition{
				"savepoint1": stepDef,
			},
		},
	}

	mockStore.EXPECT().GetWorkflowDefinition(mock.Anything, instance.WorkflowID).Return(definition, nil)
	mockStore.EXPECT().UpdateStep(mock.Anything, stepID, StepStatusRunning, mock.Anything, mock.Anything).Return(nil)
	mockStore.EXPECT().LogEvent(mock.Anything, instanceID, &stepID, EventStepStarted, mock.Anything).Return(nil)
	mockStore.EXPECT().UpdateStep(mock.Anything, stepID, StepStatusCompleted, mock.Anything, mock.Anything).Return(nil)
	mockStore.EXPECT().LogEvent(mock.Anything, instanceID, &stepID, EventStepCompleted, mock.Anything).Return(nil)
	mockStore.EXPECT().GetInstance(mock.Anything, instanceID).Return(instance, nil)
	mockStore.EXPECT().GetWorkflowDefinition(mock.Anything, instance.WorkflowID).Return(definition, nil)
	mockStore.EXPECT().GetStepsByInstance(mock.Anything, instanceID).Return([]WorkflowStep{step}, nil)

	err := engine.executeStep(context.Background(), instance, &step)

	assert.NoError(t, err)
}

func TestEngine_ExecuteFork_Success(t *testing.T) {
	mockTxManager := NewMockTxManager(t)
	mockStore := NewMockStore(t)
	mockStore.EXPECT().GetCancelRequest(mock.Anything, mock.Anything).Return(nil, ErrEntityNotFound).Maybe()
	engine := NewEngine(nil, WithEngineTxManager(mockTxManager), WithEngineStore(mockStore))
	defer engine.Shutdown()

	instanceID := int64(123)
	stepID := int64(456)

	instance := &WorkflowInstance{
		ID:         instanceID,
		WorkflowID: "test-workflow",
		Status:     StatusRunning,
	}

	step := &WorkflowStep{
		ID:         stepID,
		InstanceID: instanceID,
		StepName:   "fork-step",
		StepType:   StepTypeFork,
		Status:     StepStatusPending,
		Input:      json.RawMessage(`{"key": "value"}`),
		MaxRetries: 3,
	}

	stepDef := &StepDefinition{
		Name:       "fork-step",
		Type:       StepTypeFork,
		Handler:    "fork-handler",
		MaxRetries: 3,
		Parallel:   []string{"parallel1", "parallel2"},
		Next:       []string{"join-step"},
	}

	definition := &WorkflowDefinition{
		ID:   "test-workflow",
		Name: "Test Workflow",
		Definition: GraphDefinition{
			Start: "fork-step",
			Steps: map[string]*StepDefinition{
				"fork-step": stepDef,
				"parallel1": {
					Name:       "parallel1",
					Type:       StepTypeTask,
					Handler:    "test-handler",
					MaxRetries: 3,
				},
				"parallel2": {
					Name:       "parallel2",
					Type:       StepTypeTask,
					Handler:    "test-handler",
					MaxRetries: 3,
				},
				"join-step": {
					Name:         "join-step",
					Type:         StepTypeJoin,
					WaitFor:      []string{"parallel1", "parallel2"},
					JoinStrategy: JoinStrategyAll,
				},
			},
		},
	}

	mockStore.EXPECT().LogEvent(mock.Anything, instanceID, &stepID, EventForkStarted, mock.Anything).Return(nil)
	mockStore.EXPECT().GetWorkflowDefinition(mock.Anything, instance.WorkflowID).Return(definition, nil)
	mockStore.EXPECT().CreateStep(mock.Anything, mock.MatchedBy(func(s *WorkflowStep) bool {
		return s.InstanceID == instanceID && s.StepName == "parallel1"
	})).Return(nil)
	mockStore.EXPECT().EnqueueStep(mock.Anything, instanceID, mock.Anything, PriorityNormal, mock.Anything).Return(nil)
	mockStore.EXPECT().CreateStep(mock.Anything, mock.MatchedBy(func(s *WorkflowStep) bool {
		return s.InstanceID == instanceID && s.StepName == "parallel2"
	})).Return(nil)
	mockStore.EXPECT().EnqueueStep(mock.Anything, instanceID, mock.Anything, PriorityNormal, mock.Anything).Return(nil)
	mockStore.EXPECT().CreateJoinState(mock.Anything, instanceID, "join-step", []string{"parallel1", "parallel2"}, JoinStrategyAll).Return(nil)
	mockStore.EXPECT().LogEvent(mock.Anything, instanceID, &stepID, EventJoinStateCreated, mock.Anything).Return(nil)

	output, err := engine.executeFork(context.Background(), instance, step, stepDef)

	assert.NoError(t, err)
	assert.Contains(t, string(output), "forked")
	assert.Contains(t, string(output), "parallel1")
	assert.Contains(t, string(output), "parallel2")
}

func TestEngine_ExecuteJoin_NotReady(t *testing.T) {
	mockTxManager := NewMockTxManager(t)
	mockStore := NewMockStore(t)
	mockStore.EXPECT().GetCancelRequest(mock.Anything, mock.Anything).Return(nil, ErrEntityNotFound).Maybe()
	engine := NewEngine(nil, WithEngineTxManager(mockTxManager), WithEngineStore(mockStore))
	defer engine.Shutdown()

	instanceID := int64(123)
	stepID := int64(456)

	instance := &WorkflowInstance{
		ID:         instanceID,
		WorkflowID: "test-workflow",
		Status:     StatusRunning,
	}

	step := &WorkflowStep{
		ID:         stepID,
		InstanceID: instanceID,
		StepName:   "join-step",
		StepType:   StepTypeJoin,
		Status:     StepStatusPending,
		Input:      json.RawMessage(`{"key": "value"}`),
		MaxRetries: 0,
	}

	joinState := &JoinState{
		InstanceID:   instanceID,
		JoinStepName: "join-step",
		WaitingFor:   []string{"step1", "step2"},
		Completed:    []string{"step1"},
		Failed:       []string{},
		JoinStrategy: JoinStrategyAll,
		IsReady:      false,
	}

	mockStore.EXPECT().GetJoinState(mock.Anything, instanceID, "join-step").Return(joinState, nil)
	mockStore.EXPECT().LogEvent(mock.Anything, instanceID, &stepID, EventJoinCheck, mock.Anything).Return(nil)

	output, err := engine.executeJoin(context.Background(), instance, step, nil)

	assert.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "join not ready")
}

func TestEngine_ExecuteJoin_Success(t *testing.T) {
	mockTxManager := NewMockTxManager(t)
	mockStore := NewMockStore(t)
	mockStore.EXPECT().GetCancelRequest(mock.Anything, mock.Anything).Return(nil, ErrEntityNotFound).Maybe()
	engine := NewEngine(nil, WithEngineTxManager(mockTxManager), WithEngineStore(mockStore))
	defer engine.Shutdown()

	instanceID := int64(123)
	stepID := int64(456)

	instance := &WorkflowInstance{
		ID:         instanceID,
		WorkflowID: "test-workflow",
		Status:     StatusRunning,
	}

	step := &WorkflowStep{
		ID:         stepID,
		InstanceID: instanceID,
		StepName:   "join-step",
		StepType:   StepTypeJoin,
		Status:     StepStatusPending,
		Input:      json.RawMessage(`{"key": "value"}`),
		MaxRetries: 0,
	}

	joinState := &JoinState{
		InstanceID:   instanceID,
		JoinStepName: "join-step",
		WaitingFor:   []string{"step1", "step2"},
		Completed:    []string{"step1", "step2"},
		Failed:       []string{},
		JoinStrategy: JoinStrategyAll,
		IsReady:      true,
	}

	steps := []WorkflowStep{
		{
			ID:         789,
			InstanceID: instanceID,
			StepName:   "step1",
			Status:     StepStatusCompleted,
			Output:     json.RawMessage(`{"result1": "success"}`),
		},
		{
			ID:         790,
			InstanceID: instanceID,
			StepName:   "step2",
			Status:     StepStatusCompleted,
			Output:     json.RawMessage(`{"result2": "success"}`),
		},
	}

	mockStore.EXPECT().GetJoinState(mock.Anything, instanceID, "join-step").Return(joinState, nil)
	mockStore.EXPECT().LogEvent(mock.Anything, instanceID, &stepID, EventJoinCheck, mock.Anything).Return(nil)
	mockStore.EXPECT().GetStepsByInstance(mock.Anything, instanceID).Return(steps, nil)
	mockStore.EXPECT().LogEvent(mock.Anything, instanceID, &stepID, EventJoinCompleted, mock.Anything).Return(nil)

	output, err := engine.executeJoin(context.Background(), instance, step, nil)

	assert.NoError(t, err)
	assert.NotNil(t, output)

	var result map[string]any
	err = json.Unmarshal(output, &result)
	assert.NoError(t, err)
	assert.Equal(t, "success", result["status"])
	assert.Contains(t, result, "outputs")
	assert.Contains(t, result, "completed")
	assert.Contains(t, result, "failed")
}

func TestEngine_ExecuteJoin_WithFailures(t *testing.T) {
	mockTxManager := NewMockTxManager(t)
	mockStore := NewMockStore(t)
	mockStore.EXPECT().GetCancelRequest(mock.Anything, mock.Anything).Return(nil, ErrEntityNotFound).Maybe()
	engine := NewEngine(nil, WithEngineTxManager(mockTxManager), WithEngineStore(mockStore))
	defer engine.Shutdown()

	instanceID := int64(123)
	stepID := int64(456)

	instance := &WorkflowInstance{
		ID:         instanceID,
		WorkflowID: "test-workflow",
		Status:     StatusRunning,
	}

	step := &WorkflowStep{
		ID:         stepID,
		InstanceID: instanceID,
		StepName:   "join-step",
		StepType:   StepTypeJoin,
		Status:     StepStatusPending,
		Input:      json.RawMessage(`{"key": "value"}`),
		MaxRetries: 0,
	}

	joinState := &JoinState{
		InstanceID:   instanceID,
		JoinStepName: "join-step",
		WaitingFor:   []string{"step1", "step2"},
		Completed:    []string{"step1"},
		Failed:       []string{"step2"},
		JoinStrategy: JoinStrategyAll,
		IsReady:      true,
	}

	steps := []WorkflowStep{
		{
			ID:         789,
			InstanceID: instanceID,
			StepName:   "step1",
			Status:     StepStatusCompleted,
			Output:     json.RawMessage(`{"result1": "success"}`),
		},
		{
			ID:         790,
			InstanceID: instanceID,
			StepName:   "step2",
			Status:     StepStatusFailed,
			Output:     json.RawMessage(`{}`),
		},
	}

	mockStore.EXPECT().GetJoinState(mock.Anything, instanceID, "join-step").Return(joinState, nil)
	mockStore.EXPECT().LogEvent(mock.Anything, instanceID, &stepID, EventJoinCheck, mock.Anything).Return(nil)
	mockStore.EXPECT().GetStepsByInstance(mock.Anything, instanceID).Return(steps, nil)

	output, err := engine.executeJoin(context.Background(), instance, step, nil)

	assert.Error(t, err)
	assert.NotNil(t, output)
	assert.Contains(t, err.Error(), "join failed")

	var result map[string]any
	err = json.Unmarshal(output, &result)
	assert.NoError(t, err)
	assert.Equal(t, "failed", result["status"])
}

func TestEngine_HandleStepSuccess_WithNextSteps(t *testing.T) {
	mockTxManager := NewMockTxManager(t)
	mockStore := NewMockStore(t)
	mockStore.EXPECT().GetCancelRequest(mock.Anything, mock.Anything).Return(nil, ErrEntityNotFound).Maybe()
	engine := NewEngine(nil, WithEngineTxManager(mockTxManager), WithEngineStore(mockStore))
	defer engine.Shutdown()

	instanceID := int64(123)
	stepID := int64(456)

	instance := &WorkflowInstance{
		ID:         instanceID,
		WorkflowID: "test-workflow",
		Status:     StatusRunning,
	}

	step := WorkflowStep{
		ID:         stepID,
		InstanceID: instanceID,
		StepName:   "step1",
		StepType:   StepTypeTask,
		Status:     StepStatusRunning,
		Input:      json.RawMessage(`{"key": "value"}`),
		MaxRetries: 3,
	}

	stepDef := &StepDefinition{
		Name:       "step1",
		Type:       StepTypeTask,
		Handler:    "test-handler",
		MaxRetries: 3,
		Next:       []string{"step2"},
	}

	output := json.RawMessage(`{"result": "success"}`)

	definition := &WorkflowDefinition{
		ID:   "test-workflow",
		Name: "Test Workflow",
		Definition: GraphDefinition{
			Start: "step1",
			Steps: map[string]*StepDefinition{
				"step1": stepDef,
				"step2": {
					Name:       "step2",
					Type:       StepTypeTask,
					Handler:    "test-handler",
					MaxRetries: 3,
				},
			},
		},
	}

	mockStore.EXPECT().UpdateStep(mock.Anything, stepID, StepStatusCompleted, output, mock.Anything).Return(nil)
	mockStore.EXPECT().LogEvent(mock.Anything, instanceID, &stepID, EventStepCompleted, mock.Anything).Return(nil)
	mockStore.EXPECT().GetInstance(mock.Anything, instanceID).Return(instance, nil)
	mockStore.EXPECT().GetWorkflowDefinition(mock.Anything, instance.WorkflowID).Return(definition, nil)
	mockStore.EXPECT().GetStepsByInstance(mock.Anything, instanceID).Return([]WorkflowStep{step}, nil)
	mockStore.EXPECT().GetStepsByInstance(mock.Anything, instanceID).Return([]WorkflowStep{step}, nil)
	mockStore.EXPECT().CreateStep(mock.Anything, mock.MatchedBy(func(s *WorkflowStep) bool {
		return s.InstanceID == instanceID && s.StepName == "step2"
	})).Return(nil)
	mockStore.EXPECT().EnqueueStep(mock.Anything, instanceID, mock.Anything, PriorityNormal, mock.Anything).Return(nil)

	err := engine.handleStepSuccess(context.Background(), instance, &step, stepDef, output, true)

	assert.NoError(t, err)
}

func TestEngine_HandleStepFailure_WithRetries(t *testing.T) {
	mockTxManager := NewMockTxManager(t)
	mockStore := NewMockStore(t)
	mockStore.EXPECT().GetCancelRequest(mock.Anything, mock.Anything).Return(nil, ErrEntityNotFound).Maybe()
	engine := NewEngine(nil, WithEngineTxManager(mockTxManager), WithEngineStore(mockStore))
	defer engine.Shutdown()

	instanceID := int64(123)
	stepID := int64(456)

	instance := &WorkflowInstance{
		ID:         instanceID,
		WorkflowID: "test-workflow",
		Status:     StatusRunning,
	}

	step := &WorkflowStep{
		ID:         stepID,
		InstanceID: instanceID,
		StepName:   "step1",
		StepType:   StepTypeTask,
		Status:     StepStatusRunning,
		Input:      json.RawMessage(`{"key": "value"}`),
		MaxRetries: 3,
		RetryCount: 1,
	}

	stepDef := &StepDefinition{
		Name:       "step1",
		Type:       StepTypeTask,
		Handler:    "test-handler",
		MaxRetries: 3,
	}

	stepErr := errors.New("step execution failed")

	mockStore.EXPECT().UpdateStep(mock.Anything, stepID, StepStatusFailed, mock.Anything, mock.Anything).Return(nil)
	mockStore.EXPECT().LogEvent(mock.Anything, instanceID, &stepID, EventStepRetry, mock.MatchedBy(func(data map[string]any) bool {
		return data[KeyRetryCount] == 2 // RetryCount was incremented from 1 to 2
	})).Return(nil)
	mockStore.EXPECT().EnqueueStep(mock.Anything, instanceID, &stepID, PriorityHigh, mock.Anything).Return(nil)

	err := engine.handleStepFailure(context.Background(), instance, step, stepDef, stepErr)

	assert.NoError(t, err)
}

func TestEngine_HandleStepFailure_NoRetriesLeft(t *testing.T) {
	mockTxManager := NewMockTxManager(t)
	mockStore := NewMockStore(t)
	mockStore.EXPECT().GetCancelRequest(mock.Anything, mock.Anything).Return(nil, ErrEntityNotFound).Maybe()
	engine := NewEngine(nil, WithEngineTxManager(mockTxManager), WithEngineStore(mockStore))
	defer engine.Shutdown()

	instanceID := int64(123)
	stepID := int64(456)

	instance := &WorkflowInstance{
		ID:         instanceID,
		WorkflowID: "test-workflow",
		Status:     StatusRunning,
	}

	step := WorkflowStep{
		ID:         stepID,
		InstanceID: instanceID,
		StepName:   "step1",
		StepType:   StepTypeTask,
		Status:     StepStatusRunning,
		Input:      json.RawMessage(`{"key": "value"}`),
		MaxRetries: 3,
		RetryCount: 3,
	}

	stepDef := &StepDefinition{
		Name:       "step1",
		Type:       StepTypeTask,
		Handler:    "test-handler",
		MaxRetries: 3,
	}

	definition := &WorkflowDefinition{
		ID:   "test-workflow",
		Name: "Test Workflow",
		Definition: GraphDefinition{
			Start: "step1",
			Steps: map[string]*StepDefinition{
				"step1": stepDef,
			},
		},
	}

	stepErr := errors.New("step execution failed")

	mockStore.EXPECT().UpdateStep(mock.Anything, stepID, StepStatusFailed, mock.Anything, mock.Anything).Return(nil)
	mockStore.EXPECT().LogEvent(mock.Anything, instanceID, &stepID, EventStepFailed, mock.Anything).Return(nil)
	mockStore.EXPECT().GetInstance(mock.Anything, instanceID).Return(instance, nil)
	mockStore.EXPECT().GetWorkflowDefinition(mock.Anything, instance.WorkflowID).Return(definition, nil)
	mockStore.EXPECT().GetStepsByInstance(mock.Anything, instanceID).Return([]WorkflowStep{step}, nil)
	mockStore.EXPECT().UpdateInstanceStatus(mock.Anything, instanceID, StatusFailed, mock.Anything, mock.Anything).Return(nil)

	err := engine.handleStepFailure(context.Background(), instance, &step, stepDef, stepErr)

	assert.NoError(t, err)
}

func TestEngine_HandleStepFailure_WithOnFailure(t *testing.T) {
	mockTxManager := NewMockTxManager(t)
	mockStore := NewMockStore(t)
	mockStore.EXPECT().GetCancelRequest(mock.Anything, mock.Anything).Return(nil, ErrEntityNotFound).Maybe()
	engine := NewEngine(nil, WithEngineTxManager(mockTxManager), WithEngineStore(mockStore))
	defer engine.Shutdown()

	instanceID := int64(123)
	stepID := int64(456)

	instance := &WorkflowInstance{
		ID:         instanceID,
		WorkflowID: "test-workflow",
		Status:     StatusRunning,
	}

	step := WorkflowStep{
		ID:         stepID,
		InstanceID: instanceID,
		StepName:   "step1",
		StepType:   StepTypeTask,
		Status:     StepStatusRunning,
		Input:      json.RawMessage(`{"key": "value"}`),
		MaxRetries: 3,
		RetryCount: 3,
	}

	stepDef := &StepDefinition{
		Name:       "step1",
		Type:       StepTypeTask,
		Handler:    "test-handler",
		MaxRetries: 3,
		OnFailure:  "compensation-step",
	}

	stepErr := errors.New("step execution failed")

	definition := &WorkflowDefinition{
		ID:   "test-workflow",
		Name: "Test Workflow",
		Definition: GraphDefinition{
			Start: "step1",
			Steps: map[string]*StepDefinition{
				"step1": stepDef,
				"compensation-step": {
					Name:       "compensation-step",
					Type:       StepTypeTask,
					Handler:    "compensation-handler",
					MaxRetries: 3,
				},
			},
		},
	}

	mockStore.EXPECT().UpdateStep(mock.Anything, stepID, StepStatusFailed, mock.Anything, mock.Anything).Return(nil)
	mockStore.EXPECT().LogEvent(mock.Anything, instanceID, &stepID, EventStepFailed, mock.Anything).Return(nil)
	mockStore.EXPECT().GetInstance(mock.Anything, instanceID).Return(instance, nil)
	mockStore.EXPECT().GetWorkflowDefinition(mock.Anything, instance.WorkflowID).Return(definition, nil)
	mockStore.EXPECT().GetStepsByInstance(mock.Anything, instanceID).Return([]WorkflowStep{step}, nil)
	mockStore.EXPECT().UpdateInstanceStatus(mock.Anything, instanceID, StatusFailed, mock.Anything, mock.Anything).Return(nil)

	err := engine.handleStepFailure(context.Background(), instance, &step, stepDef, stepErr)

	assert.NoError(t, err)
}

func TestEngine_GetStatus(t *testing.T) {
	mockTxManager := NewMockTxManager(t)
	mockStore := NewMockStore(t)
	mockStore.EXPECT().GetCancelRequest(mock.Anything, mock.Anything).Return(nil, ErrEntityNotFound).Maybe()
	engine := NewEngine(nil, WithEngineTxManager(mockTxManager), WithEngineStore(mockStore))
	defer engine.Shutdown()

	instanceID := int64(123)
	expectedInstance := &WorkflowInstance{
		ID:         instanceID,
		WorkflowID: "test-workflow",
		Status:     StatusRunning,
	}

	mockStore.EXPECT().GetInstance(mock.Anything, instanceID).Return(expectedInstance, nil)

	status, err := engine.GetStatus(context.Background(), instanceID)

	assert.NoError(t, err)
	assert.Equal(t, StatusRunning, status)
}

func TestEngine_GetSteps(t *testing.T) {
	mockTxManager := NewMockTxManager(t)
	mockStore := NewMockStore(t)
	mockStore.EXPECT().GetCancelRequest(mock.Anything, mock.Anything).Return(nil, ErrEntityNotFound).Maybe()
	engine := NewEngine(nil, WithEngineTxManager(mockTxManager), WithEngineStore(mockStore))
	defer engine.Shutdown()

	instanceID := int64(123)
	expectedSteps := []WorkflowStep{
		{
			ID:         456,
			InstanceID: instanceID,
			StepName:   "step1",
			Status:     StepStatusCompleted,
		},
		{
			ID:         789,
			InstanceID: instanceID,
			StepName:   "step2",
			Status:     StepStatusRunning,
		},
	}

	mockStore.EXPECT().GetStepsByInstance(mock.Anything, instanceID).Return(expectedSteps, nil)

	steps, err := engine.GetSteps(context.Background(), instanceID)

	assert.NoError(t, err)
	assert.Equal(t, expectedSteps, steps)
}

func TestEngine_ValidateDefinition_Valid(t *testing.T) {
	mockTxManager := NewMockTxManager(t)
	mockStore := NewMockStore(t)
	mockStore.EXPECT().GetCancelRequest(mock.Anything, mock.Anything).Return(nil, ErrEntityNotFound).Maybe()
	engine := NewEngine(nil, WithEngineTxManager(mockTxManager), WithEngineStore(mockStore))
	defer engine.Shutdown()

	definition := &WorkflowDefinition{
		ID:   "test-workflow",
		Name: "Test Workflow",
		Definition: GraphDefinition{
			Start: "step1",
			Steps: map[string]*StepDefinition{
				"step1": {
					Name:       "step1",
					Type:       StepTypeTask,
					Handler:    "test-handler",
					MaxRetries: 3,
					Next:       []string{"step2"},
				},
				"step2": {
					Name:       "step2",
					Type:       StepTypeTask,
					Handler:    "test-handler",
					MaxRetries: 3,
					OnFailure:  "compensation-step",
				},
				"compensation-step": {
					Name:       "compensation-step",
					Type:       StepTypeTask,
					Handler:    "compensation-handler",
					MaxRetries: 3,
				},
			},
		},
	}

	err := engine.validateDefinition(definition)

	assert.NoError(t, err)
}

func TestEngine_ValidateDefinition_ComplexWorkflow(t *testing.T) {
	mockTxManager := NewMockTxManager(t)
	mockStore := NewMockStore(t)
	mockStore.EXPECT().GetCancelRequest(mock.Anything, mock.Anything).Return(nil, ErrEntityNotFound).Maybe()
	engine := NewEngine(nil, WithEngineTxManager(mockTxManager), WithEngineStore(mockStore))
	defer engine.Shutdown()

	definition := &WorkflowDefinition{
		ID:   "complex-workflow",
		Name: "Complex Workflow",
		Definition: GraphDefinition{
			Start: "fork-step",
			Steps: map[string]*StepDefinition{
				"fork-step": {
					Name:       "fork-step",
					Type:       StepTypeFork,
					Handler:    "fork-handler",
					MaxRetries: 3,
					Parallel:   []string{"parallel1", "parallel2"},
					Next:       []string{"join-step"},
				},
				"parallel1": {
					Name:       "parallel1",
					Type:       StepTypeTask,
					Handler:    "test-handler",
					MaxRetries: 3,
				},
				"parallel2": {
					Name:       "parallel2",
					Type:       StepTypeTask,
					Handler:    "test-handler",
					MaxRetries: 3,
				},
				"join-step": {
					Name:         "join-step",
					Type:         StepTypeJoin,
					WaitFor:      []string{"parallel1", "parallel2"},
					JoinStrategy: JoinStrategyAll,
				},
			},
		},
	}

	err := engine.validateDefinition(definition)

	assert.NoError(t, err)
}
