# floxy

A Go library for creating and executing workflows with a custom DSL. Implements the Saga pattern with orchestrator approach, providing transaction management and compensation capabilities.

floxy means "flow" + "flux" + "tiny".

## Table of Contents

- [Features](#features)
- [Why Floxy?](#why-floxy)
  - [1. Lightweight, Not Heavyweight](#1-lightweight-not-heavyweight)
  - [2. Pragmatic by Design](#2-pragmatic-by-design)
  - [3. Embedded](#3-embedded)
- [Quick Start](#quick-start)
- [Examples](#examples)
- [Integration Tests](#integration-tests)
- [Database Migrations](#database-migrations)
- [Known Issues](#known-issues)
  - [Condition Steps in Forked Branches](#condition-steps-in-forked-branches)
- [Installation](#installation)
- [Dependencies](#dependencies)

## Features

- **Workflow DSL**: Declarative workflow definition using Builder pattern
- **Saga Pattern**: Orchestrator-based saga implementation with compensation
- **Workflows Versioning**: Safe changing flows using versions
- **Transaction Management**: Built-in transaction support with rollback capabilities
- **Parallel Execution**: Fork/Join patterns for concurrent workflow steps
- **Error Handling**: Automatic retry mechanisms and failure compensation
- **SavePoints**: Rollback to specific points in workflow execution
- **Conditional branching** with Condition steps. Smart rollback for parallel flows with condition steps.
- **Human-in-the-loop**: Interactive workflow steps that pause execution for human decisions
- **PostgreSQL Storage**: Persistent workflow state and event logging
- **Migrations**: Embedded database migrations with `go:embed`

PlantUML diagrams of compensations flow: [DIAGRAMS](docs/SAGA_COMPENSATION_DIAGRAMS.md)

Engine specification: [ENGINE](docs/ENGINE_SPEC.md)

UI for visualizing workflows: [UI](https://github.com/rom8726/floxy-ui)

> Warning: This engine is ideal for pet projects, but has not been tested in production. Using this in production, you act at your own risk.

## Why Floxy?

**Floxy** is a lightweight, embeddable workflow engine for Go developers.
It was born from the idea that *not every system needs a full-blown workflow platform like Cadence or Temporal.*

### 1. Lightweight, Not Heavyweight

Most workflow engines require you to deploy multiple services, brokers, and databases just to run a single flow.
Floxy is different — it’s a **Go library**.
You import it, initialize an engine, and define your workflow directly in Go code.

No clusters. No queues.

```go
wf, _ := floxy.NewBuilder("order", 1).
    Step("reserve_stock", "stock.Reserve").
    Then("charge_payment", "payment.Charge").
    OnFailure("refund", "payment.Refund").
    Build()
```

That’s it — a complete Saga with compensation.

### 2. Pragmatic by Design

Floxy doesn’t try to solve every problem in distributed systems.
It focuses on **clear, deterministic workflow execution** with the tools Go developers already use:

* PostgreSQL as durable storage
* Go’s standard `net/http` for API
* Structured retries, compensation, and rollback

You don’t need to learn Cadence’s terminology.
Everything is plain Go — just like your codebase.

### 3. Embedded

You can embed Floxy inside any Go service.

> Floxy is for developers who love Go, simplicity, and control.
No orchestration clusters. No external DSLs.
Just workflows — defined in Go, executed anywhere.

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
    pool, err := pgxpool.New(ctx, "postgres://floxy:password@localhost:5432/floxy?sslmode=disable")
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
    engine := floxy.NewEngine(pool, store)
	defer engine.Shutdown()
    
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
- **Human-in-the-loop**: Interactive workflows with human decision points

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
cd examples/human_in_the_loop__approved && go run main.go
cd examples/human_in_the_loop__rejected && go run main.go
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
- `003_add_compensation_retry_count.up.sql`: compensation step status and compensation_retry_count added
- `004_add_compensation_to_views.up.sql`: active_workflows view updated
- `005_add_idempotency_key_to_steps.up.sql`: Idempotency Key added to step table
- `006_add_human_in_the_loop_step.up.sql`: Human-in-the-loop step support and decision tracking

## Known Issues

### Condition Steps in Forked Branches

When using `Condition` steps within `Fork` branches, the `Join` step may not wait for all dynamically created steps (like `else` branches) to complete before considering the workflow finished. This can lead to premature workflow completion.

**Example of problematic case:**
```go
Fork("parallel_branch", func(branch1 *floxy.Builder) {
    branch1.Step("branch1_step1", "handler").
        Condition("branch1_condition", "{{ gt .count 5 }}", func(elseBranch *floxy.Builder) {
            elseBranch.Step("branch1_else", "handler") // This step might not be waited for
        }).
        Then("branch1_next", "handler")
}, func(branch2 *floxy.Builder) {
    branch2.Step("branch2_step1", "handler").
        Condition("branch2_condition", "{{ lt .count 3 }}", func(elseBranch *floxy.Builder) {
            elseBranch.Step("branch2_else", "handler") // This step might not be waited for
        }).
        Then("branch2_next", "handler")
}).
JoinStep("join", []string{"branch1_step1", "branch2_step1"}, floxy.JoinStrategyAll)
```

**Workaround:** Avoid using `Condition` steps within `Fork` branches, or ensure all possible execution paths are explicitly listed in the `JoinStep` WaitFor array.

See `examples/condition/main.go` for a demonstration of this issue.

## Installation

```bash
go get github.com/rom8726/floxy
```

## Dependencies

- PostgreSQL database
- Go 1.24+
