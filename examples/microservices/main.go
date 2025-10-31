package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rom8726/floxy"
)

type UserServiceHandler struct{}

func (h *UserServiceHandler) Name() string {
	return "user-service"
}

func (h *UserServiceHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
	var request map[string]any
	if err := json.Unmarshal(input, &request); err != nil {
		return nil, err
	}

	userID := request["user_id"].(string)

	time.Sleep(50 * time.Millisecond)

	if rand.Float64() < 0.05 {
		return nil, fmt.Errorf("user service unavailable")
	}

	user := map[string]any{
		"id":       userID,
		"name":     fmt.Sprintf("User %s", userID),
		"email":    fmt.Sprintf("user%s@example.com", userID),
		"verified": true,
		"premium":  rand.Float64() < 0.3,
	}

	log.Printf("User service: Retrieved user %s", userID)

	result := map[string]any{
		"user":      user,
		"service":   "user-service",
		"timestamp": time.Now().Unix(),
		"order":     request,
	}

	return json.Marshal(result)
}

type PaymentServiceHandler struct{}

func (h *PaymentServiceHandler) Name() string {
	return "payment-service"
}

func (h *PaymentServiceHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
	var userData map[string]any
	if err := json.Unmarshal(input, &userData); err != nil {
		return nil, err
	}

	order := userData["order"].(map[string]any)
	user := userData["user"].(map[string]any)
	amount := order["amount"].(float64)
	userID := user["id"].(string)

	time.Sleep(80 * time.Millisecond)

	if rand.Float64() < 0.08 {
		return nil, fmt.Errorf("payment declined for user %s", userID)
	}

	transactionID := fmt.Sprintf("TXN_%d_%s", time.Now().Unix(), userID)

	log.Printf("Payment service: Processed payment %s for amount %.2f", transactionID, amount)

	result := map[string]any{
		"transaction_id": transactionID,
		"amount":         amount,
		"user_id":        userID,
		"status":         "completed",
		"service":        "payment-service",
		"timestamp":      time.Now().Unix(),
		"order":          order,
		"user":           user,
	}

	return json.Marshal(result)
}

type InventoryServiceHandler struct{}

func (h *InventoryServiceHandler) Name() string {
	return "inventory-service"
}

func (h *InventoryServiceHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
	var userData map[string]any
	if err := json.Unmarshal(input, &userData); err != nil {
		return nil, err
	}

	order := userData["order"].(map[string]any)
	user := userData["user"].(map[string]any)
	items := order["items"].([]any)

	time.Sleep(60 * time.Millisecond)

	if rand.Float64() < 0.03 {
		return nil, fmt.Errorf("inventory check failed")
	}

	reservedItems := make([]map[string]any, 0)
	for _, item := range items {
		itm := item.(map[string]any)
		reservedItems = append(reservedItems, map[string]any{
			"id":       itm["id"],
			"name":     itm["name"],
			"quantity": itm["quantity"],
			"reserved": true,
		})
	}

	log.Printf("Inventory service: Reserved %d items", len(reservedItems))

	result := map[string]any{
		"reserved_items": reservedItems,
		"service":        "inventory-service",
		"timestamp":      time.Now().Unix(),
		"order":          order,
		"user":           user,
	}

	return json.Marshal(result)
}

type NotificationServiceHandler struct{}

func (h *NotificationServiceHandler) Name() string {
	return "notification-service"
}

func (h *NotificationServiceHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
	var data map[string]any
	if err := json.Unmarshal(input, &data); err != nil {
		return nil, err
	}

	// Get message from metadata or use default
	message, ok := stepCtx.GetVariableAsString("message")
	if !ok {
		message = "Order processing completed"
	}

	// Extract order and user data from the combined results
	var order map[string]any
	var user map[string]any
	var channels []any

	if _, ok := data["order"]; ok {
		order = data["order"].(map[string]any)
		user = data["user"].(map[string]any)
		channels = order["channels"].([]any)
	} else {
		// Fallback for compensation notifications
		order = data
		user = data
		channels = []any{"email"}
	}

	time.Sleep(30 * time.Millisecond)

	notifications := make([]map[string]any, 0)
	for _, channel := range channels {
		ch := channel.(string)
		notifications = append(notifications, map[string]any{
			"channel": ch,
			"message": message,
			"status":  "sent",
			"sent_at": time.Now().Unix(),
		})
	}

	log.Printf("Notification service: Sent %d notifications", len(notifications))

	result := map[string]any{
		"notifications": notifications,
		"service":       "notification-service",
		"timestamp":     time.Now().Unix(),
		"order":         order,
		"user":          user,
	}

	return json.Marshal(result)
}

