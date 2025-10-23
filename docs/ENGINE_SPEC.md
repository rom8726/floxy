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
| `Type`         | Step type (`task`, `parallel`, `fork`, `join`, `save_point`).        |
| `Handler`      | Handler function to execute.                                         |
| `OnFailure`    | Optional name of a compensation step.                                |
| `MaxRetries`   | Maximum total number of allowed handler calls (including the first). |
| `NoIdempotent` | Marks step as non-idempotent. Default is `false`.                    |
| `Next`         | List of next step names (normal flow).                               |
| `Parallel`     | List of sub-steps for `parallel` or `fork` types.                    |
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
3. For each:

    * If `OnFailure` exists → execute compensation.
    * Otherwise → mark as `rolled_back`.

### 5.3 Save Points

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

## 8. State Determinism

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

---

## 11. Design Principles

* **Deterministic State Machine** — no flags or side effects outside state transitions.
* **Declarative Compensation** — rollback is described, not coded imperatively.
* **Unified Execution Engine** — same runtime handles normal and compensation steps.
* **Safe Retry Model** — total retry limits, not infinite loops.
* **Idempotency-Aware Execution** — supports both idempotent and one-shot operations.
* **Event-Driven Logging** — every state change produces a durable event.
* **Partial Rollback Support** — rollback to `save_point` when defined.

---

## 12. Summary

Floxy Runtime provides a consistent, lightweight saga orchestration engine:

* deterministic,
* transactional,
* idempotency-aware,
* and easy to extend with custom step types or policies.
