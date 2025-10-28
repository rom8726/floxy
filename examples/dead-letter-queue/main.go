package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rom8726/floxy"
)

// This example demonstrates Dead Letter Queue (DLQ) mode:
// - A workflow is created with DLQ enabled.
// - One step fails intentionally and is moved to workflows.workflow_dlq and the instance enters `dlq` status.
// - We then requeue the step back from DLQ with a new input to recover and let the workflow finish.

type SimpleOKHandler struct{}

func (h *SimpleOKHandler) Name() string { return "simple-ok" }

func (h *SimpleOKHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
	log.Printf("[simple-ok] instance=%d step=%s idk=%s", stepCtx.InstanceID(), stepCtx.StepName(), stepCtx.IdempotencyKey())

	return input, nil
}

type MaybeFailHandler struct{}

func (h *MaybeFailHandler) Name() string { return "maybe-fail" }

func (h *MaybeFailHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
	var data map[string]any
	_ = json.Unmarshal(input, &data)
	shouldFail, _ := data["should_fail"].(bool)
	if shouldFail {
		return nil, fmt.Errorf("intentional failure (goes to DLQ in DLQ mode)")
	}
	log.Printf("[maybe-fail] recovered execution. instance=%d step=%s", stepCtx.InstanceID(), stepCtx.StepName())

	return json.RawMessage(`{"recovered":true}`), nil
}

func main() {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, "postgres://floxy:password@localhost:5435/floxy?sslmode=disable")
	if err != nil {
		log.Fatalf("failed to create pool: %v", err)
	}
	defer pool.Close()

	engine := floxy.NewEngine(pool)
	defer engine.Shutdown()

	if err := floxy.RunMigrations(ctx, pool); err != nil {
		log.Fatalf("failed to run migrations: %v", err)
	}

	engine.RegisterHandler(&SimpleOKHandler{})
	engine.RegisterHandler(&MaybeFailHandler{})

	// Build workflow with DLQ enabled
	wf, err := floxy.NewBuilder("dlq-demo", 1, floxy.WithDLQEnabled(true)).
		Step("step-ok", "simple-ok", floxy.WithStepMaxRetries(1)).
		Then("step-maybe-fail", "maybe-fail", floxy.WithStepMaxRetries(1)).
		Then("step-after-recovery", "simple-ok", floxy.WithStepMaxRetries(1)).
		Build()
	if err != nil {
		log.Fatalf("failed to build workflow: %v", err)
	}

	if err := engine.RegisterWorkflow(ctx, wf); err != nil {
		log.Fatalf("failed to register workflow: %v", err)
	}

	workerPool := floxy.NewWorkerPool(engine, 2, 250*time.Millisecond)
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	workerPool.Start(workerCtx)

	// Start the workflow with input that causes failure in step2
	input := json.RawMessage(`{"should_fail": true}`)
	instanceID, err := engine.Start(ctx, "dlq-demo-v1", input)
	if err != nil {
		log.Fatalf("failed to start workflow: %v", err)
	}
	log.Printf("started instance: %d", instanceID)

	// Wait until instance enters DLQ status and DLQ record is created
	if err := waitForDLQStatus(ctx, engine, instanceID, 30, 200*time.Millisecond); err != nil {
		log.Fatalf("instance did not enter DLQ: %v", err)
	}
	log.Printf("instance %d is in DLQ state", instanceID)

	recID, stepName, dlqErr, err := findDLQRecord(ctx, pool, instanceID)
	if err != nil {
		log.Fatalf("failed to find DLQ record: %v", err)
	}
	log.Printf("found DLQ record id=%d step=%s error=%s", recID, stepName, dlqErr)
	log.Printf("requeueing with new input to fix should_fail=false ...")

	// Prepare new input to make the step succeed on retry
	newInput := json.RawMessage(`{"should_fail": false}`)
	if err := engine.RequeueFromDLQ(ctx, recID, &newInput); err != nil {
		log.Fatalf("failed to requeue from DLQ: %v", err)
	}

	// Wait a bit to let worker process the requeued step
	if err := waitForTerminalOrRunning(ctx, engine, instanceID, 40, 250*time.Millisecond); err != nil {
		log.Printf("warning: instance did not reach terminal quickly: %v", err)
	}

	status, err := engine.GetStatus(ctx, instanceID)
	if err != nil {
		log.Printf("failed to get final status: %v", err)
	} else {
		log.Printf("final status: %s", status)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	log.Printf("press Ctrl+C to exit")
	<-sigCh
	workerPool.Stop()
	cancel()
}

func waitForDLQStatus(ctx context.Context, engine *floxy.Engine, instanceID int64, attempts int, sleep time.Duration) error {
	for i := 0; i < attempts; i++ {
		st, err := engine.GetStatus(ctx, instanceID)
		if err == nil && st == floxy.StatusDLQ {
			return nil
		}
		time.Sleep(sleep)
	}

	return fmt.Errorf("timeout waiting for DLQ status")
}

func waitForTerminalOrRunning(ctx context.Context, engine *floxy.Engine, instanceID int64, attempts int, sleep time.Duration) error {
	for i := 0; i < attempts; i++ {
		st, err := engine.GetStatus(ctx, instanceID)
		if err == nil {
			if st == floxy.StatusCompleted || st == floxy.StatusFailed || st == floxy.StatusRunning {
				return nil
			}
		}
		time.Sleep(sleep)
	}

	return fmt.Errorf("timeout waiting for terminal/running status")
}

func findDLQRecord(ctx context.Context, pool *pgxpool.Pool, instanceID int64) (int64, string, string, error) {
	const q = `SELECT id, step_name, COALESCE(error,'') FROM workflows.workflow_dlq WHERE instance_id=$1 ORDER BY created_at DESC LIMIT 1`
	var (
		id       int64
		stepName string
		errStr   string
	)
	err := pool.QueryRow(ctx, q, instanceID).Scan(&id, &stepName, &errStr)

	return id, stepName, errStr, err
}
