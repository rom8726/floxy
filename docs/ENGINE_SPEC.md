# Floxy Engine Specification

## 1. Overview

**Floxy Engine** is a lightweight workflow execution engine for Go, designed around deterministic state transitions, compensating transactions, and saga-style rollback semantics.
It provides a clear, composable builder API and a simple, declarative execution model — significantly lighter than Temporal or Cadence.

Key features:

* Deterministic, state-based workflow execution.
* Declarative compensation (saga pattern) with rollback chains.
* Configurable retry and compensation policies.
* Support for non-idempotent steps.
* Partial rollback via **Save Points**.
* **Conditional branching** with `Condition` steps.
* **Smart rollback** for parallel flows with condition steps.
* Unified runtime for both normal and compensation execution.

---

## 2. Core Concepts

### 2.1 Workflow Instance

A running workflow instance contains:

* Workflow definition (graph of steps).
* Execution context.
* Current instance status (`running`, `failed`, `completed`).
* Execution state of each step (see below).

### 2.2 Step Definition

Each step is represented by a `StepDefinition`:

| Field          | Description                                                          |
| -------------- | -------------------------------------------------------------------- |
| `Name`         | Unique step name.                                                    |
| `Type`         | Step type (`task`, `parallel`, `fork`, `join`, `save_point`, `condition`). |
| `Handler`      | Handler function to execute.                                         |
| `OnFailure`    | Optional name of a compensation step.                                |
| `MaxRetries`   | Maximum total number of allowed handler calls (including the first). |
| `NoIdempotent` | Marks step as non-idempotent. Default is `false`.                    |
| `Next`         | List of next step names (normal flow).                               |
| `Else`         | Alternative step name for condition steps (false branch).            |
| `Condition`    | Go template expression for condition evaluation.                     |
| `Parallel`     | List of sub-steps for `parallel` or `fork` types.                    |
| `WaitFor`      | List of steps to wait for in join operations.                       |
| `JoinStrategy` | Join strategy (`all` or `any`).                                     |
| `Metadata`     | Arbitrary user metadata.                                             |

---

## 3. Step Lifecycle

Each step transitions through deterministic states:

```
pending → running → completed
             ↓
            failed → compensation → rolled_back
```

### Transition Rules

| From           | To             | Trigger                            |
| -------------- | -------------- | ---------------------------------- |
| `pending`      | `running`      | Engine starts execution            |
| `running`      | `completed`    | Handler succeeds                   |
| `running`      | `failed`       | Handler returns error              |
| `failed`       | `compensation` | Engine begins rollback             |
| `compensation` | `rolled_back`  | Compensation succeeds              |
| `compensation` | `failed`       | Compensation exhausted all retries |

---

## 4. Retry Policy

### 4.1 Definition

`MaxRetries` defines the **maximum total number of handler calls**, including the first execution.
Retries occur only if the step or compensation handler returns an error and `RetryCount < MaxRetries`.

Examples:

| Setting          | Behavior                                         |
| ---------------- | ------------------------------------------------ |
| `MaxRetries = 1` | One call only (no retries).                      |
| `MaxRetries = 3` | Up to three total calls (1 initial + 2 retries). |

### 4.2 Runtime Fields

* `retry_count` — number of completed handler calls for the step.
* `compensation_retry_count` — number of completed compensation handler calls.

Before each call:

```go
if step.RetryCount >= step.MaxRetries {
    markFailed(step)
    return
}
step.RetryCount++
callHandler(step.Handler)
```

### 4.3 Idempotency

By default, every step is **idempotent**.

To mark a step or its compensation as **non-idempotent**, use:

```go
func WithStepNoIdempotent() StepOption {
    return func(step *StepDefinition) {
        step.NoIdempotent = true
        step.MaxRetries = 1
    }
}
```

This ensures the handler (or compensation handler) will be invoked **exactly once** — even on failure.

---

## 5. Compensation & Rollback

### 5.1 OnFailure Handler

Each step may define a compensation handler via:

```go
Step("charge_card", "ChargeCard").
    OnFailure("refund_card", "RefundCard", WithMaxRetries(3))
```

* The `OnFailure` step is **not** enqueued for normal execution.
* During rollback, the engine locates the failed step, reads its `OnFailure` handler and `MaxRetries`, and executes it as a compensation.
* The original step is moved to `status = 'compensation'`.
* Upon success → `rolled_back`, upon exhaustion → `failed`.

### 5.2 Rollback Chain

On workflow failure, the engine computes the **rollback chain**:

