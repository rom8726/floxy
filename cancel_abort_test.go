package floxy

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCancelWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	store, txManager, cleanup := setupTestStore(t)
	t.Cleanup(cleanup)

	ctx := context.Background()
	engine := NewEngine(nil,
		WithEngineStore(store),
		WithEngineTxManager(txManager),
	)
	defer engine.Shutdown()

	// Register handlers
	engine.RegisterHandler(&SimpleTestHandler{})
	engine.RegisterHandler(&SlowTestHandler{})

	// Create workflow: start -> process -> final
	workflowDef, err := NewBuilder("cancel_test", 1).
		Step("start", "simple-test", WithStepMaxRetries(1)).
		Then("process", "slow-test", WithStepMaxRetries(1)).
		Then("final", "simple-test", WithStepMaxRetries(1)).
		Build()

	require.NoError(t, err)
	err = engine.RegisterWorkflow(ctx, workflowDef)
	require.NoError(t, err)

	// Start workflow
	input := json.RawMessage(`{"task_id": "TASK-001", "status": "pending"}`)
	instanceID, err := engine.Start(ctx, "cancel_test-v1", input)
	require.NoError(t, err)

	// Process first step
	empty, err := engine.ExecuteNext(ctx, "worker1")
	require.NoError(t, err)
	require.False(t, empty)

	// Wait a bit to ensure the workflow is running
	time.Sleep(100 * time.Millisecond)

	// Cancel the workflow
	requestedBy := "admin@company.com"
	reason := "User requested cancellation"
	err = engine.CancelWorkflow(ctx, instanceID, requestedBy, reason)
	require.NoError(t, err)

	time.Sleep(time.Second)
	_, _ = engine.ExecuteNext(ctx, "worker1")

	// Check final status - should be cancelled
	finalStatus, err := engine.GetStatus(ctx, instanceID)
	require.NoError(t, err)
	assert.Equal(t, StatusCancelled, finalStatus)

	// Check steps
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

	// Verify that start step was rolled back due to cancellation
	assert.Contains(t, stepNames, "start")
	assert.Equal(t, StepStatusRolledBack, stepStatuses["start"])

	// Other steps should be skipped due to cancellation
	if len(steps) > 1 {
		for _, step := range steps[1:] {
			assert.Equal(t, StepStatusSkipped, step.Status, "Step %s should be skipped", step.StepName)
		}
	}
}

func TestCancelWorkflowTerminalState(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	store, txManager, cleanup := setupTestStore(t)
	t.Cleanup(cleanup)

	ctx := context.Background()
	engine := NewEngine(nil,
		WithEngineStore(store),
		WithEngineTxManager(txManager),
	)
	defer engine.Shutdown()

	// Register handlers
	engine.RegisterHandler(&SimpleTestHandler{})

	// Create simple workflow
	workflowDef, err := NewBuilder("cancel_terminal_test", 1).
		Step("start", "simple-test", WithStepMaxRetries(1)).
		Build()

	require.NoError(t, err)
	err = engine.RegisterWorkflow(ctx, workflowDef)
	require.NoError(t, err)

	// Start workflow
	input := json.RawMessage(`{"task_id": "TASK-002", "status": "pending"}`)
	instanceID, err := engine.Start(ctx, "cancel_terminal_test-v1", input)
	require.NoError(t, err)

	// Complete the workflow
	empty, err := engine.ExecuteNext(ctx, "worker1")
	require.NoError(t, err)
	require.False(t, empty)

	// Wait for completion
	time.Sleep(100 * time.Millisecond)

	// Try to cancel completed workflow - should fail
	requestedBy := "admin@company.com"
	reason := "Attempt to cancel completed workflow"
	err = engine.CancelWorkflow(ctx, instanceID, requestedBy, reason)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already in terminal state")

	time.Sleep(time.Second)
	_, _ = engine.ExecuteNext(ctx, "worker1")
	time.Sleep(time.Second)

	// Check status is still completed
	status, err := engine.GetStatus(ctx, instanceID)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, status)
}

func TestAbortWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	store, txManager, cleanup := setupTestStore(t)
	t.Cleanup(cleanup)

	ctx := context.Background()
	engine := NewEngine(nil,
		WithEngineStore(store),
		WithEngineTxManager(txManager),
	)
	defer engine.Shutdown()

	// Register handlers
	engine.RegisterHandler(&SimpleTestHandler{})
	engine.RegisterHandler(&SlowTestHandler{})

	// Create workflow: start -> process -> final
	workflowDef, err := NewBuilder("abort_test", 1).
		Step("start", "simple-test", WithStepMaxRetries(1)).
		Then("process", "slow-test", WithStepMaxRetries(1)).
		Then("final", "simple-test", WithStepMaxRetries(1)).
		Build()

	require.NoError(t, err)
	err = engine.RegisterWorkflow(ctx, workflowDef)
	require.NoError(t, err)

	// Start workflow
	input := json.RawMessage(`{"task_id": "TASK-003", "status": "pending"}`)
	instanceID, err := engine.Start(ctx, "abort_test-v1", input)
	require.NoError(t, err)

	// Process first step
	empty, err := engine.ExecuteNext(ctx, "worker1")
	require.NoError(t, err)
	require.False(t, empty)

	// Wait a bit to ensure the workflow is running
	time.Sleep(100 * time.Millisecond)

	// Abort the workflow
	requestedBy := "admin@company.com"
	reason := "Critical error detected"
	err = engine.AbortWorkflow(ctx, instanceID, requestedBy, reason)
	require.NoError(t, err)

	time.Sleep(time.Second)
	_, _ = engine.ExecuteNext(ctx, "worker1")

	// Check final status - should be aborted
	status, err := engine.GetStatus(ctx, instanceID)
	require.NoError(t, err)
	assert.Equal(t, StatusAborted, status)

	// Check steps
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

	// Verify that start step was completed and other steps were skipped
	assert.Contains(t, stepNames, "start")
	assert.Equal(t, StepStatusCompleted, stepStatuses["start"])

	// Other steps should be skipped due to abort
	if len(steps) > 1 {
		for _, step := range steps[1:] {
			assert.Equal(t, StepStatusSkipped, step.Status, "Step %s should be skipped", step.StepName)
		}
	}
}

func TestAbortWorkflowTerminalState(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	store, txManager, cleanup := setupTestStore(t)
	t.Cleanup(cleanup)

	ctx := context.Background()
	engine := NewEngine(nil,
		WithEngineStore(store),
		WithEngineTxManager(txManager),
	)
	defer engine.Shutdown()

	// Register handlers
	engine.RegisterHandler(&SimpleTestHandler{})

	// Create simple workflow
	workflowDef, err := NewBuilder("abort_terminal_test", 1).
		Step("start", "simple-test", WithStepMaxRetries(1)).
		Build()

	require.NoError(t, err)
	err = engine.RegisterWorkflow(ctx, workflowDef)
	require.NoError(t, err)

	// Start workflow
	input := json.RawMessage(`{"task_id": "TASK-004", "status": "pending"}`)
	instanceID, err := engine.Start(ctx, "abort_terminal_test-v1", input)
	require.NoError(t, err)

	// Complete the workflow
	empty, err := engine.ExecuteNext(ctx, "worker1")
	require.NoError(t, err)
	require.False(t, empty)

	// Wait for completion
	time.Sleep(100 * time.Millisecond)

	// Try to abort completed workflow - should fail
	requestedBy := "admin@company.com"
	reason := "Attempt to abort completed workflow"
	err = engine.AbortWorkflow(ctx, instanceID, requestedBy, reason)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already in terminal state")

	// Check status is still completed
	status, err := engine.GetStatus(ctx, instanceID)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, status)
}

