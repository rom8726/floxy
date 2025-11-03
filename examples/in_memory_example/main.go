package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/rom8726/floxy"
)

type ValidateHandler struct{}

func (h *ValidateHandler) Name() string {
	return "validate"
}

func (h *ValidateHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
	var data map[string]any
	if err := json.Unmarshal(input, &data); err != nil {
		return nil, err
	}

	log.Printf("[%s] Validating order: %v", stepCtx.StepName(), data)

	data["validated"] = true
	data["validation_time"] = time.Now().Unix()

	return json.Marshal(data)
}

type ProcessPaymentHandler struct{}

func (h *ProcessPaymentHandler) Name() string {
	return "process-payment"
}

func (h *ProcessPaymentHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
	var data map[string]any
	if err := json.Unmarshal(input, &data); err != nil {
		return nil, err
	}

	log.Printf("[%s] Processing payment for order: %.2f", stepCtx.StepName(), data["amount"].(float64))

	data["payment_processed"] = true
	data["payment_id"] = fmt.Sprintf("PAY-%d", time.Now().Unix())

	return json.Marshal(data)
}

type SendNotificationHandler struct{}

func (h *SendNotificationHandler) Name() string {
	return "send-notification"
}

func (h *SendNotificationHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
	var data map[string]any
	if err := json.Unmarshal(input, &data); err != nil {
		return nil, err
	}

	log.Printf("[%s] Sending notification to: %s", stepCtx.StepName(), data["email"])

	data["notification_sent"] = true

	return json.Marshal(data)
}

type UpdateInventoryHandler struct{}

func (h *UpdateInventoryHandler) Name() string {
	return "update-inventory"
}

func (h *UpdateInventoryHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
	var data map[string]any
	if err := json.Unmarshal(input, &data); err != nil {
		return nil, err
	}

	log.Printf("[%s] Updating inventory for product: %s", stepCtx.StepName(), data["product_id"])

	data["inventory_updated"] = true

	return json.Marshal(data)
}

type SkipInventoryHandler struct{}

func (h *SkipInventoryHandler) Name() string {
	return "skip-inventory"
}

func (h *SkipInventoryHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
	var data map[string]any
	if err := json.Unmarshal(input, &data); err != nil {
		return nil, err
	}

	log.Printf("[%s] Skipping inventory update (digital product)", stepCtx.StepName())

	data["inventory_skipped"] = true

	return json.Marshal(data)
}

type FinalizeOrderHandler struct{}

func (h *FinalizeOrderHandler) Name() string {
	return "finalize-order"
}

func (h *FinalizeOrderHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
	var data map[string]any
	if err := json.Unmarshal(input, &data); err != nil {
		return nil, err
	}

	log.Printf("[%s] Finalizing order", stepCtx.StepName())

	data["order_finalized"] = true
	data["order_id"] = fmt.Sprintf("ORD-%d", time.Now().Unix())

	return json.Marshal(data)
}

