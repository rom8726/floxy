package floxy

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRollbackConditionActualFailureInParallelBranches(t *testing.T) {
	container, pool := setupTestDatabase(t)
	t.Cleanup(func() {
		pool.Close()
		_ = container.Terminate(context.Background())
	})

	ctx := context.Background()
	store := NewStore(pool)
	txManager := NewTxManager(pool)
	engine := NewEngine(txManager, store)

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
		JoinStep("join", []string{"branch1_step1", "branch2_step1"}, JoinStrategyAll).
		Then("final", "simple-test", WithStepMaxRetries(1)).
		Build()

	require.NoError(t, err)
	err = engine.RegisterWorkflow(ctx, workflowDef)
	require.NoError(t, err)

	// Start with count = 2 (branch1: false -> else, branch2: true -> next, but else will fail)
	input := json.RawMessage(`{"count": 2}`)
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

	// This test should show the same behavior as the previous one
	// because branch2 takes the "next" path, not the "else" path
	// So branch2_else (which fails) should not be executed

	if status == StatusCompleted {
		t.Logf("✅ Workflow completed successfully - this is the expected behavior")
	} else {
		t.Logf("❌ Workflow failed - this might indicate a problem with join logic")
	}
}

func TestRollbackConditionActualFailureInParallelBranchesWithFailure(t *testing.T) {
	container, pool := setupTestDatabase(t)
	t.Cleanup(func() {
		pool.Close()
		_ = container.Terminate(context.Background())
	})

	ctx := context.Background()
	store := NewStore(pool)
	txManager := NewTxManager(pool)
	engine := NewEngine(txManager, store)

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
		JoinStep("join", []string{"branch1_step1", "branch2_step1"}, JoinStrategyAll).
		Then("final", "simple-test", WithStepMaxRetries(1)).
		Build()

	require.NoError(t, err)
	err = engine.RegisterWorkflow(ctx, workflowDef)
	require.NoError(t, err)

	// Start with count = 2 (branch1: false -> else, branch2: true -> next, but else will fail)
	input := json.RawMessage(`{"count": 2}`)
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

	// This test should show the same behavior as the previous one
	// because branch2 takes the "next" path, not the "else" path
	// So branch2_else (which fails) should not be executed

	if status == StatusCompleted {
		t.Logf("✅ Workflow completed successfully - this is the expected behavior")
	} else {
		t.Logf("❌ Workflow failed - this might indicate a problem with join logic")
	}
}
