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
	payment := inventory["payment"].(map[string]any)

	fmt.Printf("Shipping order for user %s\n", order["user_id"])

	if payment["amount"].(float64) > 1000 {
		return nil, fmt.Errorf("payment declined: amount too high")
	}

	// Simulate shipping
	time.Sleep(75 * time.Millisecond)

	result := map[string]any{
		"tracking_number": fmt.Sprintf("TRK_%d", time.Now().Unix()),
		"status":          "shipped",
		"timestamp":       time.Now().Unix(),
		"order":           order,
		"payment":         payment,
		"inventory":       inventory,
	}

	return json.Marshal(result)
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

	// Create a store and transaction manager
	engine := floxy.NewEngine(pool)
	defer engine.Shutdown()

	// Register handlers
	engine.RegisterHandler(&PaymentHandler{})
	engine.RegisterHandler(&InventoryHandler{})
	engine.RegisterHandler(&ShippingHandler{})
	engine.RegisterHandler(&NotificationHandler{})
	engine.RegisterHandler(&CompensationHandler{})

	// Create workflow with SavePoint
	workflowDef, err := floxy.NewBuilder("savepoint-demo", 1).
		Step("process-payment", "payment", floxy.WithStepMaxRetries(2)).
		SavePoint("payment-checkpoint").
		Then("reserve-inventory", "inventory", floxy.WithStepMaxRetries(1)).
		Then("ship-order", "shipping", floxy.WithStepMaxRetries(1)).
		OnFailure("ship-order-failure", "compensation",
			floxy.WithStepMaxRetries(1), floxy.WithStepMetadata(map[string]any{
				"action": "return",
			}),
		).
		Then("send-success-notification", "notification",
			floxy.WithStepMaxRetries(1),
			floxy.WithStepMetadata(map[string]any{
				"message": "Order processed successfully!",
			})).
		Build()

	if err != nil {
		log.Fatalf("Failed to create workflow: %v", err)
	}

	// Register workflow
	if err := engine.RegisterWorkflow(ctx, workflowDef); err != nil {
		log.Fatalf("Failed to register workflow: %v", err)
	}

	fmt.Println("=== SavePoint Demo Workflow ===")
	fmt.Printf("Workflow ID: %s\n", workflowDef.ID)
	fmt.Println("Steps:")
	for name, step := range workflowDef.Definition.Steps {
		fmt.Printf("  - %s (type: %s, prev: %s)\n", name, step.Type, step.Prev)
	}
	fmt.Println()

	// Test case 1: Successful order
	fmt.Println("=== Test Case 1: Successful Order ===")
	order1 := map[string]any{
		"user_id": "user123",
		"amount":  500.0,
		"items":   []string{"item1", "item2"},
	}

	input1, _ := json.Marshal(order1)
	instanceID1, err := engine.Start(ctx, "savepoint-demo-v1", input1)
	if err != nil {
		log.Printf("Failed to start workflow: %v", err)
	} else {
		fmt.Printf("Started workflow instance: %d\n", instanceID1)
	}

	// Test case 2: Failed order (high amount)
	fmt.Println("\n=== Test Case 2: Failed Order (High Amount) ===")
	order2 := map[string]any{
		"user_id": "user456",
		"amount":  1500.0,
		"items":   []string{"item3", "item4"},
	}

	input2, _ := json.Marshal(order2)
	instanceID2, err := engine.Start(ctx, "savepoint-demo-v1", input2)
	if err != nil {
		log.Printf("Failed to start workflow: %v", err)
	} else {
		fmt.Printf("Started workflow instance: %d\n", instanceID2)
	}

	// Process workflows
	fmt.Println("\n=== Processing Workflows ===")
	for range 100 {
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
	if instanceID1 > 0 {
		status1, _ := engine.GetStatus(ctx, instanceID1)
		fmt.Printf("Instance %d status: %s\n", instanceID1, status1)
	}

	if instanceID2 > 0 {
		status2, _ := engine.GetStatus(ctx, instanceID2)
		fmt.Printf("Instance %d status: %s\n", instanceID2, status2)
	}
}
