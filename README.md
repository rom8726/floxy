# floxy

A Go library for creating and executing workflows with a custom DSL. Implements the Saga pattern with orchestrator approach, providing transaction management and compensation capabilities.

## Features

- **Workflow DSL**: Declarative workflow definition using Builder pattern
- **Saga Pattern**: Orchestrator-based saga implementation with compensation
- **Transaction Management**: Built-in transaction support with rollback capabilities
- **Parallel Execution**: Fork/Join patterns for concurrent workflow steps
- **Error Handling**: Automatic retry mechanisms and failure compensation
- **PostgreSQL Storage**: Persistent workflow state and event logging

## Quick Start

```go
package main

import (
    "context"
    "encoding/json"
    "log"
    
    "github.com/rom8726/floxy"
)

func main() {
    // Create engine with database connection
    pool, _ := pgxpool.New(context.Background(), "postgres://...")
    store := floxy.NewStore(pool)
    txManager := floxy.NewTxManager(pool)
    engine := floxy.NewEngine(txManager, store)
    
    // Define workflow using Builder DSL
    workflow, _ := floxy.NewBuilder("order-processing", 1).
        Step("validate-order", "validator", floxy.WithStepMaxRetries(3)).
        OnFailure("compensate-validation", "compensation").
        Then("process-payment", "payment", floxy.WithStepMaxRetries(2)).
        OnFailure("refund-payment", "refund").
        Then("ship-order", "shipping").
        Build()
    
    // Register and start workflow
    engine.RegisterWorkflow(context.Background(), workflow)
    instanceID, _ := engine.Start(context.Background(), "order-processing-v1", input)
    
    log.Printf("Workflow started: %d", instanceID)
}
```

## Examples

See the `examples/` directory for complete workflow examples:

- **Hello World**: Basic single-step workflow
- **E-commerce**: Order processing with compensation
- **Data Pipeline**: Parallel data processing with joins
- **Microservices**: Complex service orchestration

## Installation

```bash
go get github.com/rom8726/floxy
```
