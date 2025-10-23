# floxy

A Go library for creating and executing workflows with a custom DSL. Implements the Saga pattern with orchestrator approach, providing transaction management and compensation capabilities.

floxy means "flow" + "flux" + "tiny".

## Features

- **Workflow DSL**: Declarative workflow definition using Builder pattern
- **Saga Pattern**: Orchestrator-based saga implementation with compensation
- **Transaction Management**: Built-in transaction support with rollback capabilities
- **Parallel Execution**: Fork/Join patterns for concurrent workflow steps
- **Error Handling**: Automatic retry mechanisms and failure compensation
- **SavePoints**: Rollback to specific points in workflow execution
- **PostgreSQL Storage**: Persistent workflow state and event logging
- **Migrations**: Embedded database migrations with `go:embed`

PlantUML diagrams of compensations flow: [DIAGRAMS](docs/SAGA_COMPENSATION_DIAGRAMS.md)

Engine specification: [ENGINE](docs/ENGINE_SPEC.md)

## Quick Start

```go
package main

import (
    "context"
    "encoding/json"
    "log"
    
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/rom8726/floxy"
)

func main() {
    ctx := context.Background()
    
    // Connect to PostgreSQL
    pool, err := pgxpool.New(ctx, "postgres://user:password@localhost:5432/floxy?sslmode=disable")
    if err != nil {
        log.Fatal(err)
    }
    defer pool.Close()
    
    // Run database migrations
    if err := floxy.RunMigrations(ctx, pool); err != nil {
        log.Fatal(err)
    }
    
    // Create engine
    store := floxy.NewStore(pool)
    txManager := floxy.NewTxManager(pool)
    engine := floxy.NewEngine(txManager, store)
    
    // Register step handlers
    engine.RegisterHandler(&PaymentHandler{})
    engine.RegisterHandler(&InventoryHandler{})
    engine.RegisterHandler(&ShippingHandler{})
    engine.RegisterHandler(&CompensationHandler{})
    
    // Define workflow using Builder DSL
    workflow, err := floxy.NewBuilder("order-processing", 1).
        Step("process-payment", "payment", floxy.WithStepMaxRetries(3)).
        OnFailure("refund-payment", "compensation").
        SavePoint("payment-checkpoint").
        Then("reserve-inventory", "inventory", floxy.WithStepMaxRetries(2)).
        OnFailure("release-inventory", "compensation").
        Then("ship-order", "shipping").
        OnFailure("cancel-shipment", "compensation").
        Build()
    if err != nil {
        log.Fatal(err)
    }
    
    // Register and start workflow
    if err := engine.RegisterWorkflow(ctx, workflow); err != nil {
        log.Fatal(err)
    }
    
    order := map[string]any{
        "user_id": "user123",
        "amount":  100.0,
        "items":   []string{"item1", "item2"},
    }
    input, _ := json.Marshal(order)
    
    instanceID, err := engine.Start(ctx, "order-processing-v1", input)
    if err != nil {
        log.Fatal(err)
    }
    
    log.Printf("Workflow started: %d", instanceID)
    
    // Process workflow steps
    for {
        empty, err := engine.ExecuteNext(ctx, "worker1")
        if err != nil {
            log.Printf("ExecuteNext error: %v", err)
        }
        if empty {
            break
        }
    }
    
    // Check final status
    status, err := engine.GetStatus(ctx, instanceID)
    if err != nil {
        log.Fatal(err)
    }
    log.Printf("Workflow status: %s", status)
}

// Step handlers
type PaymentHandler struct{}
func (h *PaymentHandler) Name() string { return "payment" }
func (h *PaymentHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
    // Process payment logic
    return json.Marshal(map[string]any{"status": "paid"})
}

type InventoryHandler struct{}
func (h *InventoryHandler) Name() string { return "inventory" }
func (h *InventoryHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
    // Reserve inventory logic
    return json.Marshal(map[string]any{"status": "reserved"})
}

type ShippingHandler struct{}
func (h *ShippingHandler) Name() string { return "shipping" }
func (h *ShippingHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
    // Ship order logic
    return json.Marshal(map[string]any{"status": "shipped"})
}

type CompensationHandler struct{}
func (h *CompensationHandler) Name() string { return "compensation" }
func (h *CompensationHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
    // Compensation logic
    return json.Marshal(map[string]any{"status": "compensated"})
}
```

## Examples

See the `examples/` directory for complete workflow examples:

- **Hello World**: Basic single-step workflow
- **E-commerce**: Order processing with compensation flows
- **Data Pipeline**: Parallel data processing with Fork/Join patterns
- **Microservices**: Complex service orchestration with multiple branches
- **SavePoint Demo**: Demonstrates SavePoint functionality and rollback
- **Rollback Demo**: Shows full rollback mechanism with OnFailure handlers

Run examples:
```bash
# Start PostgreSQL (required for examples)
make dev-up

# Run all examples
cd examples/hello_world && go run main.go
cd examples/ecommerce && go run main.go
cd examples/data_pipeline && go run main.go
cd examples/microservices && go run main.go
cd examples/savepoint_demo && go run main.go
cd examples/rollback_demo && go run main.go
```

## Integration Tests

The library includes comprehensive integration tests using testcontainers:

```bash
# Run all tests
go test ./...

# Run only integration tests
go test -v -run TestIntegration

# Run specific integration test
go test -v -run TestIntegration_DataPipeline
```

Integration tests cover:
- **Data Pipeline**: Parallel data processing with multiple sources
- **E-commerce**: Order processing with success/failure scenarios
- **Microservices**: Complex orchestration with multiple service calls
- **SavePoint Demo**: SavePoint functionality with conditional failures
- **Rollback Demo**: Full rollback mechanism testing

## Database Migrations

The library includes embedded database migrations using `go:embed`. Migrations are automatically applied when using `floxy.RunMigrations()`:

```go
// Run migrations
if err := floxy.RunMigrations(ctx, pool); err != nil {
    log.Fatal(err)
}
```

Available migrations:
- `001_initial.up.sql`: Initial schema creation
- `002_add_savepoint_and_rollback.up.sql`: SavePoint and rollback support

## Installation

```bash
go get github.com/rom8726/floxy
```

## Dependencies

- PostgreSQL database
- Go 1.24+
