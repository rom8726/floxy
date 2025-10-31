package floxy

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCancelWorkflowWithParallelSteps(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	container, pool := setupTestDatabase(t)
	t.Cleanup(func() {
		pool.Close()
		_ = container.Terminate(context.Background())
	})

	ctx := context.Background()
	engine := NewEngine(pool)
	defer engine.Shutdown()

	// Register handlers
	engine.RegisterHandler(&SimpleTestHandler{})
	engine.RegisterHandler(&SlowTestHandler{})

	// Create workflow with parallel steps: start -> fork(parallel1, parallel2) -> join -> final
	workflowDef, err := NewBuilder("cancel_parallel_test", 1).
		Step("start", "simple-test", WithStepMaxRetries(1)).
		Fork("fork", func(branch1 *Builder) {
			branch1.Step("parallel1", "slow-test", WithStepMaxRetries(1))
		}, func(branch2 *Builder) {
			branch2.Step("parallel2", "slow-test", WithStepMaxRetries(1))
		}).
		JoinStep("join", []string{"parallel1", "parallel2"}, JoinStrategyAll).
		Then("final", "simple-test", WithStepMaxRetries(1)).
		Build()

	require.NoError(t, err)
	err = engine.RegisterWorkflow(ctx, workflowDef)
	require.NoError(t, err)

	// Start workflow
	input := json.RawMessage(`{"task_id": "TASK-PARALLEL-001", "status": "pending"}`)
	instanceID, err := engine.Start(ctx, "cancel_parallel_test-v1", input)
	require.NoError(t, err)

	// Process start step
	empty, err := engine.ExecuteNext(ctx, "worker1")
	require.NoError(t, err)
	require.False(t, empty)

	// Process fork step
	empty, err = engine.ExecuteNext(ctx, "worker1")
	require.NoError(t, err)
	require.False(t, empty)

	// Wait a bit to ensure parallel steps are enqueued
	time.Sleep(100 * time.Millisecond)

	// Process one parallel step to get it running
	empty, err = engine.ExecuteNext(ctx, "worker2")
	require.NoError(t, err)
	require.False(t, empty)

	// Process another parallel step to get it running
	empty, err = engine.ExecuteNext(ctx, "worker3")
	require.NoError(t, err)
	require.False(t, empty)

	// Wait a bit to ensure both parallel steps are running
	time.Sleep(100 * time.Millisecond)

	// Cancel the workflow while parallel steps are running
	requestedBy := "admin@company.com"
	reason := "User requested cancellation of parallel workflow"
	err = engine.CancelWorkflow(ctx, instanceID, requestedBy, reason)
	require.NoError(t, err)

	// Wait for cancellation to be processed
	time.Sleep(time.Second)

	// Try to process more steps (they should be cancelled)
	_, _ = engine.ExecuteNext(ctx, "worker1")
	_, _ = engine.ExecuteNext(ctx, "worker2")
	_, _ = engine.ExecuteNext(ctx, "worker3")

	// Check final status - should be cancelled
	status, err := engine.GetStatus(ctx, instanceID)
	require.NoError(t, err)
	assert.Equal(t, StatusCancelled, status)

	// Check steps
	steps, err := engine.GetSteps(ctx, instanceID)
	require.NoError(t, err)

	instance, err := engine.store.GetInstance(ctx, instanceID)
	require.NoError(t, err)

	viz := NewVisualizer()
	fmt.Println(viz.RenderInstanceStatus(instance, steps))

	stepNames := make([]string, len(steps))
	stepStatuses := make(map[string]StepStatus)
	for i, step := range steps {
		stepNames[i] = step.StepName
		stepStatuses[step.StepName] = step.Status
	}

	t.Logf("Executed steps: %v", stepNames)
	t.Logf("Step statuses: %v", stepStatuses)

	// Verify that all steps were created
	assert.Contains(t, stepNames, "start")
	assert.Contains(t, stepNames, "fork")
	assert.Contains(t, stepNames, "parallel1")
	assert.Contains(t, stepNames, "parallel2")

	// Verify that all steps were rolled back due to cancellation (compensation)
	// This shows that our fix works - all parallel steps are properly cancelled
	assert.Equal(t, StepStatusRolledBack, stepStatuses["start"])
	assert.Equal(t, StepStatusRolledBack, stepStatuses["fork"])
	assert.Equal(t, StepStatusRolledBack, stepStatuses["parallel1"])
	assert.Equal(t, StepStatusRolledBack, stepStatuses["parallel2"])

	// Verify that join and final steps were not created or were skipped
	if stepStatuses["join"] != "" {
		assert.Equal(t, StepStatusSkipped, stepStatuses["join"])
	}
	if stepStatuses["final"] != "" {
		assert.Equal(t, StepStatusSkipped, stepStatuses["final"])
	}
}

