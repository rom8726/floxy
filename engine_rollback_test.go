package floxy

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func Test_rollbackStep_NoCompensationHandler(t *testing.T) {
	ctx := context.Background()
	engine, store := newTestEngineWithStore(t)

	def := &WorkflowDefinition{
		ID:   "wf_rb1",
		Name: "wf_rb1",
		Definition: GraphDefinition{
			Start: "A",
			Steps: map[string]*StepDefinition{
				"A": {Name: "A", Type: StepTypeTask, OnFailure: "", Prev: rootStepName},
			},
		},
	}

	step := &WorkflowStep{
		ID:         10,
		InstanceID: 1001,
		StepName:   "A",
		StepType:   StepTypeTask,
		Status:     StepStatusCompleted,
		Input:      json.RawMessage(`{"x":1}`),
	}

	store.EXPECT().UpdateStep(mock.Anything, step.ID, StepStatusRolledBack, step.Input, (*string)(nil)).Return(nil)

	err := engine.rollbackStep(ctx, step, def)
	assert.NoError(t, err)
}

func Test_rollbackStep_WithCompensation_RetryAvailable(t *testing.T) {
	ctx := context.Background()
	engine, store := newTestEngineWithStore(t)

	def := &WorkflowDefinition{
		ID:   "wf_rb2",
		Name: "wf_rb2",
		Definition: GraphDefinition{
			Start: "A",
			Steps: map[string]*StepDefinition{
				"A":      {Name: "A", Type: StepTypeTask, OnFailure: "A_comp", RetryDelay: 0},
				"A_comp": {Name: "A_comp", Type: StepTypeTask, MaxRetries: 3},
			},
		},
	}

	step := &WorkflowStep{
		ID:                     20,
		InstanceID:             2002,
		StepName:               "A",
		StepType:               StepTypeTask,
		Status:                 StepStatusFailed,
		CompensationRetryCount: 0,
	}

	store.EXPECT().UpdateStepCompensationRetry(mock.Anything, step.ID, 1, StepStatusCompensation).Return(nil)
	store.EXPECT().EnqueueStep(mock.Anything, step.InstanceID, &step.ID, PriorityHigh, mock.Anything).Return(nil)
	store.EXPECT().LogEvent(mock.Anything, step.InstanceID, &step.ID, EventStepStarted, mock.Anything).Return(nil)

	err := engine.rollbackStep(ctx, step, def)
	assert.NoError(t, err)
}

func Test_rollbackStep_MaxRetriesExceeded_DLQ(t *testing.T) {
	ctx := context.Background()
	engine, store := newTestEngineWithStore(t)

	def := &WorkflowDefinition{
		ID:   "wf_rb3",
		Name: "wf_rb3",
		Definition: GraphDefinition{
			Start: "A",
			Steps: map[string]*StepDefinition{
				"A":      {Name: "A", Type: StepTypeTask, OnFailure: "A_comp"},
				"A_comp": {Name: "A_comp", Type: StepTypeTask, MaxRetries: 1},
			},
		},
	}

	msg := "boom"
	step := &WorkflowStep{
		ID:                     30,
		InstanceID:             3003,
		StepName:               "A",
		StepType:               StepTypeTask,
		Status:                 StepStatusFailed,
		CompensationRetryCount: 1, // >= MaxRetries
		Error:                  &msg,
	}

	store.EXPECT().UpdateStep(mock.Anything, step.ID, StepStatusFailed, step.Input, (*string)(nil)).Return(nil)
	store.EXPECT().LogEvent(mock.Anything, step.InstanceID, &step.ID, EventStepFailed, mock.Anything).Return(nil)
	store.EXPECT().CreateDeadLetterRecord(mock.Anything, mock.MatchedBy(func(rec *DeadLetterRecord) bool {
		return rec != nil && rec.InstanceID == step.InstanceID && rec.StepID == step.ID && rec.WorkflowID == def.ID
	})).Return(nil)

	err := engine.rollbackStep(ctx, step, def)
	assert.NoError(t, err)
}

