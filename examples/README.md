# Floxy Examples

This directory contains various examples demonstrating different complexity levels of workflow orchestration using the Floxy library.

## Prerequisites

1. Start the PostgreSQL database using Docker Compose:
   ```bash
   cd dev
   docker-compose up -d
   ```

2. Run database migrations:
   ```bash
   go run cmd/migrate/main.go
   ```

## Examples

### 1. Hello World (`hello_world/`)
**Complexity: Simple**
- Basic workflow with a single step
- Demonstrates simple task execution
- Shows basic error handling and retries

**Features:**
- Single task handler
- Basic input/output processing
- Simple workflow registration

### 2. E-commerce Order Processing (`ecommerce/`)
**Complexity: Medium**
- Multi-step workflow with error handling
- Demonstrates compensation patterns
- Shows sequential step execution with failure handling

**Features:**
- Payment processing
- Inventory management
- Shipping coordination
- Notification system
- Compensation workflows (refunds)
- Error handling and retries

### 3. Data Processing Pipeline (`data_pipeline/`)
**Complexity: High**
- Complex workflow with parallel processing
- Demonstrates Fork/Join patterns
- Shows data aggregation across multiple sources

**Features:**
- Parallel data extraction from multiple sources
- Data validation and transformation
- Join synchronization
- Data aggregation
- Report generation

### 4. Microservices Orchestration (`microservices/`)
**Complexity: Very High**
- Complex microservices coordination
- Demonstrates advanced patterns
- Shows compensation and saga patterns

**Features:**
- Multiple microservices coordination
- Parallel service calls
- Join synchronization
- Compensation workflows
- Audit and analytics tracking
- Complex error handling

## Running Examples

Each example can be run independently:

```bash
cd examples/hello_world
go mod tidy
go run main.go
```

```bash
cd examples/ecommerce
go mod tidy
go run main.go
```

```bash
cd examples/data_pipeline
go mod tidy
go run main.go
```

```bash
cd examples/microservices
go mod tidy
go run main.go
```

## Database Connection

All examples connect to the PostgreSQL database running in Docker:
- Host: `localhost`
- Port: `5435`
- Database: `floxy`
- Username: `user`
- Password: `password`

## Worker Configuration

Each example uses a WorkerPool with different configurations:
- **Hello World**: 2 workers, 1-second interval
- **E-commerce**: 3 workers, 500ms interval
- **Data Pipeline**: 5 workers, 200ms interval
- **Microservices**: 8 workers, 100ms interval

## Workflow Patterns Demonstrated

1. **Simple Sequential**: Basic step-by-step execution
2. **Error Handling**: Retry mechanisms and failure handling
3. **Compensation**: Rollback and compensation workflows
4. **Parallel Processing**: Fork/Join patterns
5. **Service Orchestration**: Microservices coordination
6. **Data Processing**: ETL-like workflows
7. **Event-Driven**: Notification and analytics tracking

## Monitoring

Each example logs workflow execution details including:
- Step execution status
- Error messages and retries
- Workflow completion status
- Service call results
- Timing information

Use these logs to understand workflow execution flow and debug issues.
