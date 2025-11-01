package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os/signal"
	"reflect"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rom8726/floxy"
)

// Shared mutable state to simulate side effects across services.
// All services mutate this in their task handlers; compensation must restore it.
var (
	stateMu   sync.Mutex
	accounts  = map[string]float64{}
	inventory = map[string]int{}
	orders    = map[string]bool{}
)

type BillingServiceHandler struct{}

func (h *BillingServiceHandler) Name() string { return "billing-service" }

func (h *BillingServiceHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
	var order map[string]any
	if err := json.Unmarshal(input, &order); err != nil {
		return nil, err
	}
	userID := order["user_id"].(string)
	amount := order["amount"].(float64)

	stateMu.Lock()
	defer stateMu.Unlock()
	balance := accounts[userID]
	if balance < amount {
		return nil, fmt.Errorf("insufficient funds: have %.2f, need %.2f", balance, amount)
	}
	accounts[userID] = balance - amount
	log.Printf("[billing] debited %.2f from %s (new balance %.2f)", amount, userID, accounts[userID])

	out := map[string]any{
		"debited":  amount,
		"user_id":  userID,
		"order":    order,
		"snapshot": balance,
	}
	return json.Marshal(out)
}

type InventoryServiceHandler struct{}

func (h *InventoryServiceHandler) Name() string { return "inventory-service" }

func (h *InventoryServiceHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
	var billed map[string]any
	if err := json.Unmarshal(input, &billed); err != nil {
		return nil, err
	}
	order := billed["order"].(map[string]any)
	items := order["items"].([]any)

	stateMu.Lock()
	defer stateMu.Unlock()

	// Reserve all items
	reserved := make([]map[string]any, 0, len(items))
	for _, it := range items {
		itm := it.(map[string]any)
		id := itm["id"].(string)
		qty := int(itm["quantity"].(float64))
		available := inventory[id]
		if available < qty {
			return nil, fmt.Errorf("inventory low for %s: have %d, need %d", id, available, qty)
		}
		inventory[id] = available - qty
		reserved = append(reserved, map[string]any{"id": id, "quantity": qty})
	}
	log.Printf("[inventory] reserved %d items", len(reserved))

	out := map[string]any{
		"reserved": reserved,
		"order":    order,
		"billed":   billed,
	}
	return json.Marshal(out)
}

type OrderServiceHandler struct{}

func (h *OrderServiceHandler) Name() string { return "order-service" }

func (h *OrderServiceHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
	var inv map[string]any
	if err := json.Unmarshal(input, &inv); err != nil {
		return nil, err
	}
	order := inv["order"].(map[string]any)
	orderID := fmt.Sprintf("ORD-%d", time.Now().UnixNano())

	stateMu.Lock()
	orders[orderID] = true
	stateMu.Unlock()
	log.Printf("[order] created %s", orderID)

	out := map[string]any{
		"order_id": orderID,
		"order":    order,
		"inv":      inv,
	}
	return json.Marshal(out)
}

type ShipmentServiceHandler struct{}

func (h *ShipmentServiceHandler) Name() string { return "shipment-service" }

func (h *ShipmentServiceHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
	// Force a failure to trigger rollback of entire workflow
	return nil, errors.New("shipment provider outage (forced)")
}

// Compensation handler used by a dedicated microservice; actions are driven by metadata.
// It receives original step input to revert the side effects.

type CompensationHandler struct{}

func (h *CompensationHandler) Name() string { return "compensation" }

