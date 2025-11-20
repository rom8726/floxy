package floxy

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Build a minimal fork/join workflow definition for tests
func buildForkJoinDef() *WorkflowDefinition {
	def := &WorkflowDefinition{
		ID:      "wf1",
		Name:    "wf1",
		Version: 1,
		Definition: GraphDefinition{
			Start: "F",
			Steps: map[string]*StepDefinition{},
		},
	}

	// Fork step F -> Parallel [A, B] -> Next includes J
	def.Definition.Steps["F"] = &StepDefinition{
		Name:     "F",
		Type:     StepTypeFork,
		Parallel: []string{"A", "B"},
		Next:     []string{"J"},
		Prev:     rootStepName,
	}

	// Branch A: A -> A1 -> T (terminal)
	def.Definition.Steps["A"] = &StepDefinition{
		Name: "A",
		Type: StepTypeTask,
		Next: []string{"A1"},
		Prev: "F",
	}
	def.Definition.Steps["A1"] = &StepDefinition{
		Name: "A1",
		Type: StepTypeTask,
		Next: []string{"T"},
		Prev: "A",
	}
	def.Definition.Steps["T"] = &StepDefinition{
		Name: "T",
		Type: StepTypeTask,
		Prev: "A1",
		// No Next -> terminal in branch
	}

	// Branch B: B -> B1 -> J
	def.Definition.Steps["B"] = &StepDefinition{
		Name: "B",
		Type: StepTypeTask,
		Next: []string{"B1"},
		Prev: "F",
	}
	def.Definition.Steps["B1"] = &StepDefinition{
		Name: "B1",
		Type: StepTypeTask,
		Next: []string{"J"},
		Prev: "B",
	}

	// Join step J
	def.Definition.Steps["J"] = &StepDefinition{
		Name:         "J",
		Type:         StepTypeJoin,
		JoinStrategy: JoinStrategyAll,
		WaitFor:      []string{"A1", "B1"},
		Prev:         "F",
	}

	return def
}

func newTestEngineWithStore(t *testing.T) (*Engine, *MockStore) {
	mockTxManager := NewMockTxManager(t)
	mockStore := NewMockStore(t)
	// Background cancel worker queries (ignore)
	mockStore.EXPECT().GetCancelRequest(mock.Anything, mock.Anything).
		Return(nil, ErrEntityNotFound).Maybe()

	engine := NewEngine(nil, WithEngineTxManager(mockTxManager), WithEngineStore(mockStore))
	t.Cleanup(func() { _ = engine.Shutdown() })

	return engine, mockStore
}

func Test_isTerminalStepInForkBranch_TerminalTrue(t *testing.T) {
	ctx := context.Background()
	def := buildForkJoinDef()

	engine, store := newTestEngineWithStore(t)

	// Instance and def lookups invoked inside findForkStepForParallelStep
	instanceID := int64(100)
	store.EXPECT().GetInstance(mock.Anything, instanceID).
		Return(&WorkflowInstance{ID: instanceID, WorkflowID: def.ID}, nil).Maybe()
	store.EXPECT().GetWorkflowDefinition(mock.Anything, def.ID).Return(def, nil).Maybe()

	// Step T has no Next and is within fork branch -> terminal
	got := engine.isTerminalStepInForkBranch(ctx, instanceID, "T", def)
	assert.True(t, got)
}

func Test_isTerminalStepInForkBranch_NotTerminal_HasNext(t *testing.T) {
	ctx := context.Background()
	def := buildForkJoinDef()

	engine, store := newTestEngineWithStore(t)

	instanceID := int64(101)
	store.EXPECT().GetInstance(mock.Anything, instanceID).
		Return(&WorkflowInstance{ID: instanceID, WorkflowID: def.ID}, nil).Maybe()
	store.EXPECT().GetWorkflowDefinition(mock.Anything, def.ID).Return(def, nil).Maybe()

	// A1 has Next -> not terminal even though in fork branch
	got := engine.isTerminalStepInForkBranch(ctx, instanceID, "A1", def)
	assert.False(t, got)
}