func TestCancelWorkflowWithCompensation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	store, txManager, cleanup := setupTestStore(t)
	t.Cleanup(cleanup)

	ctx := context.Background()
	engine := NewEngine(nil,
		WithEngineStore(store),
		WithEngineTxManager(txManager),
	)
	defer engine.Shutdown()

	// Register handlers
	engine.RegisterHandler(&SimpleTestHandler{})
	engine.RegisterHandler(&CompensationTestHandler{})

	// Create workflow with compensation: start -> process (with compensation) -> final
	workflowDef, err := NewBuilder("cancel_compensation_test", 1).
		Step("start", "simple-test", WithStepMaxRetries(1)).
		Step("process", "compensation-test", WithStepMaxRetries(1)).
		OnFailure("compensate", "compensation-test", WithStepMaxRetries(1)).
		Then("final", "simple-test", WithStepMaxRetries(1)).
		Build()

	require.NoError(t, err)
	err = engine.RegisterWorkflow(ctx, workflowDef)
	require.NoError(t, err)

	// Start workflow
	input := json.RawMessage(`{"task_id": "TASK-005", "status": "pending"}`)
	instanceID, err := engine.Start(ctx, "cancel_compensation_test-v1", input)
	require.NoError(t, err)

	// Process first step
	empty, err := engine.ExecuteNext(ctx, "worker1")
	require.NoError(t, err)
	require.False(t, empty)

	// Wait a bit to ensure the workflow is running
	time.Sleep(100 * time.Millisecond)

	// Cancel the workflow - this should trigger compensation
	requestedBy := "admin@company.com"
	reason := "User requested cancellation with compensation"
	err = engine.CancelWorkflow(ctx, instanceID, requestedBy, reason)
	require.NoError(t, err)

	time.Sleep(time.Second)
	_, _ = engine.ExecuteNext(ctx, "worker1")

	// Check final status - should be cancelled
	status, err := engine.GetStatus(ctx, instanceID)
	require.NoError(t, err)
	assert.Equal(t, StatusCancelled, status)

	// Check steps
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

	// Verify that start step was rolled back due to cancellation
	assert.Contains(t, stepNames, "start")
	assert.Equal(t, StepStatusRolledBack, stepStatuses["start"])

	// Process step should be skipped due to cancellation
	if stepStatuses["process"] != "" {
		assert.Equal(t, StepStatusSkipped, stepStatuses["process"])
	}
}

// Test handler for slow operations
type SlowTestHandler struct{}

func (h *SlowTestHandler) Name() string { return "slow-test" }

func (h *SlowTestHandler) Execute(ctx context.Context, stepCtx StepContext, input json.RawMessage) (json.RawMessage, error) {
	// Simulate slow operation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(3 * time.Second):
	}

	var data map[string]any
	if err := json.Unmarshal(input, &data); err != nil {
		return nil, err
	}

	data["status"] = "processed"
	data["processed_at"] = time.Now().Unix()

	return json.Marshal(data)
}

// Test handler for compensation operations
type CompensationTestHandler struct{}

func (h *CompensationTestHandler) Name() string { return "compensation-test" }

func (h *CompensationTestHandler) Execute(ctx context.Context, stepCtx StepContext, input json.RawMessage) (json.RawMessage, error) {
	// Check if this is a compensation call
	variables := stepCtx.CloneData()
	if reason, exists := variables["reason"]; exists && reason == "compensation" {
		// This is a compensation call
		var data map[string]any
		if err := json.Unmarshal(input, &data); err != nil {
			return nil, err
		}

		data["status"] = "compensated"
		data["compensated_at"] = time.Now().Unix()
		data["reason"] = "compensation"

		return json.Marshal(data)
	}

	// Regular execution
	var data map[string]any
	if err := json.Unmarshal(input, &data); err != nil {
		return nil, err
	}

	data["status"] = "processed"
	data["processed_at"] = time.Now().Unix()

	return json.Marshal(data)
}