func (h *CompensationHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
	action, _ := stepCtx.GetVariableAsString("action")

	switch action {
	case "refund-debit":
		var order map[string]any
		_ = json.Unmarshal(input, &order)
		userID := order["user_id"].(string)
		amount := order["amount"].(float64)
		stateMu.Lock()
		accounts[userID] = accounts[userID] + amount
		stateMu.Unlock()
		log.Printf("[compensation] refunded %.2f to %s", amount, userID)
		return json.Marshal(map[string]any{"status": "refunded"})
	case "release-inventory":
		var billed map[string]any
		_ = json.Unmarshal(input, &billed)
		order := billed["order"].(map[string]any)
		items := order["items"].([]any)
		stateMu.Lock()
		for _, it := range items {
			itm := it.(map[string]any)
			id := itm["id"].(string)
			qty := int(itm["quantity"].(float64))
			inventory[id] = inventory[id] + qty
		}
		stateMu.Unlock()
		log.Printf("[compensation] released inventory for %d items", len(items))
		return json.Marshal(map[string]any{"status": "inventory_released"})
	case "delete-order":
		var inv map[string]any
		_ = json.Unmarshal(input, &inv)
		// We stored generated order_id only in OrderService output. To simulate, delete any order.
		stateMu.Lock()
		for k := range orders {
			delete(orders, k)
		}
		stateMu.Unlock()
		log.Printf("[compensation] deleted order(s)")
		return json.Marshal(map[string]any{"status": "order_deleted"})
	default:
		log.Printf("[compensation] unknown action: %s", action)
		return json.Marshal(map[string]any{"status": "noop"})
	}
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(context.Background(), "postgres://floxy:password@localhost:5435/floxy?sslmode=disable")
	if err != nil {
		log.Fatalf("Failed to create connection pool: %v", err)
	}
	defer pool.Close()

	if err := floxy.RunMigrations(ctx, pool); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	// Initialize shared state and take snapshot
	stateMu.Lock()
	accounts = map[string]float64{"user_rollback": 500.0}
	inventory = map[string]int{"item1": 10, "item2": 5}
	orders = map[string]bool{}
	initialAccounts := copyMapFloat(accounts)
	initialInventory := copyMapInt(inventory)
	initialOrders := copyMapBool(orders)
	stateMu.Unlock()

	type svc struct {
		name    string
		handler floxy.StepHandler
	}

	services := []svc{
		{"billing", &BillingServiceHandler{}},
		{"inventory", &InventoryServiceHandler{}},
		{"order", &OrderServiceHandler{}},
		{"shipment", &ShipmentServiceHandler{}},
		{"compensation", &CompensationHandler{}},
	}

	engines := make(map[string]*floxy.Engine)
	for _, s := range services {
		eng := floxy.NewEngine(pool,
			floxy.WithMissingHandlerCooldown(time.Millisecond*50),
			floxy.WithMissingHandlerLogThrottle(time.Millisecond*100),
			floxy.WithQueueAgingEnabled(true),
			floxy.WithQueueAgingRate(0.5),
		)
		eng.RegisterHandler(s.handler)
		engines[s.name] = eng
	}

	// Build a workflow where every task has an on-failure compensation.
	wf, err := floxy.NewBuilder("microservices-rollback", 1).
		Step("debit-account", "billing-service").
		OnFailure("refund-debit", "compensation", floxy.WithStepMaxRetries(2), floxy.WithStepMetadata(map[string]any{"action": "refund-debit"})).
		Step("reserve-inventory", "inventory-service").
		OnFailure("release-inventory", "compensation", floxy.WithStepMaxRetries(2), floxy.WithStepMetadata(map[string]any{"action": "release-inventory"})).
		Step("create-order", "order-service").
		OnFailure("delete-order", "compensation", floxy.WithStepMaxRetries(2), floxy.WithStepMetadata(map[string]any{"action": "delete-order"})).
		// Final step intentionally fails to trigger rollback of the whole flow
		Step("ship-order", "shipment-service").
		OnFailure("noop-shipment-compensation", "compensation", floxy.WithStepMetadata(map[string]any{"action": "noop"})).
		Build()
	if err != nil {
		log.Fatalf("build workflow: %v", err)
	}
	if err := engines["billing"].RegisterWorkflow(ctx, wf); err != nil {
		log.Fatalf("register workflow: %v", err)
	}

	// Start workers per microservice
	for name, eng := range engines {
		name := name
		eng := eng
		go func() {
			workerID := "svc-" + name
			for {
				select {
				case <-ctx.Done():
					return
				default:
					empty, err := eng.ExecuteNext(ctx, workerID)
					if err != nil {
						log.Printf("%s ExecuteNext error: %v", name, err)
						time.Sleep(100 * time.Millisecond)
						continue
					}
					if empty {
						time.Sleep(50 * time.Millisecond)
					}
				}
			}
		}()
	}

	// Start a single instance designed to fail at shipment and trigger rollback
	orderInput := map[string]any{
		"user_id": "user_rollback",
		"amount":  120.0,
		"items":   []map[string]any{{"id": "item1", "quantity": 2}, {"id": "item2", "quantity": 1}},
	}
	inputJSON, _ := json.Marshal(orderInput)
	instanceID, err := engines["billing"].Start(ctx, "microservices-rollback-v1", inputJSON)
	if err != nil {
		log.Fatalf("start workflow: %v", err)
	}
	log.Printf("Started instance %d", instanceID)

	// Wait for terminal state (engine marks instance failed once rollback is orchestrated)
	deadline := time.Now().Add(15 * time.Second)
	failedObserved := false
	for time.Now().Before(deadline) {
		status, err := engines["billing"].GetStatus(ctx, instanceID)
		if err != nil {
			log.Printf("get status error: %v", err)
			time.Sleep(200 * time.Millisecond)
			continue
		}
		if status == floxy.StatusFailed {
			failedObserved = true
			break
		}
		if status == floxy.StatusCompleted || status == floxy.StatusAborted || status == floxy.StatusCancelled {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	// If failed, wait additionally until all compensation steps have finished processing.
	if failedObserved {
		rollDeadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(rollDeadline) {
			steps, err := engines["billing"].GetSteps(ctx, instanceID)
			if err != nil {
				log.Printf("get steps error: %v", err)
				time.Sleep(200 * time.Millisecond)
				continue
			}
			remaining := 0
			for _, st := range steps {
				if st.Status == floxy.StepStatusPending || st.Status == floxy.StepStatusRunning || st.Status == floxy.StepStatusCompensation {
					remaining++
				}
			}
			if remaining == 0 {
				break
			}
			log.Printf("waiting for compensation to finish: remaining=%d", remaining)
			time.Sleep(250 * time.Millisecond)
		}
	}

	// Verify that the shared state matches the initial snapshot (full rollback)
	stateMu.Lock()
	finalAccounts := copyMapFloat(accounts)
	finalInventory := copyMapInt(inventory)
	finalOrders := copyMapBool(orders)
	stateMu.Unlock()

	ok := reflect.DeepEqual(initialAccounts, finalAccounts) &&
		reflect.DeepEqual(initialInventory, finalInventory) &&
		reflect.DeepEqual(initialOrders, finalOrders)
	if !ok {
		log.Printf("INITIAL accounts=%v inventory=%v orders=%v", initialAccounts, initialInventory, initialOrders)
		log.Printf("FINAL   accounts=%v inventory=%v orders=%v", finalAccounts, finalInventory, finalOrders)
		log.Fatalf("State mismatch: rollback did not restore initial state")
	}

	log.Printf("Rollback demo succeeded: shared state fully restored")
}

func copyMapFloat(in map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(in))
	for k, v := range in {
		out[k] = v
	}

	return out
}

func copyMapInt(in map[string]int) map[string]int {
	out := make(map[string]int, len(in))
	for k, v := range in {
		out[k] = v
	}

	return out
}

func copyMapBool(in map[string]bool) map[string]bool {
	out := make(map[string]bool, len(in))
	for k, v := range in {
		out[k] = v
	}

	return out
}
