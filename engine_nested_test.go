package floxy

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type maybeErrorCtxKey struct{}

func TestEngineNested(t *testing.T) {
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

	target := &FloxyStressTarget{
		engine: engine,
	}

	target.registerHandlers()

	workflowDef, err := NewBuilder("nested-workflow", 1).
		Step("validate", "validation").
		Then("process-payment", "payment", WithStepMaxRetries(3)).
		OnFailure("refund-payment", "compensation", WithStepMetadata(map[string]any{"action": "refund"})).
		SavePoint("payment-checkpoint").
		Fork("nested-parallel",
			func(b *Builder) {
				b.Step("reserve-inventory-1", "inventory").
					OnFailure("release-inventory-1", "compensation", WithStepMetadata(map[string]any{"action": "release"})).
					Then("ship-1", "shipping", WithStepMaxRetries(1)).
					OnFailure("cancel-shipment-1", "compensation", WithStepMetadata(map[string]any{"action": "cancel"}))
			},
			func(b *Builder) {
				b.Step("reserve-inventory-2", "inventory").
					OnFailure("release-inventory-2", "compensation", WithStepMetadata(map[string]any{"action": "release"})).
					Then("ship-2", "shipping", WithStepMaxRetries(1)).
					OnFailure("cancel-shipment-2", "compensation", WithStepMetadata(map[string]any{"action": "cancel"}))
			},
		).
		Join("join", JoinStrategyAll).
		Then("notify", "notification").
		Build()

	require.NoError(t, err)
	err = engine.RegisterWorkflow(ctx, workflowDef)
	require.NoError(t, err)

	t.Run("without error", func(t *testing.T) {
		// Create random order data
		order := map[string]any{
			"order_id": fmt.Sprintf("ORD-%d", time.Now().UnixNano()),
			"user_id":  fmt.Sprintf("user-%d", rand.Intn(1000)),
			"amount":   float64(rand.Intn(500) + 50),
			"items":    rand.Intn(10) + 1,
		}

		input, _ := json.Marshal(order)

		instanceID, err := engine.Start(ctx, "nested-workflow-v1", input)
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

		assert.Equal(t, StatusCompleted, status)
		assert.Equal(t,
			[]string{"validate", "process-payment", "payment-checkpoint", "nested-parallel",
				"reserve-inventory-1", "reserve-inventory-2", "ship-1", "ship-2", "join", "notify"},
			stepNames,
		)
		for _, step := range steps {
			assert.Equal(t, StepStatusCompleted, step.Status, "Step %s should be completed", step.StepName)
		}
	})

	t.Run("with error in shipping", func(t *testing.T) {
		// Create random order data
		order := map[string]any{
			"order_id": fmt.Sprintf("ORD-%d", time.Now().UnixNano()),
			"user_id":  fmt.Sprintf("user-%d", rand.Intn(1000)),
			"amount":   float64(rand.Intn(500) + 50),
			"items":    rand.Intn(10) + 1,
		}

		input, _ := json.Marshal(order)

		instanceID, err := engine.Start(ctx, "nested-workflow-v1", input)
		require.NoError(t, err)

		ctx = context.WithValue(ctx, maybeErrorCtxKey{}, "shipping")

		// Process workflow until completion or failure
		for i := 0; i < 100; i++ {
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

		assert.Equal(t, StatusFailed, status)

		for _, step := range steps {
			switch step.StepName {
			case "validate", "process-payment", "payment-checkpoint":
				assert.Equal(t, StepStatusCompleted, step.Status)
			case "fork", "join", "nested-parallel", "reserve-inventory-1", "reserve-inventory-2", "ship-1", "ship-2":
				assert.Equal(t, StepStatusRolledBack, step.Status)
			default:
				assert.Fail(t, "Unexpected step %s", step.StepName)
			}
		}
	})
}

type FloxyStressTarget struct {
	engine *Engine
}

func (t *FloxyStressTarget) registerHandlers() {
	handlers := []StepHandler{
		&PaymentNestedHandler{target: t},
		&InventoryNestedHandler{target: t},
		&ShippingNestedHandler{target: t},
		&NotificationNestedHandler{target: t},
		&CompensationNestedHandler{target: t},
		&ValidationNestedHandler{target: t},
	}

	for _, h := range handlers {
		t.engine.RegisterHandler(h)
	}
}

// Step handlers implementation

// PaymentHandler processes payment
var processPaymentHandler = func(ctx context.Context, order map[string]any) error {
	// Simulate processing time
	time.Sleep(time.Millisecond * time.Duration(5+rand.Intn(20)))

	if maybeError, ok := ctx.Value(maybeErrorCtxKey{}).(string); ok && maybeError == "payment" {
		return fmt.Errorf("error in payment")
	}

	order["payment_status"] = "paid"

	return nil
}

type PaymentNestedHandler struct{ target *FloxyStressTarget }

func (h *PaymentNestedHandler) Name() string { return "payment" }
func (h *PaymentNestedHandler) Execute(
	ctx context.Context,
	stepCtx StepContext,
	input json.RawMessage,
) (json.RawMessage, error) {
	var order map[string]any
	if err := json.Unmarshal(input, &order); err != nil {
		return nil, err
	}

	// Call a processable function
	if err := processPaymentHandler(ctx, order); err != nil {
		return nil, err
	}

	return json.Marshal(order)
}

// InventoryHandler processes inventory reservation
var processInventoryHandler = func(ctx context.Context, order map[string]any) error {
	time.Sleep(time.Millisecond * time.Duration(5+rand.Intn(15)))

	if maybeError, ok := ctx.Value(maybeErrorCtxKey{}).(string); ok && maybeError == "inventory" {
		return fmt.Errorf("error in inventory")
	}

	order["inventory_status"] = "reserved"

	return nil
}

type InventoryNestedHandler struct{ target *FloxyStressTarget }

func (h *InventoryNestedHandler) Name() string { return "inventory" }
func (h *InventoryNestedHandler) Execute(
	ctx context.Context,
	stepCtx StepContext,
	input json.RawMessage,
) (json.RawMessage, error) {
	var order map[string]any
	if err := json.Unmarshal(input, &order); err != nil {
		return nil, err
	}

	if err := processInventoryHandler(ctx, order); err != nil {
		return nil, err
	}

	return json.Marshal(order)
}

// ShippingHandler processes shipping
var processShippingHandler = func(ctx context.Context, order map[string]any) error {
	time.Sleep(time.Millisecond * time.Duration(10+rand.Intn(20)))

	if maybeError, ok := ctx.Value(maybeErrorCtxKey{}).(string); ok && maybeError == "shipping" {
		return fmt.Errorf("error in shipping")
	}

	order["shipping_status"] = "shipped"

	return nil
}

type ShippingNestedHandler struct{ target *FloxyStressTarget }

func (h *ShippingNestedHandler) Name() string { return "shipping" }
func (h *ShippingNestedHandler) Execute(
	ctx context.Context,
	stepCtx StepContext,
	input json.RawMessage,
) (json.RawMessage, error) {
	var order map[string]any
	if err := json.Unmarshal(input, &order); err != nil {
		return nil, err
	}

	if err := processShippingHandler(ctx, order); err != nil {
		return nil, err
	}

	return json.Marshal(order)
}

type NotificationNestedHandler struct{ target *FloxyStressTarget }

func (h *NotificationNestedHandler) Name() string { return "notification" }
func (h *NotificationNestedHandler) Execute(
	ctx context.Context,
	stepCtx StepContext,
	input json.RawMessage,
) (json.RawMessage, error) {
	time.Sleep(time.Millisecond * time.Duration(5+rand.Intn(10)))

	return input, nil
}

type ValidationNestedHandler struct{ target *FloxyStressTarget }

func (h *ValidationNestedHandler) Name() string { return "validation" }
func (h *ValidationNestedHandler) Execute(
	ctx context.Context,
	stepCtx StepContext,
	input json.RawMessage,
) (json.RawMessage, error) {
	time.Sleep(time.Millisecond * time.Duration(5+rand.Intn(10)))

	return input, nil
}

type CompensationNestedHandler struct{ target *FloxyStressTarget }

func (h *CompensationNestedHandler) Name() string { return "compensation" }
func (h *CompensationNestedHandler) Execute(
	ctx context.Context,
	stepCtx StepContext,
	input json.RawMessage,
) (json.RawMessage, error) {
	action, _ := stepCtx.GetVariableAsString("action")
	time.Sleep(time.Millisecond * time.Duration(5+rand.Intn(10)))

	return json.Marshal(map[string]any{"compensated": action})
}
