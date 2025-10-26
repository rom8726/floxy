package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rom8726/floxy"
)

type PaymentHandler struct{}

func (h *PaymentHandler) Name() string {
	return "payment"
}

func (h *PaymentHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
	var order map[string]any
	_ = json.Unmarshal(input, &order)

	amount := order["amount"].(float64)
	userID := order["user_id"].(string)

	fmt.Printf("Processing payment for user %s, amount: $%.2f\n", userID, amount)

	// Simulate payment processing
	time.Sleep(100 * time.Millisecond)

	result := map[string]any{
		"transaction_id": fmt.Sprintf("txn_%d", time.Now().Unix()),
		"amount":         amount,
		"user_id":        userID,
		"status":         "completed",
		"order":          order,
	}

	return json.Marshal(result)
}

type InventoryHandler struct{}

func (h *InventoryHandler) Name() string {
	return "inventory"
}

func (h *InventoryHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
	var payment map[string]any
	_ = json.Unmarshal(input, &payment)

	order := payment["order"].(map[string]any)
	items := order["items"].([]any)

	fmt.Printf("Reserving inventory for %d items\n", len(items))

	// Simulate inventory check
	time.Sleep(50 * time.Millisecond)

	result := map[string]any{
		"reserved_items": items,
		"status":         "reserved",
		"timestamp":      time.Now().Unix(),
		"order":          order,
		"payment":        payment,
	}

	return json.Marshal(result)
}

type ShippingHandler struct{}

func (h *ShippingHandler) Name() string {
	return "shipping"
}

func (h *ShippingHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
	var inventory map[string]any
	_ = json.Unmarshal(input, &inventory)

	order := inventory["order"].(map[string]any)

	fmt.Printf("Shipping order for user %s\n", order["user_id"])

	// Simulate shipping
	time.Sleep(75 * time.Millisecond)

	// Force failure for demonstration
	return nil, fmt.Errorf("shipping service unavailable")
}

type NotificationHandler struct{}

func (h *NotificationHandler) Name() string {
	return "notification"
}

func (h *NotificationHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
	message, _ := stepCtx.GetVariableAsString("message")
	fmt.Printf("Sending notification: %s\n", message)

	// Simulate notification
	time.Sleep(25 * time.Millisecond)

	return json.Marshal(map[string]any{
		"status":  "sent",
		"message": message,
	})
}

type CompensationHandler struct{}

func (h *CompensationHandler) Name() string {
	return "compensation"
}

func (h *CompensationHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
	action, _ := stepCtx.GetVariableAsString("action")
	reason, _ := stepCtx.GetVariableAsString("reason")

	fmt.Printf("Executing compensation: %s (reason: %s)\n", action, reason)

	// Simulate compensation
	time.Sleep(50 * time.Millisecond)

	return json.Marshal(map[string]any{
		"status": "compensated",
		"action": action,
		"reason": reason,
	})
}

func main() {
	ctx := context.Background()

	// Connect to database
	pool, err := pgxpool.New(ctx, "postgres://floxy:password@localhost:5435/floxy?sslmode=disable")
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer pool.Close()

	// Run migrations
	if err := floxy.RunMigrations(ctx, pool); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	// Create store and transaction manager
	engine := floxy.NewEngine(pool)
	defer engine.Shutdown()

	// Register handlers
	engine.RegisterHandler(&PaymentHandler{})
	engine.RegisterHandler(&InventoryHandler{})
	engine.RegisterHandler(&ShippingHandler{})
	engine.RegisterHandler(&NotificationHandler{})
	engine.RegisterHandler(&CompensationHandler{})

	// Create workflow with SavePoint and OnFailure handlers for all steps
	workflowDef, err := floxy.NewBuilder("rollback-demo", 1).
		Step("process-payment", "payment", floxy.WithStepMaxRetries(2)).
		OnFailure("refund-payment", "compensation",
			floxy.WithStepMaxRetries(1),
			floxy.WithStepMetadata(map[string]any{
				"action": "refund",
				"reason": "payment_failed",
			})).
		Then("reserve-inventory", "inventory", floxy.WithStepMaxRetries(1)).
		OnFailure("release-inventory", "compensation",
			floxy.WithStepMaxRetries(1),
			floxy.WithStepMetadata(map[string]any{
				"action": "release",
				"reason": "inventory_failed",
			})).
		Then("ship-order", "shipping", floxy.WithStepMaxRetries(1)).
		OnFailure("cancel-shipment", "compensation",
			floxy.WithStepMaxRetries(1),
			floxy.WithStepMetadata(map[string]any{
				"action": "cancel",
				"reason": "shipping_failed",
			})).
		Then("send-success-notification", "notification",
			floxy.WithStepMaxRetries(1),
			floxy.WithStepMetadata(map[string]any{
				"message": "Order processed successfully!",
			})).
		OnFailure("send-failure-notification", "notification",
			floxy.WithStepMaxRetries(1),
			floxy.WithStepMetadata(map[string]any{
				"message": "Order processing failed!",
			})).
		Build()

	if err != nil {
		log.Fatalf("Failed to create workflow: %v", err)
	}

	// Register workflow
	if err := engine.RegisterWorkflow(ctx, workflowDef); err != nil {
		log.Fatalf("Failed to register workflow: %v", err)
	}

	fmt.Println("=== Rollback Demo Workflow ===")
	fmt.Printf("Workflow ID: %s\n", workflowDef.ID)
	fmt.Println("Steps:")
	for name, step := range workflowDef.Definition.Steps {
		fmt.Printf("  - %s (type: %s, prev: %s, on_failure: %s)\n",
			name, step.Type, step.Prev, step.OnFailure)
	}
	fmt.Println()

	// Test case: Order that will fail and trigger rollback
	fmt.Println("=== Test Case: Order with Rollback ===")
	order := map[string]any{
		"user_id": "user789",
		"amount":  750.0,
		"items":   []string{"item5", "item6", "item7"},
	}

	input, _ := json.Marshal(order)
	instanceID, err := engine.Start(ctx, "rollback-demo-v1", input)
	if err != nil {
		log.Printf("Failed to start workflow: %v", err)
	} else {
		fmt.Printf("Started workflow instance: %d\n", instanceID)
	}

	// Process workflow
	fmt.Println("\n=== Processing Workflow ===")
	for range 100 {
		// Process workflow
		isEmpty, err := engine.ExecuteNext(ctx, "worker1")
		if err != nil {
			fmt.Printf("ExecuteNext: %v\n", err)
		}
		if isEmpty {
			break
		}

		time.Sleep(100 * time.Millisecond)
	}

	// Check final status
	fmt.Println("\n=== Final Status ===")
	if instanceID > 0 {
		status, _ := engine.GetStatus(ctx, instanceID)
		fmt.Printf("Instance %d status: %s\n", instanceID, status)

		// Get steps to see rollback status
		steps, _ := engine.GetSteps(ctx, instanceID)
		fmt.Println("\nStep Status:")
		for _, step := range steps {
			fmt.Printf("  - %s: %s\n", step.StepName, step.Status)
		}
	}
}
