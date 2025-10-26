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

type HelloHandler struct{}

func (h *HelloHandler) Name() string {
	return "hello"
}

func (h *HelloHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
	var data map[string]interface{}
	if err := json.Unmarshal(input, &data); err != nil {
		return nil, err
	}

	name := "World"
	if n, ok := data["name"].(string); ok {
		name = n
	}

	message := fmt.Sprintf("Hello, %s!", name)
	log.Printf("Hello handler executed: %s. Idempotency key: %s", message, stepCtx.IdempotencyKey())

	result := map[string]interface{}{
		"message":   message,
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

	engine.RegisterHandler(&HelloHandler{})

	workflowDef, err := floxy.NewBuilder("hello-world", 1).
		Step("say-hello", "hello", floxy.WithStepMaxRetries(3)).
		Build()
	if err != nil {
		log.Fatalf("Failed to build workflow: %v", err)
	}

	if err := engine.RegisterWorkflow(context.Background(), workflowDef); err != nil {
		log.Fatalf("Failed to register workflow: %v", err)
	}

	workerPool := floxy.NewWorkerPool(engine, 2, 1*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	workerPool.Start(ctx)

	input := json.RawMessage(`{"name": "Floxy"}`)
	instanceID, err := engine.Start(context.Background(), "hello-world-v1", input)
	if err != nil {
		log.Fatalf("Failed to start workflow: %v", err)
	}

	log.Printf("Started workflow instance: %d", instanceID)

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