1. Find the nearest `save_point` (if any).
2. Collect all executed steps up to that point.
3. For each step:

    * **Condition steps in parallel flows**: Determine which branch was executed and rollback only that branch.
    * **Regular steps**: If `OnFailure` exists → execute compensation, otherwise → mark as `rolled_back`.

### 5.3 Condition Step Rollback

For condition steps in parallel flows, the engine uses **smart rollback**:

1. **Branch Detection**: Check which branch (Next or Else) was actually executed by examining the step map.
2. **Selective Rollback**: Only rollback steps from the executed branch.
3. **Context Preservation**: Maintain the same rollback context for compensation steps.

```go
// Example: Parallel flow with condition
Fork("parallel_branch", func(branch1 *Builder) {
    branch1.Condition("check_condition", "{{ gt .count 5 }}", func(elseBranch *Builder) {
        elseBranch.Step("else_action", "ElseHandler")
    }).Then("next_action", "NextHandler")
}, func(branch2 *Builder) {
    branch2.Step("other_action", "OtherHandler")
})
```

If `next_action` fails, the engine will:
- Detect that the "Next" branch was executed (not "Else")
- Rollback only `next_action` and `check_condition`
- Skip `else_action` since it was never executed

### 5.4 Save Points

A `StepTypeSavePoint` marks a rollback boundary:

```go
builder.
  Step("reserve_funds", "ReserveFunds").
  SavePoint("after_reserve").
  Step("ship_order", "ShipOrder")
```

If `ship_order` fails, rollback occurs only up to `after_reserve`.

---

## 6. Queue & Event System

### 6.1 Workflow Queue

`workflow_queue` holds pending work items for both normal and compensation execution.

| Field         | Description                    |
| ------------- | ------------------------------ |
| `step_id`     | Reference to `workflow_steps`. |
| `instance_id` | Workflow instance ID.          |
| `status`      | `pending` or `compensation`.   |
| `created_at`  | Timestamp.                     |

### 6.2 Workflow Events

All state changes are persisted as events.

| Field         | Description                                                                                                                               |
| ------------- | ----------------------------------------------------------------------------------------------------------------------------------------- |
| `instance_id` | Workflow instance.                                                                                                                        |
| `step_name`   | Step name.                                                                                                                                |
| `status`      | New state after event.                                                                                                                    |
| `event_type`  | `step_started`, `step_failed`, `compensation_started`, `compensation_retry`, `compensation_success`, `compensation_max_retries_exceeded`. |
| `retry_count` | Current retry counter.                                                                                                                    |
| `error`       | Error message, if any.                                                                                                                    |
| `timestamp`   | Event time.                                                                                                                               |

---

## 7. Concurrency and Control Flow

### 7.1 Parallel

`StepTypeParallel` allows concurrent execution of multiple independent tasks.

```go
Parallel("parallel_group",
    NewTask("task1", "HandlerA"),
    NewTask("task2", "HandlerB"),
)
```

All subtasks must complete before continuing.

### 7.2 Fork / Join

`StepTypeFork` splits execution into multiple branches;
`StepTypeJoin` synchronizes them with configurable strategy:

* `JoinStrategyAll` — wait for all branches.
* `JoinStrategyAny` — proceed after the first completes.

---

## 8. Condition Steps

### 8.1 Overview

`StepTypeCondition` enables conditional branching based on runtime data. The condition is evaluated using Go template syntax with built-in comparison functions.

### 8.2 Condition Expression

Conditions use Go templates with the following functions:

| Function | Description                    | Example                    |
|----------|--------------------------------|----------------------------|
| `eq`     | Equality comparison            | `{{ eq .count 5 }}`        |
| `ne`     | Inequality comparison          | `{{ ne .status "active" }}`|
| `gt`     | Greater than (numeric)         | `{{ gt .amount 100 }}`     |
| `lt`     | Less than (numeric)            | `{{ lt .price 50 }}`       |
| `ge`     | Greater than or equal          | `{{ ge .age 18 }}`         |
| `le`     | Less than or equal             | `{{ le .count 10 }}`       |

### 8.3 Condition Step Definition

```go
builder.Condition("check_funds", "{{ gt .balance 100 }}", func(elseBranch *Builder) {
    elseBranch.Step("insufficient_funds", "HandleInsufficientFunds")
}).
Then("process_payment", "ProcessPayment")
```

### 8.4 Execution Flow

1. **Condition Evaluation**: The engine evaluates the condition expression against the current step context.
2. **Branch Selection**: 
   - If `true` → execute `Next` steps
   - If `false` → execute `Else` step (if defined)
