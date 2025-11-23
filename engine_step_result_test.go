package floxy

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// minimal linear workflow without joins/forks
func buildLinearDef() *WorkflowDefinition {
	return &WorkflowDefinition{
		ID:   "wf_linear",
		Name: "wf_linear",
		Definition: GraphDefinition{
			Start: "A",
			Steps: map[string]*StepDefinition{
				"A": {Name: "A", Type: StepTypeTask, Next: []string{"B"}, Prev: rootStepName, MaxRetries: 1},
				"B": {Name: "B", Type: StepTypeTask, Prev: "A"},
			},
		},
	}
}

func Test_handleStepSuccess_SimpleNextFlow(t *testing.T) {
	ctx := context.Background()
	engine, store := newTestEngineWithStore(t)

	def := buildLinearDef()
	instance := &WorkflowInstance{ID: 42, WorkflowID: def.ID, Status: StatusRunning}

	// Current step A succeeded with some output
	output := json.RawMessage(`{"ok":true}`)
	step := &WorkflowStep{ID: 1001, InstanceID: instance.ID, StepName: "A", StepType: StepTypeTask, Status: StepStatusRunning}
	stepDef := def.Definition.Steps["A"]

	// Expectations:
	// 1) UpdateStep -> completed
	store.EXPECT().UpdateStep(mock.Anything, step.ID, StepStatusCompleted, output, (*string)(nil)).Return(nil)
	// 2) LogEvent StepCompleted (payload not asserted)
	store.EXPECT().LogEvent(mock.Anything, instance.ID, &step.ID, EventStepCompleted, mock.Anything).Return(nil).Maybe()
	// 3) Definition lookup inside handleStepSuccess (for join/conditions scan)
	store.EXPECT().GetWorkflowDefinition(mock.Anything, def.ID).Return(def, nil)
	// 4) Helpers used by isTerminalStepInForkBranch/findForkStepForParallelStep may hit instance/def; allow Maybe
	store.EXPECT().GetInstance(mock.Anything, instance.ID).Return(instance, nil).Maybe()
	store.EXPECT().GetWorkflowDefinition(mock.Anything, def.ID).Return(def, nil).Maybe()
	// 5) notifyJoinSteps path will need instance, def, steps list; provide empty steps list without joins
	store.EXPECT().GetInstance(mock.Anything, instance.ID).Return(instance, nil)
	store.EXPECT().GetWorkflowDefinition(mock.Anything, def.ID).Return(def, nil)
	// 6) Enqueue next step B via enqueueNextSteps -> CreateStep + EnqueueStep
	next := &WorkflowStep{
		InstanceID: instance.ID,
		StepName:   "B",
		StepType:   StepTypeTask,
		Status:     StepStatusPending,
		MaxRetries: 0,
		Input:      output,
	}
	// We cannot know ID beforehand; match by fields
	store.EXPECT().CreateStep(mock.Anything, mock.MatchedBy(func(ws *WorkflowStep) bool {
		return ws != nil && ws.InstanceID == next.InstanceID && ws.StepName == next.StepName && ws.StepType == next.StepType && ws.Status == StepStatusPending
	})).RunAndReturn(func(ctx context.Context, ws *WorkflowStep) error {
		ws.ID = 2002
		return nil
	})
	store.EXPECT().EnqueueStep(mock.Anything, instance.ID, mock.Anything, PriorityNormal, time.Duration(0)).Return(nil)

	err := engine.handleStepSuccess(ctx, instance, step, stepDef, output, true)
	assert.NoError(t, err)
}

func Test_handleStepSuccess_HumanWaitingDecision_NoOp(t *testing.T) {
	ctx := context.Background()
	engine, _ := newTestEngineWithStore(t)

	def := &WorkflowDefinition{ID: "wf_human", Definition: GraphDefinition{Start: "H", Steps: map[string]*StepDefinition{
		"H": {Name: "H", Type: StepTypeHuman, Prev: rootStepName},
	}}}
	instance := &WorkflowInstance{ID: 77, WorkflowID: def.ID}
	step := &WorkflowStep{ID: 5005, InstanceID: instance.ID, StepName: "H", StepType: StepTypeHuman, Status: StepStatusWaitingDecision}
	stepDef := def.Definition.Steps["H"]

	// Should early-return without any store calls; no expectations needed
	err := engine.handleStepSuccess(ctx, instance, step, stepDef, json.RawMessage(`{}`), true)
	assert.NoError(t, err)
}

