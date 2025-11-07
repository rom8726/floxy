package floxy

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRollbackConditionActualFailureInParallelBranches(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	store, txManager, cleanup := setupTestStore(t)
	t.Cleanup(cleanup)

	ctx := context.Background()
	engine := NewEngine(nil,
		WithEngineStore(store),
		WithEngineTxManager(txManager),
		WithEngineCancelInterval(time.Minute),
	)
	defer engine.Shutdown()

	// Register handlers
	engine.RegisterHandler(&SimpleTestHandler{})
	engine.RegisterHandler(&FailingHandler{})

	// Create workflow with condition in parallel branch where one branch fails
	workflowDef, err := NewBuilder("rollback_actual_failure", 1).
		Step("start", "simple-test", WithStepMaxRetries(1)).
		Fork("parallel_branch", func(branch1 *Builder) {
			branch1.Step("branch1_step1", "simple-test", WithStepMaxRetries(1)).
				Condition("branch1_condition", "{{ gt .count 5 }}", func(elseBranch *Builder) {
					elseBranch.Step("branch1_else", "simple-test", WithStepMaxRetries(1))
				}).
				Then("branch1_next", "simple-test", WithStepMaxRetries(1))
		}, func(branch2 *Builder) {
			branch2.Step("branch2_step1", "simple-test", WithStepMaxRetries(1)).
				Condition("branch2_condition", "{{ lt .count 3 }}", func(elseBranch *Builder) {
					elseBranch.Step("branch2_else", "failing-handler", WithStepMaxRetries(1)) // This will fail
				}).
				Then("branch2_next", "simple-test", WithStepMaxRetries(1))
		}).
		Join("join", JoinStrategyAll).
		Then("final", "simple-test", WithStepMaxRetries(1)).
		Build()

	require.NoError(t, err)
	err = engine.RegisterWorkflow(ctx, workflowDef)
	require.NoError(t, err)

	// Start with count = 6 (branch1: true -> next, branch2: false -> else which fails)
	input := json.RawMessage(`{"count": 6}`)
	instanceID, err := engine.Start(ctx, "rollback_actual_failure-v1", input)
	require.NoError(t, err)

	// Process workflow until completion or failure
	for i := 0; i < 20; i++ {
		empty, err := engine.ExecuteNext(ctx, "worker1")
		require.NoError(t, err)
		if empty {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Check final status
	status, err := engine.GetStatus(ctx, instanceID)
	require.NoError(t, err)

	// Check steps
	steps, err := engine.GetSteps(ctx, instanceID)
	require.NoError(t, err)

	stepNames := make([]string, len(steps))
	stepStatuses := make(map[string]StepStatus)
	for i, step := range steps {
		stepNames[i] = step.StepName
		stepStatuses[step.StepName] = step.Status
	}

	t.Logf("Final status: %s", status)
	t.Logf("Executed steps: %v", stepNames)
	t.Logf("Step statuses: %v", stepStatuses)

	// Expected execution path with count = 6:
	// - start (completed)
	// - parallel_branch (completed)
	// - branch1_step1 (completed)
	// - branch1_condition (completed, true -> next)
	// - branch1_next (completed)
	// - branch2_step1 (completed)
	// - branch2_condition (completed, false -> else)
	// - branch2_else (failed) -> triggers rollback
	// - All steps should be rolled_back due to failure

	// The key test: verify that when one branch fails, the entire workflow fails
	// and all steps are rolled back

	// Verify that the workflow failed
	require.Equal(t, StatusFailed, status, "Workflow should fail when one branch fails")

	// Verify that all steps are rolled back
	for _, step := range steps {
		require.Equal(t, StepStatusRolledBack, step.Status, "All steps should be rolled back")
	}
}

func TestRollbackConditionActualFailureInParallelBranchesWithFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	store, txManager, cleanup := setupTestStore(t)
	t.Cleanup(cleanup)

	ctx := context.Background()
	engine := NewEngine(nil,
		WithEngineStore(store),
		WithEngineTxManager(txManager),
		WithEngineCancelInterval(time.Minute),
	)
	defer engine.Shutdown()

	// Register handlers
	engine.RegisterHandler(&SimpleTestHandler{})
	engine.RegisterHandler(&FailingHandler{})

	// Create workflow with condition in parallel branch where one branch fails
	workflowDef, err := NewBuilder("rollback_actual_failure2", 1).
		Step("start", "simple-test", WithStepMaxRetries(1)).
		Fork("parallel_branch", func(branch1 *Builder) {
			branch1.Step("branch1_step1", "simple-test", WithStepMaxRetries(1)).
				Condition("branch1_condition", "{{ gt .count 5 }}", func(elseBranch *Builder) {
					elseBranch.Step("branch1_else", "simple-test", WithStepMaxRetries(1))
				}).
				Then("branch1_next", "simple-test", WithStepMaxRetries(1))
		}, func(branch2 *Builder) {
			branch2.Step("branch2_step1", "simple-test", WithStepMaxRetries(1)).
				Condition("branch2_condition", "{{ lt .count 3 }}", func(elseBranch *Builder) {
					elseBranch.Step("branch2_else", "failing-handler", WithStepMaxRetries(1)) // This will fail
				}).
				Then("branch2_next", "simple-test", WithStepMaxRetries(1))
		}).
		Join("join", JoinStrategyAll).
		Then("final", "simple-test", WithStepMaxRetries(1)).
		Build()

	require.NoError(t, err)
	err = engine.RegisterWorkflow(ctx, workflowDef)
	require.NoError(t, err)

	// Start with count = 6 (branch1: true -> next, branch2: false -> else which fails)
	input := json.RawMessage(`{"count": 6}`)
	instanceID, err := engine.Start(ctx, "rollback_actual_failure2-v1", input)
	require.NoError(t, err)

	// Process workflow until completion or failure
	for i := 0; i < 20; i++ {
		empty, err := engine.ExecuteNext(ctx, "worker1")
		require.NoError(t, err)
		if empty {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Check final status
	status, err := engine.GetStatus(ctx, instanceID)
	require.NoError(t, err)

	// Check steps
	steps, err := engine.GetSteps(ctx, instanceID)
	require.NoError(t, err)

	stepNames := make([]string, len(steps))
	stepStatuses := make(map[string]StepStatus)
	for i, step := range steps {
		stepNames[i] = step.StepName
		stepStatuses[step.StepName] = step.Status
	}

	t.Logf("Final status: %s", status)
	t.Logf("Executed steps: %v", stepNames)
	t.Logf("Step statuses: %v", stepStatuses)

	// Expected execution path with count = 6:
	// - start (completed)
	// - parallel_branch (completed)
	// - branch1_step1 (completed)
	// - branch1_condition (completed, true -> next)
	// - branch1_next (completed)
	// - branch2_step1 (completed)
	// - branch2_condition (completed, false -> else)
	// - branch2_else (failed) -> triggers rollback
	// - All steps should be rolled_back due to failure

	// The key test: verify that when one branch fails, the entire workflow fails
	// and all steps are rolled back

	// Verify that the workflow failed
	require.Equal(t, StatusFailed, status, "Workflow should fail when one branch fails")

	// Verify that all steps are rolled back
	for _, step := range steps {
		require.Equal(t, StepStatusRolledBack, step.Status, "All steps should be rolled back")
	}
}
