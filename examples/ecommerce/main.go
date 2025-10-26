package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
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
	if err := json.Unmarshal(input, &order); err != nil {
		return nil, err
	}

	amount := order["amount"].(float64)

	if rand.Float64() < 0.2 {
		return nil, fmt.Errorf("payment failed for amount %.2f", amount)
	}

	transactionID := fmt.Sprintf("txn_%d", time.Now().Unix())

	log.Printf("Payment processed: %s for amount %.2f", transactionID, amount)

	result := map[string]any{
		"transaction_id": transactionID,
		"amount":         amount,
		"status":         "paid",
		"timestamp":      time.Now().Unix(),
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
	if err := json.Unmarshal(input, &payment); err != nil {
		return nil, err
	}

	order := payment["order"].(map[string]any)
	items := order["items"].([]any)

	if rand.Float64() < 0.1 {
		return nil, fmt.Errorf("insufficient inventory for items")
	}

	log.Printf("Inventory reserved for %d items", len(items))

	result := map[string]any{
		"reserved_items": items,
		"status":         "reserved",
		"timestamp":      time.Now().Unix(),
		"order":          order,
	}

	return json.Marshal(result)
}

type ShippingHandler struct{}

func (h *ShippingHandler) Name() string {
	return "shipping"
}

func (h *ShippingHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
	var inventory map[string]any
	if err := json.Unmarshal(input, &inventory); err != nil {
		return nil, err
	}

	order := inventory["order"].(map[string]any)
	address := order["shipping_address"].(map[string]any)

	trackingNumber := fmt.Sprintf("TRK%d", rand.Intn(1000000))

	log.Printf("Order shipped to %s, tracking: %s", address["city"], trackingNumber)

	result := map[string]any{
		"tracking_number":  trackingNumber,
		"shipping_address": address,
		"status":           "shipped",
		"timestamp":        time.Now().Unix(),
		"order":            order,
	}

	return json.Marshal(result)
}

type NotificationHandler struct{}

func (h *NotificationHandler) Name() string {
	return "notification"
}

func (h *NotificationHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
	var data map[string]any
	if err := json.Unmarshal(input, &data); err != nil {
		return nil, err
	}

	var order map[string]any
	if _, ok := data["order_id"]; ok {
		order = data
	} else {
		order = data["order"].(map[string]any)
	}

	message, ok := stepCtx.GetVariableAsString("message")
	if !ok {
		message = "message not found"
	}
	email := order["email"].(string)

	log.Printf("Notification sent to %s: %s", email, message)

	result := map[string]any{
		"email":     email,
		"message":   message,
		"status":    "sent",
		"timestamp": time.Now().Unix(),
	}

	return json.Marshal(result)
}

type RefundHandler struct{}

func (h *RefundHandler) Name() string {
	return "refund"
}

func (h *RefundHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
	var data map[string]any
	if err := json.Unmarshal(input, &data); err != nil {
		return nil, err
	}

	amount := data["amount"].(float64)
	reason := data["reason"].(string)

	refundID := fmt.Sprintf("ref_%d", time.Now().Unix())

	log.Printf("Refund processed: %s for amount %.2f, reason: %s", refundID, amount, reason)

	result := map[string]any{
		"refund_id": refundID,
		"amount":    amount,
		"reason":    reason,
		"status":    "refunded",
		"timestamp": time.Now().Unix(),
	}

	return json.Marshal(result)
}

func main() {
	ctx := context.Background()

	pool, err := pgxpool.New(context.Background(), "postgres://floxy:password@localhost:5435/floxy?sslmode=disable")
	if err != nil {
		log.Fatalf("Failed to create connection pool: %v", err)
	}
	defer pool.Close()

	engine := floxy.NewEngine(pool)
	defer engine.Shutdown()

	// Run migrations
	if err := floxy.RunMigrations(ctx, pool); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	engine.RegisterHandler(&PaymentHandler{})
	engine.RegisterHandler(&InventoryHandler{})
	engine.RegisterHandler(&ShippingHandler{})
	engine.RegisterHandler(&NotificationHandler{})
	engine.RegisterHandler(&RefundHandler{})

	workflowDef, err := floxy.NewBuilder("ecommerce-order", 1).
		Step("process-payment", "payment", floxy.WithStepMaxRetries(3)).
		OnFailure("send-payment-failure-notification", "notification",
			floxy.WithStepMaxRetries(1),
			floxy.WithStepMetadata(map[string]any{
				"message": "Payment failed!",
			})).
		Then("reserve-inventory", "inventory", floxy.WithStepMaxRetries(2)).
		Then("ship-order", "shipping", floxy.WithStepMaxRetries(2)).
		Then("send-success-notification", "notification",
			floxy.WithStepMaxRetries(1),
			floxy.WithStepMetadata(map[string]any{
				"message": "Order shipped successfully!",
			}),
		).Build()

	if err != nil {
		log.Fatalf("Failed to build workflow: %v", err)
	}

	if err := engine.RegisterWorkflow(context.Background(), workflowDef); err != nil {
		log.Fatalf("Failed to register workflow: %v", err)
	}

	workerPool := floxy.NewWorkerPool(engine, 3, 500*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	workerPool.Start(ctx)

	order := map[string]any{
		"order_id": "ORD-001",
		"amount":   99.99,
		"email":    "customer@example.com",
		"items": []map[string]any{
			{"id": "item1", "name": "Laptop", "quantity": 1},
			{"id": "item2", "name": "Mouse", "quantity": 2},
		},
		"shipping_address": map[string]any{
			"street": "123 Main St",
			"city":   "New York",
			"state":  "NY",
			"zip":    "10001",
		},
	}

	orderJSON, _ := json.Marshal(order)
	instanceID, err := engine.Start(context.Background(), "ecommerce-order-v1", orderJSON)
	if err != nil {
		log.Fatalf("Failed to start workflow: %v", err)
	}

	log.Printf("Started e-commerce order workflow instance: %d", instanceID)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	<-sigCh
	log.Println("Shutting down...")

	workerPool.Stop()
	cancel()

	status, err := engine.GetStatus(context.Background(), instanceID)
	if err != nil {
		log.Printf("Failed to get status: %v", err)
	} else {
		log.Printf("Final status: %s", status)
	}
}
