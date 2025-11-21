package floxy

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type testInventoryHandler struct {
	failCounter *int
}

func (h *testInventoryHandler) Name() string {
	return "inventory"
}

func (h *testInventoryHandler) Execute(ctx context.Context, stepCtx StepContext, input json.RawMessage) (json.RawMessage, error) {
	stepName := stepCtx.StepName()
	if stepName == "reserve-inventory-2" {
		*h.failCounter++
		// Fail after multiple retries (max_retries=3, so on 4th attempt it should fail)
		if *h.failCounter >= 4 {
			return nil, fmt.Errorf("chaos error")
		}
		return nil, fmt.Errorf("chaos error")
	}
	// reserve-inventory-1 succeeds
	var data map[string]any
	_ = json.Unmarshal(input, &data)
	data["inventory_status"] = "reserved"
	return json.Marshal(data)
}

type testShippingHandler struct{}

func (h *testShippingHandler) Name() string {
	return "shipping"
}

func (h *testShippingHandler) Execute(ctx context.Context, stepCtx StepContext, input json.RawMessage) (json.RawMessage, error) {
	var data map[string]any
	_ = json.Unmarshal(input, &data)
	data["shipping_status"] = "shipped"
	return json.Marshal(data)
}

type testCompensationHandler struct{}

func (h *testCompensationHandler) Name() string {
	return "compensation"
}

func (h *testCompensationHandler) Execute(ctx context.Context, stepCtx StepContext, input json.RawMessage) (json.RawMessage, error) {
	// Compensation succeeds
	return input, nil
}

// TestBugFix_WorkflowStatusWithRolledBackSteps tests the fix for the bug where
// a workflow was marked as completed even though it had rolled_back steps.
// This test reproduces the scenario from bug.sql where reserve-inventory-2 failed,
// compensation was executed (marking steps as rolled_back), but ship-1 continued
// and completed successfully, causing the workflow to be incorrectly marked as completed.
func TestBugFix_WorkflowStatusWithRolledBackSteps(t *testing.T) {
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
	defer func() { _ = engine.Shutdown(time.Second) }()

	// Create handlers that simulate the bug scenario
	failCounter := 0

	inventoryHandler := &testInventoryHandler{failCounter: &failCounter}
	shippingHandler := &testShippingHandler{}
	compensationHandler := &testCompensationHandler{}

	engine.RegisterHandler(inventoryHandler)
	engine.RegisterHandler(shippingHandler)
	engine.RegisterHandler(compensationHandler)

	// Create workflow similar to the one in bug.sql using Builder
	workflowDef, err := NewBuilder("nested-workflow", 1).
		Fork("nested-parallel",
			func(b *Builder) {
				b.Step("reserve-inventory-1", "inventory").
					OnFailure("release-inventory-1", "compensation", WithStepMetadata(map[string]any{"action": "release"})).
					Then("ship-1", "shipping", WithStepMaxRetries(3)).
					OnFailure("cancel-shipment-1", "compensation", WithStepMetadata(map[string]any{"action": "cancel"}))
			},
			func(b *Builder) {
				b.Step("reserve-inventory-2", "inventory").
					OnFailure("release-inventory-2", "compensation", WithStepMetadata(map[string]any{"action": "release"})).
					Then("ship-2", "shipping", WithStepMaxRetries(3)).
					OnFailure("cancel-shipment-2", "compensation", WithStepMetadata(map[string]any{"action": "cancel"}))
			},
		).
		Join("join", JoinStrategyAll).
		Build()

	require.NoError(t, err)
	err = engine.RegisterWorkflow(ctx, workflowDef)
	require.NoError(t, err)

	input := json.RawMessage(`{"items": 4, "amount": 105, "user_id": "user-292", "order_id": "ORD-123"}`)
	instanceID, err := engine.Start(ctx, "nested-workflow-v1", input)
	require.NoError(t, err)

	// Process workflow until completion or failure
	for i := 0; i < 50; i++ {
		empty, err := engine.ExecuteNext(ctx, "worker1")
		require.NoError(t, err)
		if empty {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Get final workflow status
	status, err := engine.GetStatus(ctx, instanceID)
	require.NoError(t, err)

	// Get all steps
	steps, err := engine.GetSteps(ctx, instanceID)
	require.NoError(t, err)

	// Check if there are rolled_back steps
	hasRolledBack := false
	stepStatuses := make(map[string]StepStatus)
	for _, step := range steps {
		stepStatuses[step.StepName] = step.Status
		if step.Status == StepStatusRolledBack {
			hasRolledBack = true
		}
	}

	t.Logf("Final workflow status: %s", status)
	t.Logf("Step statuses: %v", stepStatuses)

	// ASSERTION: If there are rolled_back steps, the workflow status should be "failed", not "completed"
	if hasRolledBack && status == StatusCompleted {
		t.Errorf("BUG: Workflow is marked as completed but has rolled_back steps! This is the bug we're fixing.")
	}

	if hasRolledBack && status != StatusFailed {
		t.Errorf("Expected workflow status to be 'failed' when there are rolled_back steps, got: %s", status)
	}

	// Additional check: verify that the fix is working correctly
	if hasRolledBack && status == StatusFailed {
		t.Logf("BUG FIX VERIFIED: Workflow correctly marked as 'failed' when there are rolled_back steps")
	}

	// Verify that some steps are indeed rolled_back
	if !hasRolledBack {
		t.Error("Expected to have rolled_back steps in this test scenario")
	}

	// Verify reserve-inventory-1 is rolled_back
	if stepStatus, ok := stepStatuses["reserve-inventory-1"]; !ok || stepStatus != StepStatusRolledBack {
		t.Errorf("Expected reserve-inventory-1 to be rolled_back, got: %s", stepStatus)
	}

	// Verify reserve-inventory-2 is rolled_back
	if stepStatus, ok := stepStatuses["reserve-inventory-2"]; !ok || stepStatus != StepStatusRolledBack {
		t.Errorf("Expected reserve-inventory-2 to be rolled_back, got: %s", stepStatus)
	}
}
