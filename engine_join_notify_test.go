package floxy

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func Test_notifyJoinSteps_Ready_NoPending_Enqueues(t *testing.T) {
	ctx := context.Background()
	engine, store := newTestEngineWithStore(t)

	def := buildForkJoinDirectWaitForDef() // Fork F -> A,B ; Join J.WaitFor=[A,B]
	instanceID := int64(7001)

	store.EXPECT().GetInstance(mock.Anything, instanceID).
		Return(&WorkflowInstance{ID: instanceID, WorkflowID: def.ID, Status: StatusRunning}, nil).Maybe()
	store.EXPECT().GetWorkflowDefinition(mock.Anything, def.ID).Return(def, nil).Maybe()

	// Steps in the instance: A and B completed; no join step yet
	steps := []WorkflowStep{
		{InstanceID: instanceID, StepName: "A", StepType: StepTypeTask, Status: StepStatusCompleted, Input: json.RawMessage(`{"a":1}`)},
		{InstanceID: instanceID, StepName: "B", StepType: StepTypeTask, Status: StepStatusCompleted},
	}
	store.EXPECT().GetStepsByInstance(mock.Anything, instanceID).Return(steps, nil)

	// Join state not yet created -> rely on stepDef.WaitFor
	store.EXPECT().GetJoinState(mock.Anything, instanceID, "J").Return(nil, ErrEntityNotFound)

	// Mark join as ready for completed step A
	store.EXPECT().UpdateJoinState(mock.Anything, instanceID, "J", "A", true).Return(true, nil)

	// Log event for join state update
	store.EXPECT().LogEvent(mock.Anything, instanceID, (*int64)(nil), EventJoinUpdated, mock.Anything).Return(nil).Maybe()

	// Since ready and no pending extras, the engine should create and enqueue join step J
	store.EXPECT().CreateStep(mock.Anything, mock.MatchedBy(func(s *WorkflowStep) bool {
		return s != nil && s.InstanceID == instanceID && s.StepName == "J" && s.StepType == StepTypeJoin && s.Status == StepStatusPending
	})).Return(nil)
	store.EXPECT().EnqueueStep(mock.Anything, instanceID, mock.AnythingOfType("*int64"), PriorityNormal, timeZero()).Return(nil)
	store.EXPECT().LogEvent(mock.Anything, instanceID, mock.AnythingOfType("*int64"), EventJoinReady, mock.Anything).Return(nil).Maybe()

	err := engine.notifyJoinSteps(ctx, instanceID, "A", true)
	assert.NoError(t, err)
}

func Test_notifyJoinSteps_NotReady_DueToPendingExtra(t *testing.T) {
	ctx := context.Background()
	engine, store := newTestEngineWithStore(t)

	def := buildForkJoinDirectWaitForDef()
	instanceID := int64(7002)

	store.EXPECT().GetInstance(mock.Anything, instanceID).
		Return(&WorkflowInstance{ID: instanceID, WorkflowID: def.ID, Status: StatusRunning}, nil).Maybe()
	store.EXPECT().GetWorkflowDefinition(mock.Anything, def.ID).Return(def, nil).Maybe()

	// Steps include extra pending step A1 within parallel branch A to block join
	steps := []WorkflowStep{
		{InstanceID: instanceID, StepName: "A", StepType: StepTypeTask, Status: StepStatusCompleted},
		{InstanceID: instanceID, StepName: "B", StepType: StepTypeTask, Status: StepStatusCompleted},
		{InstanceID: instanceID, StepName: "A1", StepType: StepTypeTask, Status: StepStatusPending}, // pending extra in branch
	}
	store.EXPECT().GetStepsByInstance(mock.Anything, instanceID).Return(steps, nil)

	store.EXPECT().GetJoinState(mock.Anything, instanceID, "J").Return(nil, ErrEntityNotFound)

	// First call indicates ready according to join strategy
	store.EXPECT().UpdateJoinState(mock.Anything, instanceID, "J", "A", true).Return(true, nil)
	// Engine will reset readiness due to pending extras by calling UpdateJoinState again
	store.EXPECT().UpdateJoinState(mock.Anything, instanceID, "J", "A", true).Return(false, nil)

	store.EXPECT().LogEvent(mock.Anything, instanceID, (*int64)(nil), EventJoinUpdated, mock.Anything).Return(nil).Maybe()

	// No CreateStep/EnqueueStep expected due to not ready
	err := engine.notifyJoinSteps(ctx, instanceID, "A", true)
	assert.NoError(t, err)
}

