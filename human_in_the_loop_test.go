package floxy

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHumanInTheLoopConfirmed(t *testing.T) {
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
	engine.RegisterHandler(&ApprovalTestHandler{})

	// Create workflow: start -> human-approval -> approve -> final
	workflowDef, err := NewBuilder("human_approval_confirmed", 1).
		Step("start", "simple-test", WithStepMaxRetries(1)).
		WaitHumanConfirm("human-approval", WithStepDelay(time.Millisecond*100)).
		Then("approve", "approval-test", WithStepMaxRetries(1)).
		Then("final", "simple-test", WithStepMaxRetries(1)).
		Build()

	require.NoError(t, err)
	err = engine.RegisterWorkflow(ctx, workflowDef)
	require.NoError(t, err)

	// Start workflow
	input := json.RawMessage(`{"document_id": "DOC-001", "status": "pending"}`)
	instanceID, err := engine.Start(ctx, "human_approval_confirmed-v1", input)
	require.NoError(t, err)

	// Process workflow until a human step is waiting
	var humanStepID int64
	for i := 0; i < 10; i++ {
		empty, err := engine.ExecuteNext(ctx, "worker1")
		require.NoError(t, err)
		if empty {
			break
		}
		time.Sleep(time.Millisecond * 100)

		// Check if human step is waiting for decision
		steps, err := engine.GetSteps(ctx, instanceID)
		require.NoError(t, err)
		for _, step := range steps {
			if step.StepName == "human-approval" && step.Status == StepStatusWaitingDecision {
				humanStepID = step.ID
				break
			}
		}
		if humanStepID != 0 {
			break
		}
	}

	// Verify human step is waiting
	require.NotZero(t, humanStepID, "Human step should be waiting for decision")

	// Make human decision - CONFIRMED
	comment := "Document approved for publication"
	err = engine.MakeHumanDecision(ctx, humanStepID, "manager@company.com", HumanDecisionConfirmed, &comment)
	require.NoError(t, err)
	time.Sleep(time.Second)

	// Continue processing workflow
	for i := 0; i < 3; i++ {
		empty, err := engine.ExecuteNext(ctx, "worker1")
		require.NoError(t, err)
		if empty {
			break
		}
		time.Sleep(time.Millisecond * 100)
	}

	// Check final status - should be completed
	status, err := engine.GetStatus(ctx, instanceID)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, status)

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

	// Expected execution path:
	// - start (completed)
	// - human-approval (confirmed)
	// - approve (completed)
	// - final (completed)

	// Verify all steps were executed
	assert.Contains(t, stepNames, "start")
	assert.Contains(t, stepNames, "human-approval")
	assert.Contains(t, stepNames, "approve")
	assert.Contains(t, stepNames, "final")

	// Verify step statuses
	assert.Equal(t, StepStatusCompleted, stepStatuses["start"])
	assert.Equal(t, StepStatusCompleted, stepStatuses["human-approval"])
	assert.Equal(t, StepStatusCompleted, stepStatuses["approve"])
	assert.Equal(t, StepStatusCompleted, stepStatuses["final"])

	// Verify human decision was recorded
	decision, err := engine.store.GetHumanDecision(ctx, humanStepID)
	require.NoError(t, err)
	assert.Equal(t, HumanDecisionConfirmed, decision.Decision)
	assert.Equal(t, "manager@company.com", decision.DecidedBy)
	assert.Equal(t, &comment, decision.Comment)
}

func TestHumanInTheLoopRejected(t *testing.T) {
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
	engine.RegisterHandler(&ApprovalTestHandler{})

	// Create workflow: start -> human-approval -> approve -> final
	workflowDef, err := NewBuilder("human_approval_rejected", 1).
		Step("start", "simple-test", WithStepMaxRetries(1)).
		WaitHumanConfirm("human-approval", WithStepDelay(time.Millisecond*100)).
		Then("approve", "approval-test", WithStepMaxRetries(1)).
		Then("final", "simple-test", WithStepMaxRetries(1)).
		Build()

	require.NoError(t, err)
	err = engine.RegisterWorkflow(ctx, workflowDef)
	require.NoError(t, err)

	// Start workflow
	input := json.RawMessage(`{"document_id": "DOC-002", "status": "pending"}`)
	instanceID, err := engine.Start(ctx, "human_approval_rejected-v1", input)
	require.NoError(t, err)

	// Process workflow until human step is waiting
	var humanStepID int64
	for i := 0; i < 10; i++ {
		empty, err := engine.ExecuteNext(ctx, "worker1")
		require.NoError(t, err)
		if empty {
			break
		}
		time.Sleep(100 * time.Millisecond)

		// Check if human step is waiting for decision
		steps, err := engine.GetSteps(ctx, instanceID)
		require.NoError(t, err)
		for _, step := range steps {
			if step.StepName == "human-approval" && step.Status == StepStatusWaitingDecision {
				humanStepID = step.ID
				break
			}
		}
		if humanStepID != 0 {
			break
		}
	}

	// Verify human step is waiting
	require.NotZero(t, humanStepID, "Human step should be waiting for decision")

	// Make human decision - REJECTED
	comment := "Document does not meet quality standards"
	err = engine.MakeHumanDecision(ctx, humanStepID, "manager@company.com", HumanDecisionRejected, &comment)
	require.NoError(t, err)
	time.Sleep(time.Second)

	// Continue processing workflow (should not execute further steps)
	for i := 0; i < 5; i++ {
		empty, err := engine.ExecuteNext(ctx, "worker1")
		require.NoError(t, err)
		if empty {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

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

	// Expected execution path:
	// - start (completed)
	// - human-approval (rejected)
	// - approve (should NOT be executed)
	// - final (should NOT be executed)

	// Verify that only start and human-approval were executed
	assert.Contains(t, stepNames, "start")
	assert.Contains(t, stepNames, "human-approval")
	assert.NotContains(t, stepNames, "approve")
	assert.NotContains(t, stepNames, "final")

	// Verify step statuses
	assert.Equal(t, StepStatusCompleted, stepStatuses["start"])
	assert.Equal(t, StepStatusRejected, stepStatuses["human-approval"])

	// Verify human decision was recorded
	decision, err := engine.store.GetHumanDecision(ctx, humanStepID)
	require.NoError(t, err)
	assert.Equal(t, HumanDecisionRejected, decision.Decision)
	assert.Equal(t, "manager@company.com", decision.DecidedBy)
	assert.Equal(t, &comment, decision.Comment)
}

// Test handler for approval steps
type ApprovalTestHandler struct{}

func (h *ApprovalTestHandler) Name() string { return "approval-test" }

func (h *ApprovalTestHandler) Execute(ctx context.Context, stepCtx StepContext, input json.RawMessage) (json.RawMessage, error) {
	// Just pass through the input with approval status
	var data map[string]any
	if err := json.Unmarshal(input, &data); err != nil {
		return nil, err
	}

	data["status"] = "approved"
	data["approved_at"] = time.Now().Unix()

	return json.Marshal(data)
}
