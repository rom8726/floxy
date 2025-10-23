package floxy

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRollbackWithConditionSteps(t *testing.T) {
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
	engine.RegisterHandler(&RollbackTestHandler{})
	engine.RegisterHandler(&FailingHandler{})

	// Create workflow with condition in parallel branch
	workflowDef, err := NewBuilder("rollback_condition", 1).
		Step("start", "rollback-test", WithStepMaxRetries(1)).
		Fork("parallel_branch", func(branch1 *Builder) {
			branch1.Step("branch1_step1", "rollback-test", WithStepMaxRetries(1)).
				Condition("branch1_condition", "{{ gt .count 5 }}", func(elseBranch *Builder) {
					elseBranch.Step("branch1_else", "rollback-test", WithStepMaxRetries(1))
				}).
				Then("branch1_next", "rollback-test", WithStepMaxRetries(1))
		}, func(branch2 *Builder) {
			branch2.Step("branch2_step1", "rollback-test", WithStepMaxRetries(1)).
				Condition("branch2_condition", "{{ lt .count 3 }}", func(elseBranch *Builder) {
					elseBranch.Step("branch2_else", "rollback-test", WithStepMaxRetries(1))
				}).
				Then("branch2_next", "failing-handler", WithStepMaxRetries(1)) // This will fail
		}).
		JoinStep("join", []string{"branch1_step1", "branch2_step1"}, JoinStrategyAll).
		Then("final", "rollback-test", WithStepMaxRetries(1)).
		Build()

	require.NoError(t, err)
	err = engine.RegisterWorkflow(ctx, workflowDef)
	require.NoError(t, err)

	// Start with count = 2 (branch1: false -> else, branch2: true -> next)
	input := json.RawMessage(`{"count": 2}`)
	instanceID, err := engine.Start(ctx, "rollback_condition-v1", input)
	require.NoError(t, err)

	// Process workflow until failure
	for i := 0; i < 20; i++ {
		empty, err := engine.ExecuteNext(ctx, "worker1")
		require.NoError(t, err)
		if empty {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Check final status - should be failed due to failing-handler
	status, err := engine.GetStatus(ctx, instanceID)
	require.NoError(t, err)
	assert.Equal(t, StatusFailed, status)

	// Check steps to see which ones were executed and which need rollback
	steps, err := engine.GetSteps(ctx, instanceID)
	require.NoError(t, err)

	stepNames := make([]string, len(steps))
	stepStatuses := make(map[string]StepStatus)
	for i, step := range steps {
		stepNames[i] = step.StepName
		stepStatuses[step.StepName] = step.Status
	}

	t.Logf("Executed steps: %v", stepNames)
	t.Logf("Step statuses: %v", stepStatuses)

	// Expected execution path:
	// - start (completed)
	// - parallel_branch (completed)
	// - branch1_step1 (completed)
	// - branch1_condition (completed, false -> else)
	// - branch1_else (completed)
	// - branch2_step1 (completed)
	// - branch2_condition (completed, true -> next)
	// - branch2_next (failed - this triggers rollback)
	// - branch1_next (should NOT be executed)
	// - branch2_else (should NOT be executed)

	// The issue: join and final should NOT be executed if branch2_next fails
	// But they are being executed, which means the rollback logic is not working correctly

	// Verify that branch1_next and branch2_else were not executed
	assert.NotContains(t, stepNames, "branch1_next")
	assert.NotContains(t, stepNames, "branch2_else")

	// Verify that the executed steps are present
	assert.Contains(t, stepNames, "start")
	assert.Contains(t, stepNames, "parallel_branch")
	assert.Contains(t, stepNames, "branch1_step1")
	assert.Contains(t, stepNames, "branch1_condition")
	assert.Contains(t, stepNames, "branch1_else")
	assert.Contains(t, stepNames, "branch2_step1")
	assert.Contains(t, stepNames, "branch2_condition")
	assert.Contains(t, stepNames, "branch2_next")

	// The problem: join and final should not be executed if branch2_next fails
	// This indicates that the rollback logic is not properly handling condition steps
	t.Logf("PROBLEM: join and final were executed even though branch2_next failed")
	t.Logf("This means the rollback logic needs to be updated for condition steps")
}

type RollbackTestHandler struct{}

func (h *RollbackTestHandler) Name() string { return "rollback-test" }

func (h *RollbackTestHandler) Execute(ctx context.Context, stepCtx StepContext, input json.RawMessage) (json.RawMessage, error) {
	// Just pass through the input
	return input, nil
}

type FailingHandler struct{}

func (h *FailingHandler) Name() string { return "failing-handler" }

func (h *FailingHandler) Execute(ctx context.Context, stepCtx StepContext, input json.RawMessage) (json.RawMessage, error) {
	// Always fail
	return nil, assert.AnError
}