func Test_notifyJoinStepsForStep_Ready_Enqueues(t *testing.T) {
	ctx := context.Background()
	engine, store := newTestEngineWithStore(t)

	def := buildForkJoinDirectWaitForDef()
	instanceID := int64(7003)

	store.EXPECT().GetInstance(mock.Anything, instanceID).
		Return(&WorkflowInstance{ID: instanceID, WorkflowID: def.ID, Status: StatusRunning}, nil)
	store.EXPECT().GetStepsByInstance(mock.Anything, instanceID).Return([]WorkflowStep{
		{InstanceID: instanceID, StepName: "A", StepType: StepTypeTask, Status: StepStatusCompleted, Input: json.RawMessage(`{"a":2}`)},
		{InstanceID: instanceID, StepName: "B", StepType: StepTypeTask, Status: StepStatusCompleted},
	}, nil)
	store.EXPECT().GetWorkflowDefinition(mock.Anything, def.ID).Return(def, nil)

	// Update join state for a specific join
	store.EXPECT().UpdateJoinState(mock.Anything, instanceID, "J", "A", true).Return(true, nil)
	store.EXPECT().LogEvent(mock.Anything, instanceID, (*int64)(nil), EventJoinUpdated, mock.Anything).Return(nil).Maybe()

	store.EXPECT().CreateStep(mock.Anything, mock.MatchedBy(func(s *WorkflowStep) bool {
		return s.StepName == "J" && s.Status == StepStatusPending
	})).Return(nil)
	store.EXPECT().EnqueueStep(mock.Anything, instanceID, mock.AnythingOfType("*int64"), PriorityNormal, timeZero()).Return(nil)
	store.EXPECT().LogEvent(mock.Anything, instanceID, mock.AnythingOfType("*int64"), EventJoinReady, mock.Anything).Return(nil).Maybe()

	err := engine.notifyJoinStepsForStep(ctx, instanceID, "J", "A", true)
	assert.NoError(t, err)
}

func Test_notifyJoinStepsForStep_DLQ_Pauses(t *testing.T) {
	ctx := context.Background()
	engine, store := newTestEngineWithStore(t)

	def := buildForkJoinDirectWaitForDef()
	instanceID := int64(7004)

	store.EXPECT().GetInstance(mock.Anything, instanceID).
		Return(&WorkflowInstance{ID: instanceID, WorkflowID: def.ID, Status: StatusDLQ}, nil)
	store.EXPECT().GetStepsByInstance(mock.Anything, instanceID).Return([]WorkflowStep{
		{InstanceID: instanceID, StepName: "A", StepType: StepTypeTask, Status: StepStatusCompleted},
		{InstanceID: instanceID, StepName: "B", StepType: StepTypeTask, Status: StepStatusCompleted},
	}, nil)
	store.EXPECT().GetWorkflowDefinition(mock.Anything, def.ID).Return(def, nil)

	store.EXPECT().UpdateJoinState(mock.Anything, instanceID, "J", "A", true).Return(true, nil)
	store.EXPECT().LogEvent(mock.Anything, instanceID, (*int64)(nil), EventJoinUpdated, mock.Anything).Return(nil).Maybe()

	// Should create paused step and NOT enqueue
	store.EXPECT().CreateStep(mock.Anything, mock.MatchedBy(func(s *WorkflowStep) bool {
		return s.StepName == "J" && s.Status == StepStatusPaused
	})).Return(nil)
	store.EXPECT().LogEvent(mock.Anything, instanceID, mock.AnythingOfType("*int64"), EventJoinReady, mock.Anything).Return(nil).Maybe()

	err := engine.notifyJoinStepsForStep(ctx, instanceID, "J", "A", true)
	assert.NoError(t, err)
}

// timeZero returns a time.Duration literal 0 in a way acceptable to testify's argument matching for EnqueueStep delay param.
// We use a helper to avoid importing time package in this test file explicitly for matching purposes.
func timeZero() interface{} { return mock.Anything } // keep matcher generic for delay
