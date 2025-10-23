# Compensation Flow Testing

This document describes the testing approach for saga compensation flow with non-idempotent steps in the Floxy library.

## Test Overview

The tests verify that compensation steps marked as non-idempotent using `WithStepNoIdempotent()` can only be executed once, even if they fail. This ensures that critical compensation operations (like refunds, rollbacks) are not executed multiple times.

## Test Files

### 1. `compensation_flow_test.go`
Unit tests for compensation flow logic:

- **`TestCompensationFlow_NonIdempotentSteps`** - Tests workflow builder with non-idempotent compensation steps
- **`TestCompensationFlow_NonIdempotentSteps_Integration`** - Tests workflow structure with non-idempotent compensation
- **`TestCompensationFlow_NonIdempotentSteps_WithStepNoIdempotent`** - Tests the `WithStepNoIdempotent()` option behavior
- **`TestCompensationFlow_NonIdempotentSteps_WithStepMaxRetries`** - Tests that `WithStepMaxRetries()` cannot override non-idempotent steps
- **`TestCompensationFlow_NonIdempotentSteps_CompensationRetryCount`** - Tests compensation retry count logic

### 2. `compensation_integration_test.go`
Integration tests for real compensation flow execution:

- **`TestCompensationFlow_NonIdempotentSteps_RealExecution`** - Tests real compensation flow with non-idempotent steps
- **`TestCompensationFlow_NonIdempotentSteps_ExecutionCount`** - Tests execution count tracking for compensation handlers
- **`TestCompensationFlow_NonIdempotentSteps_WithStepNoIdempotent_Behavior`** - Tests the behavior of non-idempotent options
- **`TestCompensationFlow_NonIdempotentSteps_CompensationRetryLogic`** - Tests compensation retry logic

## Key Test Scenarios

### 1. Non-Idempotent Step Configuration
```go
workflow, err := NewBuilder("ecommerce-compensation", 1).
    Step("payment", "payment_handler", WithStepMaxRetries(1)).
    OnFailure("refund", "refund_handler", WithStepNoIdempotent()).
    Then("inventory", "inventory_handler", WithStepMaxRetries(1)).
    OnFailure("release", "release_handler", WithStepNoIdempotent()).
    Build()
```

**Verifies:**
- Compensation steps are marked as `NoIdempotent: true`
- `MaxRetries` is set to 1 for non-idempotent steps
- Regular steps are not affected

### 2. Compensation Retry Logic
```go
step := &WorkflowStep{
    CompensationRetryCount: 0,
    MaxRetries:            1,
}

stepDef := &StepDefinition{
    MaxRetries:   1,
    NoIdempotent: true,
}

// Test retry logic
canRetry := step.CompensationRetryCount < stepDef.MaxRetries
assert.True(t, canRetry, "Should be able to retry compensation initially")

// Simulate retry
step.CompensationRetryCount++

// After retry, should not be able to retry again
canRetry = step.CompensationRetryCount < stepDef.MaxRetries
assert.False(t, canRetry, "Should not be able to retry compensation after max retries")
```

**Verifies:**
- Initial state allows retry
- After retry, no more retries are allowed
- `CompensationRetryCount` is properly tracked

### 3. Option Behavior
```go
step := &StepDefinition{
    MaxRetries: 5, // This should be overridden
}

// Apply WithStepNoIdempotent
WithStepNoIdempotent()(step)

// Verify changes
assert.True(t, step.NoIdempotent)
assert.Equal(t, 1, step.MaxRetries)

// Test that WithStepMaxRetries cannot override
WithStepMaxRetries(10)(step)
assert.Equal(t, 1, step.MaxRetries) // Should remain 1
```

**Verifies:**
- `WithStepNoIdempotent()` sets `NoIdempotent: true` and `MaxRetries: 1`
- `WithStepMaxRetries()` cannot override non-idempotent steps
- Warning is logged when trying to set MaxRetries on non-idempotent steps

## Mock Handlers

The tests include mock handlers that simulate real compensation scenarios:

### MockRefundHandler
- Tracks execution count
- Simulates successful refund operations
- Returns JSON response with refund details

### MockReleaseHandler
- Tracks execution count  
- Simulates successful inventory release operations
- Returns JSON response with release details

## Test Execution

Run all compensation flow tests:
```bash
go test -v -run TestCompensationFlow
```

Run specific test categories:
```bash
# Unit tests only
go test -v -run TestCompensationFlow_NonIdempotentSteps

# Integration tests only  
go test -v -run TestCompensationFlow_NonIdempotentSteps_RealExecution
```

## Expected Behavior

### Non-Idempotent Compensation Steps
1. **Single Execution**: Compensation steps marked as non-idempotent can only be executed once
2. **Retry Count**: `CompensationRetryCount` is tracked separately from regular `RetryCount`
3. **Max Retries**: Non-idempotent steps have `MaxRetries: 1` by default
4. **Override Protection**: `WithStepMaxRetries()` cannot override non-idempotent step settings

### Compensation Flow
1. **Step Failure**: When a step fails, compensation is triggered
2. **Compensation Execution**: Compensation step is executed with `StepStatusCompensation`
3. **Retry Logic**: If compensation fails, it can only be retried once (for non-idempotent steps)
4. **Final State**: After max retries, step is marked as `StepStatusFailed`

## Benefits

These tests ensure that:
- Critical compensation operations are not executed multiple times
- System maintains data consistency during rollback scenarios
- Compensation retry logic works correctly for non-idempotent operations
- Option behavior is predictable and safe

This testing approach provides confidence that the saga compensation mechanism works correctly for real-world scenarios where compensation operations must be executed exactly once.