func Test_rollbackStepChain_Linear_WithSavePoint(t *testing.T) {
	ctx := context.Background()
	engine, store := newTestEngineWithStore(t)

	// Build simple linear flow: A -> B -> C; rollback from C to savepoint B should only roll back C
	def := &WorkflowDefinition{
		ID:   "wf_chain1",
		Name: "wf_chain1",
		Definition: GraphDefinition{
			Start: "A",
			Steps: map[string]*StepDefinition{
				"A": {Name: "A", Type: StepTypeTask, Next: []string{"B"}, Prev: rootStepName, OnFailure: ""},
				"B": {Name: "B", Type: StepTypeTask, Next: []string{"C"}, Prev: "A", OnFailure: ""},
				"C": {Name: "C", Type: StepTypeTask, Prev: "B", OnFailure: ""},
			},
		},
	}

	// Steps map with statuses
	stepA := &WorkflowStep{ID: 101, InstanceID: 5001, StepName: "A", StepType: StepTypeTask, Status: StepStatusCompleted}
	stepB := &WorkflowStep{ID: 102, InstanceID: 5001, StepName: "B", StepType: StepTypeTask, Status: StepStatusCompleted}
	stepC := &WorkflowStep{ID: 103, InstanceID: 5001, StepName: "C", StepType: StepTypeTask, Status: StepStatusCompleted}
	stepMap := map[string]*WorkflowStep{"A": stepA, "B": stepB, "C": stepC}

	// Only C should be rolled back
	store.EXPECT().UpdateStep(mock.Anything, stepC.ID, StepStatusRolledBack, stepC.Input, (*string)(nil)).Return(nil)

	err := engine.rollbackStepChain(ctx, stepC.InstanceID, "C", "B", def, stepMap, false, 0)
	assert.NoError(t, err)
}

func Test_rollbackStepChain_ForkWithCondition_ExecutedNextBranch(t *testing.T) {
	ctx := context.Background()
	engine, store := newTestEngineWithStore(t)

	// Graph:
	// F (fork) -> Parallel [C]
	// C (condition) -> Next: X, Else: Y
	// Expect: if C completed and X completed (executed branch Next), rollback X then C (depth-first)
	def := &WorkflowDefinition{
		ID:   "wf_chain2",
		Name: "wf_chain2",
		Definition: GraphDefinition{
			Start: "F",
			Steps: map[string]*StepDefinition{
				"F": {Name: "F", Type: StepTypeFork, Parallel: []string{"C"}, Prev: rootStepName},
				"C": {Name: "C", Type: StepTypeCondition, Next: []string{"X"}, Else: "Y", Prev: "F"},
				"X": {Name: "X", Type: StepTypeTask, Prev: "C"},
				"Y": {Name: "Y", Type: StepTypeTask, Prev: "C"},
			},
		},
	}

	// Steps that were executed
	stepC := &WorkflowStep{ID: 201, InstanceID: 6001, StepName: "C", StepType: StepTypeCondition, Status: StepStatusCompleted}
	stepX := &WorkflowStep{ID: 202, InstanceID: 6001, StepName: "X", StepType: StepTypeTask, Status: StepStatusCompleted}
	stepMap := map[string]*WorkflowStep{"C": stepC, "X": stepX}

	// Only X and C should be rolled back, Y was not executed
	store.EXPECT().UpdateStep(mock.Anything, stepX.ID, StepStatusRolledBack, stepX.Input, (*string)(nil)).Return(nil)
	store.EXPECT().UpdateStep(mock.Anything, stepC.ID, StepStatusRolledBack, stepC.Input, (*string)(nil)).Return(nil)

	// Start directly from the parallel branch condition step with isParallel=true
	err := engine.rollbackStepChain(ctx, stepC.InstanceID, "C", "", def, stepMap, true, 0)
	assert.NoError(t, err)
}