func Test_handleStepFailure_RetryAvailable(t *testing.T) {
	ctx := context.Background()
	engine, store := newTestEngineWithStore(t)

	def := buildLinearDef() // MaxRetries for A is 1
	instance := &WorkflowInstance{ID: 55, WorkflowID: def.ID}
	step := &WorkflowStep{ID: 3003, InstanceID: instance.ID, StepName: "A", StepType: StepTypeTask, Status: StepStatusRunning, RetryCount: 0, MaxRetries: 1}
	stepDef := def.Definition.Steps["A"]
	stepErr := errors.New("boom")
	errMsg := stepErr.Error()

	// Expectations: mark failed, log retry, re-enqueue with delay stepDef.Delay (0)
	store.EXPECT().UpdateStep(mock.Anything, step.ID, StepStatusFailed, json.RawMessage(nil), &errMsg).Return(nil)
	store.EXPECT().LogEvent(mock.Anything, instance.ID, &step.ID, EventStepRetry, mock.Anything).Return(nil)
	store.EXPECT().EnqueueStep(mock.Anything, instance.ID, &step.ID, PriorityHigh, stepDef.Delay).Return(nil)

	err := engine.handleStepFailure(ctx, instance, step, stepDef, stepErr)
	assert.NoError(t, err)
}

func Test_handleStepFailure_DLQEnabled_NoRetry(t *testing.T) {
	ctx := context.Background()
	engine, store := newTestEngineWithStore(t)

	def := &WorkflowDefinition{
		ID:   "wf_dlq",
		Name: "wf_dlq",
		Definition: GraphDefinition{
			Start:      "A",
			DLQEnabled: true,
			Steps: map[string]*StepDefinition{
				"A": {Name: "A", Type: StepTypeTask, Prev: rootStepName, MaxRetries: 0},
			},
		},
	}
	instance := &WorkflowInstance{ID: 88, WorkflowID: def.ID, Status: StatusRunning}
	step := &WorkflowStep{ID: 4004, InstanceID: instance.ID, StepName: "A", StepType: StepTypeTask, Status: StepStatusRunning, RetryCount: 0, MaxRetries: 0}
	stepErr := errors.New("bad")
	errStr := stepErr.Error()

	// 1) Update step to paused with error
	store.EXPECT().UpdateStep(mock.Anything, step.ID, StepStatusPaused, json.RawMessage(nil), &errStr).Return(nil)
	// 2) Log failed with reason dlq
	store.EXPECT().LogEvent(mock.Anything, instance.ID, &step.ID, EventStepFailed, mock.Anything).Return(nil)
	// 3) First GetWorkflowDefinition inside handleStepFailure to check DLQ flag
	store.EXPECT().GetWorkflowDefinition(mock.Anything, def.ID).Return(def, nil).Maybe()
	// 4) notifyJoinSteps (no joins) -> GetInstance, GetWorkflowDefinition, GetStepsByInstance
	store.EXPECT().GetInstance(mock.Anything, instance.ID).Return(instance, nil)
	store.EXPECT().GetWorkflowDefinition(mock.Anything, def.ID).Return(def, nil)
	// 5) Create DLQ record
	store.EXPECT().CreateDeadLetterRecord(mock.Anything, mock.MatchedBy(func(rec *DeadLetterRecord) bool {
		return rec != nil && rec.InstanceID == instance.ID && rec.StepID == step.ID && rec.WorkflowID == def.ID && rec.Reason != ""
	})).Return(nil)
	// 6) Freeze execution
	store.EXPECT().PauseActiveStepsAndClearQueue(mock.Anything, instance.ID).Return(nil)
	// 7) Update instance status -> DLQ
	store.EXPECT().UpdateInstanceStatus(mock.Anything, instance.ID, StatusDLQ, json.RawMessage(nil), mock.Anything).Return(nil)

	err := engine.handleStepFailure(ctx, instance, step, def.Definition.Steps["A"], stepErr)
	assert.NoError(t, err)
}
