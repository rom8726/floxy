package floxy

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Helper to build a fork/join where Join.WaitFor contains direct parallel steps
func buildForkJoinDirectWaitForDef() *WorkflowDefinition {
	def := &WorkflowDefinition{
		ID:      "wf_parallel",
		Name:    "wf_parallel",
		Version: 1,
		Definition: GraphDefinition{
			Start: "F",
			Steps: map[string]*StepDefinition{},
		},
	}

	// Fork F with parallel A, B and Next to J
	def.Definition.Steps["F"] = &StepDefinition{
		Name:     "F",
		Type:     StepTypeFork,
		Parallel: []string{"A", "B"},
		Next:     []string{"J"},
		Prev:     rootStepName,
	}

	// Branch A: A -> A1
	def.Definition.Steps["A"] = &StepDefinition{
		Name: "A",
		Type: StepTypeTask,
		Next: []string{"A1"},
		Prev: "F",
	}
	def.Definition.Steps["A1"] = &StepDefinition{
		Name: "A1",
		Type: StepTypeTask,
		Prev: "A",
	}

	// Branch B: B -> B1
	def.Definition.Steps["B"] = &StepDefinition{
		Name: "B",
		Type: StepTypeTask,
		Next: []string{"B1"},
		Prev: "F",
	}
	def.Definition.Steps["B1"] = &StepDefinition{
		Name: "B1",
		Type: StepTypeTask,
		Prev: "B",
	}

	// Join J waits for the direct parallel steps (A, B)
	def.Definition.Steps["J"] = &StepDefinition{
		Name:         "J",
		Type:         StepTypeJoin,
		JoinStrategy: JoinStrategyAll,
		WaitFor:      []string{"A", "B"},
		Prev:         "F",
	}

	return def
}

func Test_isStepDescendantOf_LinearAndNegative(t *testing.T) {
	// Build linear chain: A -> B -> C
	def := &WorkflowDefinition{
		ID:   "wf_desc",
		Name: "wf_desc",
		Definition: GraphDefinition{
			Start: "A",
			Steps: map[string]*StepDefinition{
				"A": {Name: "A", Type: StepTypeTask, Next: []string{"B"}, Prev: rootStepName},
				"B": {Name: "B", Type: StepTypeTask, Next: []string{"C"}, Prev: "A"},
				"C": {Name: "C", Type: StepTypeTask, Prev: "B"},
			},
		},
	}

	engine, _ := newTestEngineWithStore(t)

	assert.True(t, engine.isStepDescendantOf("C", "A", def))  // C is descendant of A
	assert.True(t, engine.isStepDescendantOf("A", "A", def))  // step is ancestor of itself
	assert.False(t, engine.isStepDescendantOf("A", "C", def)) // A is not descendant of C
	assert.False(t, engine.isStepDescendantOf("X", "A", def)) // unknown step
}

func Test_isStepDescendantOf_CycleGuard(t *testing.T) {
	// Create a cycle: A <- B <- C and set A.Prev = C to simulate accidental loop
	def := &WorkflowDefinition{
		ID:   "wf_cycle",
		Name: "wf_cycle",
		Definition: GraphDefinition{
			Start: "A",
			Steps: map[string]*StepDefinition{
				"A": {Name: "A", Type: StepTypeTask, Next: []string{"B"}, Prev: "C"},
				"B": {Name: "B", Type: StepTypeTask, Next: []string{"C"}, Prev: "A"},
				"C": {Name: "C", Type: StepTypeTask, Prev: "B"},
			},
		},
	}

	engine, _ := newTestEngineWithStore(t)

	// Should terminate and return false when searching an unrelated ancestor
	assert.False(t, engine.isStepDescendantOf("B", "X", def))
	// And true for valid ancestor within the cycle path before detection
	assert.True(t, engine.isStepDescendantOf("C", "A", def))
}

func Test_isStepInParallelBranch_DirectAndDescendant(t *testing.T) {
	def := buildForkJoinDef() // Parallel: A, B
	engine, _ := newTestEngineWithStore(t)

	fork := def.Definition.Steps["F"]

	assert.True(t, engine.isStepInParallelBranch("A", fork, def))  // direct parallel step
	assert.True(t, engine.isStepInParallelBranch("A1", fork, def)) // descendant of A

	// Add an external step not in parallel branches
	def.Definition.Steps["X"] = &StepDefinition{Name: "X", Type: StepTypeTask, Prev: rootStepName}
	assert.False(t, engine.isStepInParallelBranch("X", fork, def))
}