func Test_isTerminalStepInForkBranch_JoinStep_False(t *testing.T) {
	ctx := context.Background()
	def := buildForkJoinDef()

	engine, _ := newTestEngineWithStore(t)

	instanceID := int64(102)
	// Join step should be false regardless of store calls
	got := engine.isTerminalStepInForkBranch(ctx, instanceID, "J", def)
	assert.False(t, got)
}

func Test_findJoinStepForForkBranch_FindsJoin(t *testing.T) {
	ctx := context.Background()
	def := buildForkJoinDef()

	engine, store := newTestEngineWithStore(t)

	instanceID := int64(103)
	store.EXPECT().GetInstance(mock.Anything, instanceID).
		Return(&WorkflowInstance{ID: instanceID, WorkflowID: def.ID}, nil).Maybe()
	store.EXPECT().GetWorkflowDefinition(mock.Anything, def.ID).Return(def, nil).Maybe()

	join, err := engine.findJoinStepForForkBranch(ctx, instanceID, "T", def)
	assert.NoError(t, err)
	assert.Equal(t, "J", join)
}

func Test_findJoinStepForForkBranch_NotInFork_ReturnsEmpty(t *testing.T) {
	ctx := context.Background()
	// A simple linear definition without fork
	def := &WorkflowDefinition{
		ID:   "wf2",
		Name: "wf2",
		Definition: GraphDefinition{
			Start: "S1",
			Steps: map[string]*StepDefinition{
				"S1": {Name: "S1", Type: StepTypeTask, Next: []string{"S2"}, Prev: rootStepName},
				"S2": {Name: "S2", Type: StepTypeTask, Prev: "S1"},
			},
		},
	}

	engine, store := newTestEngineWithStore(t)

	instanceID := int64(104)
	store.EXPECT().GetInstance(mock.Anything, instanceID).
		Return(&WorkflowInstance{ID: instanceID, WorkflowID: def.ID}, nil).Maybe()
	store.EXPECT().GetWorkflowDefinition(mock.Anything, def.ID).Return(def, nil).Maybe()

	join, err := engine.findJoinStepForForkBranch(ctx, instanceID, "S2", def)
	assert.NoError(t, err)
	assert.Equal(t, "", join)
}

func Test_findConditionStepInBranch_FindsCondition(t *testing.T) {
	// Build chain: Cond -> X -> Y -> Z; expect to find Cond from Z
	def := &WorkflowDefinition{
		ID:   "wf3",
		Name: "wf3",
		Definition: GraphDefinition{
			Start: "Cond",
			Steps: map[string]*StepDefinition{
				"Cond": {Name: "Cond", Type: StepTypeCondition, Next: []string{"X"}, Prev: rootStepName},
				"X":    {Name: "X", Type: StepTypeTask, Next: []string{"Y"}, Prev: "Cond"},
				"Y":    {Name: "Y", Type: StepTypeTask, Next: []string{"Z"}, Prev: "X"},
				"Z":    {Name: "Z", Type: StepTypeTask, Prev: "Y"},
			},
		},
	}

	engine, _ := newTestEngineWithStore(t)

	stepDef := def.Definition.Steps["Z"]
	got := engine.findConditionStepInBranch(stepDef, def)
	assert.Equal(t, "Cond", got)
}

func Test_findConditionStepInBranch_NotFound(t *testing.T) {
	def := &WorkflowDefinition{
		ID:   "wf4",
		Name: "wf4",
		Definition: GraphDefinition{
			Start: "A",
			Steps: map[string]*StepDefinition{
				"A": {Name: "A", Type: StepTypeTask, Next: []string{"B"}, Prev: rootStepName},
				"B": {Name: "B", Type: StepTypeTask, Prev: "A"},
			},
		},
	}

	engine, _ := newTestEngineWithStore(t)

	stepDef := def.Definition.Steps["B"]
	got := engine.findConditionStepInBranch(stepDef, def)
	assert.Equal(t, "", got)
}