type AnalyticsServiceHandler struct{}

func (h *AnalyticsServiceHandler) Name() string {
	return "analytics-service"
}

func (h *AnalyticsServiceHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
	var data map[string]any
	if err := json.Unmarshal(input, &data); err != nil {
		return nil, err
	}

	var order map[string]any
	var eventType string
	if _, ok := data["order"]; ok {
		order = data["order"].(map[string]any)
		eventType = order["event_type"].(string)
	}
	var user map[string]any
	var userID string
	if _, ok := data["user"]; ok {
		user = data["user"].(map[string]any)
		userID = user["id"].(string)
	}

	time.Sleep(40 * time.Millisecond)

	eventID := fmt.Sprintf("EVT_%d_%s", time.Now().Unix(), userID)

	log.Printf("Analytics service: Tracked event %s for user %s", eventType, userID)

	result := map[string]any{
		"event_id":   eventID,
		"event_type": eventType,
		"user_id":    userID,
		"service":    "analytics-service",
		"timestamp":  time.Now().Unix(),
		"order":      order,
		"user":       user,
	}

	return json.Marshal(result)
}

type AuditServiceHandler struct{}

func (h *AuditServiceHandler) Name() string {
	return "audit-service"
}

func (h *AuditServiceHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
	var data map[string]any
	if err := json.Unmarshal(input, &data); err != nil {
		return nil, err
	}

	var order map[string]any
	var action string
	if _, ok := data["order"]; ok {
		order = data["order"].(map[string]any)
		action = order["action"].(string)
	}
	var user map[string]any
	var userID string
	if _, ok := data["user"]; ok {
		user = data["user"].(map[string]any)
		userID = user["id"].(string)
	}

	time.Sleep(20 * time.Millisecond)

	auditID := fmt.Sprintf("AUDIT_%d_%s", time.Now().Unix(), userID)

	log.Printf("Audit service: Logged action %s for user %s", action, userID)

	result := map[string]any{
		"audit_id":  auditID,
		"action":    action,
		"user_id":   userID,
		"service":   "audit-service",
		"timestamp": time.Now().Unix(),
		"order":     order,
		"user":      user,
	}

	return json.Marshal(result)
}

type CompensationHandler struct{}

func (h *CompensationHandler) Name() string {
	return "compensation"
}