func Test_findForkStepForParallelStep_FindsOnlyDirectParallel(t *testing.T) {
	ctx := context.Background()
	def := buildForkJoinDef()
	engine, store := newTestEngineWithStore(t)

	instanceID := int64(200)
	store.EXPECT().GetInstance(mock.Anything, instanceID).
		Return(&WorkflowInstance{ID: instanceID, WorkflowID: def.ID}, nil)
	store.EXPECT().GetWorkflowDefinition(mock.Anything, def.ID).Return(def, nil)

	// Direct parallel step -> finds fork
	fork := engine.findForkStepForParallelStep(ctx, instanceID, "A")
	assert.Equal(t, "F", fork)

	// Descendant step (A1) is not listed in Parallel -> not found
	fork2 := engine.findForkStepForParallelStep(ctx, instanceID, "A1")
	assert.Equal(t, "", fork2)
}

func Test_hasPendingStepsInParallelBranches_Positive(t *testing.T) {
	ctx := context.Background()
	def := buildForkJoinDirectWaitForDef() // Join.WaitFor = [A, B]
	engine, store := newTestEngineWithStore(t)

	instanceID := int64(300)
	store.EXPECT().GetInstance(mock.Anything, instanceID).
		Return(&WorkflowInstance{ID: instanceID, WorkflowID: def.ID}, nil)
	store.EXPECT().GetWorkflowDefinition(mock.Anything, def.ID).Return(def, nil)

	join := def.Definition.Steps["J"]

	// All steps in the instance (subset is enough)
	allSteps := []WorkflowStep{
		{InstanceID: instanceID, StepName: "A", StepType: StepTypeTask, Status: StepStatusCompleted},
		{InstanceID: instanceID, StepName: "B", StepType: StepTypeTask, Status: StepStatusCompleted},
		{InstanceID: instanceID, StepName: "A1", StepType: StepTypeTask, Status: StepStatusPending}, // pending extra in branch
	}

	// Expect true because A1 is pending in a parallel branch and not present in WaitFor
	got := engine.hasPendingStepsInParallelBranches(ctx, instanceID, join, allSteps)
	assert.True(t, got)
}

func Test_hasPendingStepsInParallelBranches_Negative_NoExtraPending(t *testing.T) {
	ctx := context.Background()
	def := buildForkJoinDirectWaitForDef()
	engine, store := newTestEngineWithStore(t)

	instanceID := int64(301)
	store.EXPECT().GetInstance(mock.Anything, instanceID).
		Return(&WorkflowInstance{ID: instanceID, WorkflowID: def.ID}, nil)
	store.EXPECT().GetWorkflowDefinition(mock.Anything, def.ID).Return(def, nil)

	join := def.Definition.Steps["J"]
	allSteps := []WorkflowStep{
		{InstanceID: instanceID, StepName: "A", StepType: StepTypeTask, Status: StepStatusCompleted},
		{InstanceID: instanceID, StepName: "B", StepType: StepTypeTask, Status: StepStatusRunning}, // in WaitFor -> ignored
	}

	got := engine.hasPendingStepsInParallelBranches(ctx, instanceID, join, allSteps)
	assert.False(t, got)
}

func Test_determineExecutedBranch(t *testing.T) {
	engine, _ := newTestEngineWithStore(t)

	// Condition-like definition stub: stepDef with Next and Else
	stepDef := &StepDefinition{
		Name: "Cond",
		Type: StepTypeCondition,
		Next: []string{"X"},
		Else: "Y",
	}

	// Case 1: Next branch executed
	stepMap := map[string]*WorkflowStep{
		"X": {StepName: "X", Status: StepStatusCompleted},
		"Y": {StepName: "Y", Status: StepStatusPending},
	}
	assert.Equal(t, "next", engine.determineExecutedBranch(stepDef, stepMap))

	// Case 2: Else branch executed (Failed also counts)
	stepMap = map[string]*WorkflowStep{
		"X": {StepName: "X", Status: StepStatusPending},
		"Y": {StepName: "Y", Status: StepStatusFailed},
	}
	assert.Equal(t, "else", engine.determineExecutedBranch(stepDef, stepMap))

	// Case 3: None executed
	stepMap = map[string]*WorkflowStep{
		"X": {StepName: "X", Status: StepStatusPending},
		"Y": {StepName: "Y", Status: StepStatusPending},
	}
	assert.Equal(t, "", engine.determineExecutedBranch(stepDef, stepMap))
}
