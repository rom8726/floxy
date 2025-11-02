package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/rom8726/floxy"
)

type ProcessHandler struct{}

func (h *ProcessHandler) Name() string {
	return "process"
}

func (h *ProcessHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
	var data map[string]any
	if err := json.Unmarshal(input, &data); err != nil {
		return nil, err
	}

	item := data["item"].(string)
	processed := fmt.Sprintf("processed-%s", item)

	result := map[string]any{
		"item":      processed,
		"timestamp": time.Now().Unix(),
	}

	return json.Marshal(result)
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

	engine.RegisterHandler(&ProcessHandler{})

	workflowDef, err := floxy.NewBuilder("processing-workflow", 1).
		Step("process-item", "process").
		Build()
	if err != nil {
		log.Fatalf("Failed to build workflow: %v", err)
	}

	if err := engine.RegisterWorkflow(ctx, workflowDef); err != nil {
		log.Fatalf("Failed to register workflow: %v", err)
	}

	workerPool := floxy.NewWorkerPool(engine, 2, 100*time.Millisecond)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	workerPool.Start(ctx)

	input := json.RawMessage(`{"item": "test-item-123"}`)
	instanceID, err := engine.Start(ctx, workflowDef.ID, input)
	if err != nil {
		log.Fatalf("Failed to start workflow: %v", err)
	}

	log.Printf("Started workflow instance: %d", instanceID)

	time.Sleep(2 * time.Second)

	workerPool.Stop()
	cancel()

	status, err := engine.GetStatus(ctx, instanceID)
	if err != nil {
		log.Printf("Failed to get status: %v", err)
	} else {
		log.Printf("Final status: %s", status)
	}

	instance, err := store.GetInstance(ctx, instanceID)
	if err != nil {
		log.Printf("Failed to get instance: %v", err)
	} else {
		log.Printf("Instance details: ID=%d, Status=%s", instance.ID, instance.Status)
		var outputData map[string]any
		if len(instance.Output) > 0 {
			_ = json.Unmarshal(instance.Output, &outputData)
			log.Printf("Output: %v", outputData)
		}
	}

	steps, err := store.GetStepsByInstance(ctx, instanceID)
	if err != nil {
		log.Printf("Failed to get steps: %v", err)
	} else {
		log.Printf("Total steps: %d", len(steps))
		for _, step := range steps {
			log.Printf("  Step: %s, Status: %s, Type: %s", step.StepName, step.Status, step.StepType)
		}
	}

	stats, err := store.GetSummaryStats(ctx)
	if err != nil {
		log.Printf("Failed to get stats: %v", err)
	} else {
		log.Printf("Summary stats: Total=%d, Completed=%d, Failed=%d, Running=%d, Pending=%d, Active=%d",
			stats.TotalWorkflows, stats.CompletedWorkflows, stats.FailedWorkflows,
			stats.RunningWorkflows, stats.PendingWorkflows, stats.ActiveWorkflows)
	}
}
