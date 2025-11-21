package floxy

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rom8726/chaoskit"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

func nestedWorkflow(t *testing.T) *WorkflowDefinition {
	t.Helper()

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

	return workflowDef
}

type FloxyStressTarget struct {
	engine *Engine
	wfDef  *WorkflowDefinition
	input  map[string]any
	//rollbackPlugin    *rolldepth.RollbackDepthPlugin
	workflowInstances atomic.Int64
	successfulRuns    atomic.Int64
	failedRuns        atomic.Int64
	rollbackCount     atomic.Int64
	maxRollbackDepth  atomic.Int32
}

func NewFloxyStressTarget(
	store Store,
	txManager TxManager,
	wfDef *WorkflowDefinition,
	input map[string]any,
) (*FloxyStressTarget, error) {
	engine := NewEngine(nil,
		WithEngineStore(store),
		WithEngineTxManager(txManager),
		WithMissingHandlerCooldown(50*time.Millisecond),
		WithQueueAgingEnabled(true),
		WithQueueAgingRate(0.5),
	)

	// Register plugins
	//rollbackPlugin := rolldepth.New()
	//engine.RegisterPlugin(rollbackPlugin)

	target := &FloxyStressTarget{
		engine: engine,
		wfDef:  wfDef,
		input:  input,
		//rollbackPlugin: rollbackPlugin,
	}

	// Register handlers
	target.registerHandlers()

	if err := engine.RegisterWorkflow(context.Background(), wfDef); err != nil {
		return nil, err
	}

	return target, nil
}

func (t *FloxyStressTarget) Name() string {
	return "floxy-stress-target"
}

func (t *FloxyStressTarget) Setup(ctx context.Context) error {
	return nil
}

func (t *FloxyStressTarget) Teardown(ctx context.Context) error {
	return nil
}

func (t *FloxyStressTarget) Execute(ctx context.Context) error {
	input, _ := json.Marshal(t.input)

	instanceID, err := t.engine.Start(ctx, t.wfDef.ID, input)
	if err != nil {
		return fmt.Errorf("failed to start workflow: %w", err)
	}

	t.workflowInstances.Add(1)

	// Track rollback depth for this instance
	chaoskit.RecordRecursionDepth(ctx, 0)

	var wg sync.WaitGroup
	wg.Add(1)
	defer wg.Wait()

	ctxMonitor, cancel := context.WithCancel(ctx)
	defer cancel()

	go func(ctx context.Context) {
		defer wg.Done()

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			status, err := t.engine.GetStatus(context.Background(), instanceID)
			if err != nil {
				log.Printf("failed to get status: %s", err)

				continue
			}

			// Check rollback depth
			//depth := t.rollbackPlugin.GetMaxDepth(instanceID)
			//if depth > 0 {
			//	chaoskit.RecordRecursionDepth(ctx, depth)
			//	currentMax := t.maxRollbackDepth.Load()
			//	if int32(depth) > currentMax {
			//		t.maxRollbackDepth.Store(int32(depth))
			//	}
			//}

			if status == StatusCompleted {
				t.successfulRuns.Add(1)
				//t.rollbackPlugin.ResetMaxDepth(instanceID)

				return
			}

			if status == StatusFailed {
				t.failedRuns.Add(1)
				//if depth > 0 {
				//	t.rollbackCount.Add(1)
				//}
				//t.rollbackPlugin.ResetMaxDepth(instanceID)

				return
			}

			if status == StatusAborted || status == StatusCancelled {
				t.failedRuns.Add(1)
				//t.rollbackPlugin.ResetMaxDepth(instanceID)

				return
			}

			time.Sleep(100 * time.Millisecond)
		}
	}(ctxMonitor)

	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error {
		return t.worker(groupCtx, uuid.NewString())
	})
	group.Go(func() error {
		return t.worker(groupCtx, uuid.NewString())
	})
	group.Go(func() error {
		return t.worker(groupCtx, uuid.NewString())
	})

	return group.Wait()
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

func (t *FloxyStressTarget) worker(ctx context.Context, workerID string) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			empty, err := t.engine.ExecuteNext(ctx, workerID)
			if err != nil {
				log.Printf("[Floxy] Worker %s error: %v", workerID, err)

				return fmt.Errorf("worker %s error: %w", workerID, err)
			}
			if empty {
				return nil
			}
		}
	}
}

// Step handlers implementation

// PaymentHandler processes payment
var processPaymentHandler = func(ctx context.Context, order map[string]any) error {
	// Simulate processing time
	time.Sleep(time.Millisecond * time.Duration(5+rand.Intn(20)))

	chaoskit.MaybeDelay(ctx)
	chaoskit.MaybePanic(ctx)
	if err := chaoskit.MaybeError(ctx); err != nil {
		return err
	}

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

	chaoskit.MaybeDelay(ctx)
	chaoskit.MaybePanic(ctx)
	if err := chaoskit.MaybeError(ctx); err != nil {
		return err
	}

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

	chaoskit.MaybeDelay(ctx)
	chaoskit.MaybePanic(ctx)
	if err := chaoskit.MaybeError(ctx); err != nil {
		return err
	}

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