func (h *CompensationHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
	var data map[string]any
	if err := json.Unmarshal(input, &data); err != nil {
		return nil, err
	}

	// Get action and reason from metadata
	action, ok := stepCtx.GetVariableAsString("action")
	if !ok {
		action = "unknown_action"
	}
	reason, ok := stepCtx.GetVariableAsString("reason")
	if !ok {
		reason = "unknown_reason"
	}

	log.Printf("Compensation: Executing %s due to %s", action, reason)

	result := map[string]any{
		"action":    action,
		"reason":    reason,
		"status":    "compensated",
		"timestamp": time.Now().Unix(),
		"order":     data,
	}

	return json.Marshal(result)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(context.Background(), "postgres://floxy:password@localhost:5435/floxy?sslmode=disable")
	if err != nil {
		log.Fatalf("Failed to create connection pool: %v", err)
	}
	defer pool.Close()

	// Run migrations once
	if err := floxy.RunMigrations(ctx, pool); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	// Define microservices, each with its own Engine and only its own handler registered
	type serviceSpec struct {
		name    string
		handler floxy.StepHandler
	}

	services := []serviceSpec{
		{"user", &UserServiceHandler{}},
		{"payment", &PaymentServiceHandler{}},
		{"inventory", &InventoryServiceHandler{}},
		{"notification", &NotificationServiceHandler{}},
		{"analytics", &AnalyticsServiceHandler{}},
		{"audit", &AuditServiceHandler{}},
		// Compensation handler is special and used only on failures; register it on a dedicated service
		{"compensation", &CompensationHandler{}},
	}

	// Create an Engine per service and register only its own handler
	engines := make(map[string]*floxy.Engine)
	for _, s := range services {
		engine := floxy.NewEngine(pool)
		engine.RegisterHandler(s.handler)
		engines[s.name] = engine
	}

	// Register a workflow definition once (on any engine)
	workflowDef, err := floxy.NewBuilder("microservices-orchestration", 1).
		Step("validate-user", "user-service", floxy.WithStepMaxRetries(3)).
		OnFailure("compensate-user-validation", "compensation",
			floxy.WithStepMaxRetries(1),
			floxy.WithStepMetadata(map[string]any{
				"action": "user_validation_failed",
				"reason": "user_validation_error",
			}),
		).
		Fork("process-payment-and-inventory",
			func(branch *floxy.Builder) {
				branch.Step("process-payment", "payment-service", floxy.WithStepMaxRetries(3))
			},
			func(branch *floxy.Builder) {
				branch.Step("check-inventory", "inventory-service", floxy.WithStepMaxRetries(2))
			},
		).
		JoinStep("send-notifications", []string{"process-payment", "check-inventory"}, floxy.JoinStrategyAll).
		Fork("track-analytics",
			func(branch *floxy.Builder) {
				branch.Step("track-event", "analytics-service", floxy.WithStepMaxRetries(1))
			},
			func(branch *floxy.Builder) {
				branch.Step("audit-action", "audit-service", floxy.WithStepMaxRetries(1))
			},
		).
		JoinStep("finalize-order", []string{"track-event", "audit-action"}, floxy.JoinStrategyAll).
		Build()
	if err != nil {
		log.Fatalf("Failed to build workflow: %v", err)
	}
	if err := engines["user"].RegisterWorkflow(ctx, workflowDef); err != nil {
		log.Fatalf("Failed to register workflow: %v", err)
	}

	// Launch a worker loop per service (microservice) in its own goroutine
	for name, eng := range engines {
		name := name
		eng := eng
		go func() {
			workerID := fmt.Sprintf("svc-%s", name)
			log.Printf("Service %s worker started", name)
			for {
				select {
				case <-ctx.Done():
					return
				default:
					empty, err := eng.ExecuteNext(ctx, workerID)
					if err != nil {
						log.Printf("Service %s ExecuteNext error: %v", name, err)
						// small pause to avoid hot loop on errors
						time.Sleep(100 * time.Millisecond)
						continue
					}
					if empty {
						time.Sleep(100 * time.Millisecond)
					}
				}
			}
		}()
	}

	// Seed some orders from a coordinator context
	orders := []map[string]any{
		{
			"user_id": "user_001",
			"amount":  299.99,
			"items": []map[string]any{
				{"id": "item1", "name": "Laptop", "quantity": 1},
				{"id": "item2", "name": "Mouse", "quantity": 2},
			},
			"channels":   []string{"email", "sms"},
			"event_type": "order_placed",
			"action":     "order_processing",
		},
		{
			"user_id": "user_002",
			"amount":  149.50,
			"items": []map[string]any{
				{"id": "item3", "name": "Keyboard", "quantity": 1},
			},
			"channels":   []string{"email", "push"},
			"event_type": "order_placed",
			"action":     "order_processing",
		},
		{
			"user_id": "user_003",
			"amount":  89.99,
			"items": []map[string]any{
				{"id": "item4", "name": "Headphones", "quantity": 1},
				{"id": "item5", "name": "Case", "quantity": 1},
			},
			"channels":   []string{"email"},
			"event_type": "order_placed",
			"action":     "order_processing",
		},
	}

	for i, order := range orders {
		orderJSON, _ := json.Marshal(order)
		instanceID, err := engines["user"].Start(ctx, "microservices-orchestration-v1", orderJSON)
		if err != nil {
			log.Printf("Failed to start workflow %d: %v", i+1, err)
		} else {
			log.Printf("Started microservices workflow instance %d: %d", i+1, instanceID)
		}
	}

	// Wait until signal, then shutdown all engines gracefully
	<-ctx.Done()
	log.Println("Shutting down services...")
	for name, eng := range engines {
		log.Printf("Stopping service %s", name)
		eng.Shutdown()
	}
}