3. **Context Passing**: The same input context is passed to the selected branch.

### 8.5 Data Access

Conditions can access:
- **Input data**: `{{ .field_name }}`
- **Nested objects**: `{{ .user.age }}`
- **Step context**: `{{ .instance_id }}`, `{{ .step_name }}`

### 8.6 Type Safety

The engine automatically handles type conversions for numeric comparisons:
- `int`, `int64`, `float32`, `float64` are all supported
- String comparisons use exact matching
- Missing fields default to `0` for numeric operations

---

## 9. State Determinism

Floxy ensures deterministic workflow execution:

* Step transitions are pure and state-based.
* Retries and compensations are predictable.
* `NoIdempotent` steps execute at most once.
* Replays are safe — idempotent steps can repeat without side effects.

---

## 9. Failure Semantics

| Scenario               | Behavior                                                |
|------------------------| ------------------------------------------------------- |
| Handler error          | Step → `failed`, compensation starts.                   |
| Compensation error     | Step remains `compensation`, retry counter incremented. |
| Retries exhausted      | Step → `failed`.                                        |
| No `OnFailure` defined | Step → `rolled_back`.                                   |
| SavePoint found        | Rollback stops at save point.                           |

---

## 10. Example Saga Flow

### 10.1 Basic Saga

```go
wf := NewBuilder("order_saga", 1).
    Step("reserve_funds", "ReserveFunds").
        OnFailure("refund_funds", "RefundFunds").
    Step("ship_order", "ShipOrder").
        OnFailure("cancel_shipping", "CancelShipping").
    Step("notify_user", "Notify").
    Build()
```

Execution sequence:

1. Run `reserve_funds`, `ship_order`, `notify_user`.
2. If `ship_order` fails:

    * `ship_order` → `failed`
    * Run compensation `cancel_shipping`
    * Then `refund_funds`
    * Mark both as `rolled_back`
    * Instance → `failed`

### 10.2 Saga with Condition Steps

```go
wf := NewBuilder("smart_order_saga", 1).
    Step("validate_order", "ValidateOrder").
    Condition("check_inventory", "{{ gt .inventory_count 0 }}", func(elseBranch *Builder) {
        elseBranch.Step("restock_item", "RestockItem").
            OnFailure("notify_out_of_stock", "NotifyOutOfStock")
    }).
    Then("process_payment", "ProcessPayment").
        OnFailure("refund_payment", "RefundPayment").
    Fork("fulfillment", func(branch1 *Builder) {
        branch1.Step("ship_item", "ShipItem").
            OnFailure("cancel_shipment", "CancelShipment")
    }, func(branch2 *Builder) {
        branch2.Condition("check_digital", "{{ eq .product_type \"digital\" }}", func(elseBranch *Builder) {
            elseBranch.Step("prepare_physical", "PreparePhysical")
        }).Then("deliver_digital", "DeliverDigital")
    }).
    JoinStep("fulfillment_join", []string{"ship_item", "deliver_digital"}, JoinStrategyAll).
    Then("notify_completion", "NotifyCompletion").
    Build()
```

This example demonstrates:
- **Conditional inventory check** with compensation
- **Parallel fulfillment** with different logic for digital vs physical products
- **Smart rollback** that only compensates executed branches

---

## 11. Design Principles

* **Deterministic State Machine** — no flags or side effects outside state transitions.
* **Declarative Compensation** — rollback is described, not coded imperatively.
* **Unified Execution Engine** — same runtime handles normal and compensation steps.
* **Safe Retry Model** — total retry limits, not infinite loops.
* **Idempotency-Aware Execution** — supports both idempotent and one-shot operations.
* **Event-Driven Logging** — every state change produces a durable event.
* **Partial Rollback Support** — rollback to `save_point` when defined.
* **Smart Branch Rollback** — only compensates executed branches in condition steps.
* **Type-Safe Conditions** — automatic type conversion for numeric comparisons.
* **Template-Based Logic** — conditions use familiar Go template syntax.

---

## 12. Summary

Floxy Runtime provides a consistent, lightweight saga orchestration engine:

* **deterministic** — predictable state transitions and rollback behavior,
* **transactional** — saga pattern with compensating transactions,
* **idempotency-aware** — supports both idempotent and one-shot operations,
* **conditionally intelligent** — smart branching and rollback for complex workflows,
* **parallel-safe** — proper handling of condition steps in concurrent flows,
* and **easy to extend** with custom step types or policies.