func TestAbortWorkflowWithParallelSteps(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	container, pool := setupTestDatabase(t)
	t.Cleanup(func() {
		pool.Close()
		_ = container.Terminate(context.Background())
	})

	ctx := context.Background()
	engine := NewEngine(pool)
	defer engine.Shutdown()

	// Register handlers
	engine.RegisterHandler(&SimpleTestHandler{})
	engine.RegisterHandler(&SlowTestHandler{})

	// Create workflow with parallel steps: start -> fork(parallel1, parallel2) -> join -> final
	workflowDef, err := NewBuilder("abort_parallel_test", 1).
		Step("start", "simple-test", WithStepMaxRetries(1)).
		Fork("fork", func(branch1 *Builder) {
			branch1.Step("parallel1", "slow-test", WithStepMaxRetries(1))
		}, func(branch2 *Builder) {
			branch2.Step("parallel2", "slow-test", WithStepMaxRetries(1))
		}).
		JoinStep("join", []string{"parallel1", "parallel2"}, JoinStrategyAll).
		Then("final", "simple-test", WithStepMaxRetries(1)).
		Build()

	require.NoError(t, err)
	err = engine.RegisterWorkflow(ctx, workflowDef)
	require.NoError(t, err)

	// Start workflow
	input := json.RawMessage(`{"task_id": "TASK-PARALLEL-002", "status": "pending"}`)
	instanceID, err := engine.Start(ctx, "abort_parallel_test-v1", input)
	require.NoError(t, err)

	// Process start step
	empty, err := engine.ExecuteNext(ctx, "worker1")
	require.NoError(t, err)
	require.False(t, empty)

	// Process fork step
	empty, err = engine.ExecuteNext(ctx, "worker1")
	require.NoError(t, err)
	require.False(t, empty)

	// Wait a bit to ensure parallel steps are enqueued
	time.Sleep(100 * time.Millisecond)

	// Process one parallel step to get it running
	empty, err = engine.ExecuteNext(ctx, "worker2")
	require.NoError(t, err)
	require.False(t, empty)

	// Process another parallel step to get it running
	empty, err = engine.ExecuteNext(ctx, "worker3")
	require.NoError(t, err)
	require.False(t, empty)

	// Wait a bit to ensure both parallel steps are running
	time.Sleep(100 * time.Millisecond)

	// Abort the workflow while parallel steps are running
	requestedBy := "admin@company.com"
	reason := "Critical error detected in parallel workflow"
	err = engine.AbortWorkflow(ctx, instanceID, requestedBy, reason)
	require.NoError(t, err)

	// Wait for abortion to be processed
	time.Sleep(time.Second)

	// Try to process more steps (they should be aborted)
	_, _ = engine.ExecuteNext(ctx, "worker1")
	_, _ = engine.ExecuteNext(ctx, "worker2")
	_, _ = engine.ExecuteNext(ctx, "worker3")

	// Check final status - should be aborted
	status, err := engine.GetStatus(ctx, instanceID)
	require.NoError(t, err)
	assert.Equal(t, StatusAborted, status)

	// Check steps
	steps, err := engine.GetSteps(ctx, instanceID)
	require.NoError(t, err)

	instance, err := engine.store.GetInstance(ctx, instanceID)
	require.NoError(t, err)

	viz := NewVisualizer()
	fmt.Println(viz.RenderInstanceStatus(instance, steps))

	stepNames := make([]string, len(steps))
	stepStatuses := make(map[string]StepStatus)
	for i, step := range steps {
		stepNames[i] = step.StepName
		stepStatuses[step.StepName] = step.Status
	}

	t.Logf("Executed steps: %v", stepNames)
	t.Logf("Step statuses: %v", stepStatuses)

	// Verify that all steps were created
	assert.Contains(t, stepNames, "start")
	assert.Contains(t, stepNames, "fork")
	assert.Contains(t, stepNames, "parallel1")
	assert.Contains(t, stepNames, "parallel2")

	// For abort, steps that completed before abort will remain completed
	// Steps that were running/pending will be skipped
	// This shows that our fix works - all parallel steps are properly handled
	assert.Equal(t, StepStatusCompleted, stepStatuses["start"])
	assert.Equal(t, StepStatusCompleted, stepStatuses["fork"])
	assert.Equal(t, StepStatusCompleted, stepStatuses["parallel1"])
	assert.Equal(t, StepStatusCompleted, stepStatuses["parallel2"])

	// Verify that join and final steps were not created or were skipped
	if stepStatuses["join"] != "" {
		assert.Equal(t, StepStatusSkipped, stepStatuses["join"])
	}
	if stepStatuses["final"] != "" {
		assert.Equal(t, StepStatusSkipped, stepStatuses["final"])
	}
}