func main() {
	ctx := context.Background()

	store := floxy.NewMemoryStore()
	txManager := floxy.NewMemoryTxManager()

	engine := floxy.NewEngine(nil,
		floxy.WithEngineStore(store),
		floxy.WithEngineTxManager(txManager),
	)
	defer engine.Shutdown()

	engine.RegisterHandler(&ValidateHandler{})
	engine.RegisterHandler(&ProcessPaymentHandler{})
	engine.RegisterHandler(&SendNotificationHandler{})
	engine.RegisterHandler(&UpdateInventoryHandler{})
	engine.RegisterHandler(&SkipInventoryHandler{})
	engine.RegisterHandler(&FinalizeOrderHandler{})

	workflowDef, err := floxy.NewBuilder("order-processing", 1).
		Step("validate-order", "validate").
		Step("process-payment", "process-payment").
		Fork("parallel-processing", func(branch1 *floxy.Builder) {
			branch1.Step("send-email", "send-notification").
				Condition("check-product-type", "{{ eq .product_type \"physical\" }}", func(elseBranch *floxy.Builder) {
					elseBranch.Step("skip-inventory", "skip-inventory")
				}).
				Then("update-inventory", "update-inventory")
		}, func(branch2 *floxy.Builder) {
			branch2.Step("send-sms", "send-notification")
		}).
		Join("sync-branches", floxy.JoinStrategyAll).
		Then("finalize-order", "finalize-order").
		Build()

	if err != nil {
		log.Fatalf("Failed to build workflow: %v", err)
	}

	if err := engine.RegisterWorkflow(ctx, workflowDef); err != nil {
		log.Fatalf("Failed to register workflow: %v", err)
	}

	viz := floxy.NewVisualizer()
	fmt.Println("Workflow Graph:")
	fmt.Println(viz.RenderGraph(workflowDef))
	fmt.Println()

	workerPool := floxy.NewWorkerPool(engine, 3, 50*time.Millisecond)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	workerPool.Start(ctx)

	testCases := []struct {
		name  string
		input json.RawMessage
	}{
		{
			name:  "Physical Product",
			input: json.RawMessage(`{"order_id": "12345", "amount": 99.99, "email": "customer@example.com", "product_id": "PROD-001", "product_type": "physical"}`),
		},
		{
			name:  "Digital Product",
			input: json.RawMessage(`{"order_id": "12346", "amount": 29.99, "email": "customer2@example.com", "product_id": "PROD-002", "product_type": "digital"}`),
		},
	}

	instanceIDs := make([]int64, 0, len(testCases))

	for _, tc := range testCases {
		log.Printf("=== Starting workflow for: %s ===", tc.name)
		instanceID, err := engine.Start(ctx, workflowDef.ID, tc.input)
		if err != nil {
			log.Fatalf("Failed to start workflow: %v", err)
		}
		instanceIDs = append(instanceIDs, instanceID)
		log.Printf("Started instance ID: %d", instanceID)
	}

	time.Sleep(3 * time.Second)

	workerPool.Stop()
	cancel()

	fmt.Println("\n=== Final Results ===")
	for i, instanceID := range instanceIDs {
		fmt.Printf("\n--- Test Case %d: %s (Instance ID: %d) ---\n", i+1, testCases[i].name, instanceID)

		status, err := engine.GetStatus(ctx, instanceID)
		if err != nil {
			log.Printf("Failed to get status: %v", err)
			continue
		}
		fmt.Printf("Status: %s\n", status)

		instance, err := store.GetInstance(ctx, instanceID)
		if err != nil {
			log.Printf("Failed to get instance: %v", err)
			continue
		}

		var outputData map[string]any
		if len(instance.Output) > 0 {
			if err := json.Unmarshal(instance.Output, &outputData); err == nil {
				fmt.Printf("Output: %+v\n", outputData)
			}
		}

		steps, err := store.GetStepsByInstance(ctx, instanceID)
		if err != nil {
			log.Printf("Failed to get steps: %v", err)
			continue
		}

		fmt.Println("\nExecuted Steps:")
		for _, step := range steps {
			fmt.Printf("  [%s] %s - %s (Type: %s)\n", step.Status, step.StepName, step.StepType, step.StepType)
		}

		fmt.Println("\nExecution Flow:")
		for _, step := range steps {
			if step.Status == floxy.StepStatusCompleted {
				fmt.Printf("  ✓ %s\n", step.StepName)
			} else if step.Status == floxy.StepStatusSkipped {
				fmt.Printf("  ⊘ %s (skipped)\n", step.StepName)
			} else if step.Status == floxy.StepStatusFailed {
				fmt.Printf("  ✗ %s (failed)\n", step.StepName)
			}
		}
	}

	stats, err := store.GetSummaryStats(ctx)
	if err != nil {
		log.Printf("Failed to get stats: %v", err)
	} else {
		fmt.Printf("\n=== Overall Statistics ===\n")
		fmt.Printf("Total Workflows: %d\n", stats.TotalWorkflows)
		fmt.Printf("Completed: %d\n", stats.CompletedWorkflows)
		fmt.Printf("Failed: %d\n", stats.FailedWorkflows)
		fmt.Printf("Running: %d\n", stats.RunningWorkflows)
		fmt.Printf("Pending: %d\n", stats.PendingWorkflows)
		fmt.Printf("Active: %d\n", stats.ActiveWorkflows)
	}
}
