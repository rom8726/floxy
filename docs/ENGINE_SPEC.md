# Floxy Engine Specification

## Table of Contents

- [1. Overview](#1-overview)
- [2. Core Concepts](#2-core-concepts)
  - [2.1 Workflow Instance](#21-workflow-instance)
  - [2.2 Step Definition](#22-step-definition)
- [3. Step Lifecycle](#3-step-lifecycle)
- [4. Retry Policy](#4-retry-policy)
  - [4.1 Definition](#41-definition)
  - [4.2 Runtime Fields](#42-runtime-fields)
  - [4.3 Idempotency](#43-idempotency)
- [5. Compensation & Rollback](#5-compensation--rollback)
  - [5.1 OnFailure Handler](#51-onfailure-handler)
  - [5.2 Rollback Chain](#52-rollback-chain)
  - [5.3 Condition Step Rollback](#53-condition-step-rollback)
  - [5.4 Save Points](#54-save-points)
- [6. Queue & Event System](#6-queue--event-system)
  - [6.1 Workflow Queue](#61-workflow-queue)
  - [6.2 Workflow Events](#62-workflow-events)
- [7. Concurrency and Control Flow](#7-concurrency-and-control-flow)
  - [7.1 Parallel](#71-parallel)
  - [7.2 Fork / Join](#72-fork--join)
- [8. Human-in-the-Loop Steps](#8-human-in-the-loop-steps)
  - [8.1 Overview](#81-overview)
  - [8.2 Human Step Definition](#82-human-step-definition)
  - [8.3 Human Step Lifecycle](#83-human-step-lifecycle)
  - [8.4 Human Decision States](#84-human-decision-states)
  - [8.5 Making Human Decisions](#85-making-human-decisions)
  - [8.6 Workflow Behavior](#86-workflow-behavior)
  - [8.7 Decision Tracking](#87-decision-tracking)
  - [8.8 Example Usage](#88-example-usage)
- [9. Workflow Control Operations](#9-workflow-control-operations)
  - [9.1 Overview](#91-overview)
  - [9.2 Cancel Workflow](#92-cancel-workflow)
  - [9.3 Abort Workflow](#93-abort-workflow)
  - [9.4 Workflow States](#94-workflow-states)
  - [9.5 Step Status Changes](#95-step-status-changes)
  - [9.6 Event Logging](#96-event-logging)
  - [9.7 Example Usage](#97-example-usage)
- [10. Condition Steps](#10-condition-steps)
  - [10.1 Overview](#101-overview)
  - [10.2 Condition Expression](#102-condition-expression)
  - [10.3 Condition Step Definition](#103-condition-step-definition)
  - [10.4 Execution Flow](#104-execution-flow)
  - [10.5 Data Access](#105-data-access)
  - [10.6 Type Safety](#106-type-safety)
- [11. Dead Letter Queue (DLQ)](#11-dead-letter-queue-dlq)
  - [11.1 Overview](#111-overview)
  - [11.2 DLQ Configuration](#112-dlq-configuration)
  - [11.3 DLQ Behavior](#113-dlq-behavior)
  - [11.4 Compensation Failure Handling](#114-compensation-failure-handling)
  - [11.5 Requeueing from DLQ](#115-requeueing-from-dlq)
  - [11.6 Use Cases](#116-use-cases)
  - [11.7 Example Usage](#117-example-usage)
- [12. State Determinism](#12-state-determinism)
- [12. Failure Semantics](#12-failure-semantics)
- [13. Example Saga Flow](#13-example-saga-flow)
  - [13.1 Basic Saga](#131-basic-saga)
  - [13.2 Saga with Condition Steps](#132-saga-with-condition-steps)
- [14. Design Principles](#14-design-principles)
- [15. Summary](#15-summary)

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
* **Human-in-the-loop** interactive workflow steps.
* **Workflow control operations** for cancellation and abortion.
* Unified runtime for both normal and compensation execution.

---

## 2. Core Concepts

### 2.1 Workflow Instance

A running workflow instance contains:

* Workflow definition (graph of steps).
* Execution context.
* Current instance status:
  - **Terminal**: `completed`, `failed`, `cancelled`, `aborted`
  - **Active**: `pending`, `running`
  - **Suspended**: `dlq` (paused for manual recovery)
* Execution state of each step (see below).

### 2.2 Step Definition

Each step is represented by a `StepDefinition`:

| Field          | Description                                                          |
| -------------- | -------------------------------------------------------------------- |
| `Name`         | Unique step name.                                                    |
| `Type`         | Step type (`task`, `parallel`, `fork`, `join`, `save_point`, `condition`, `human`). |
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

### 2.3 Step Status

Each step has one of the following statuses:

| Status             | Description                                           | Transition                               |
|-------------------|-------------------------------------------------------|------------------------------------------|
| `pending`         | Step is waiting to be executed                        | Start of execution                       |
| `running`         | Step is currently executing                           | After execution starts                    |
| `completed`       | Step executed successfully                             | After successful handler execution       |
| `failed`          | Step failed after retries (Classic Saga mode)         | After retries exhausted in Classic mode   |
| `paused`          | Step paused (DLQ mode or frozen)                      | When DLQ mode enabled or instance frozen  |
| `compensation`    | Compensation handler is being executed                | During rollback in Classic Saga mode      |
| `rolled_back`    | Compensation completed successfully                   | After successful compensation             |
| `skipped`         | Step was skipped (cancel/abort)                       | After cancel/abort operation             |
| `waiting_decision`| Human step waiting for decision                       | After human step execution start          |
| `confirmed`       | Human step approved                                   | After human confirms                     |
| `rejected`        | Human step rejected                                   | After human rejects                       |

---

## 3. Step Lifecycle

Each step transitions through deterministic states:

**Classic Saga Mode:**
```
pending → running → completed
             ↓
            failed → compensation → rolled_back
```

**DLQ Mode:**
```
pending → running → paused (workflow in dlq state)
                   → completed (after requeue and retry)
```

### Transition Rules

| From           | To             | Trigger                            | Mode        |
| -------------- | -------------- | ---------------------------------- | ----------- |
| `pending`      | `running`      | Engine starts execution            | Both        |
| `running`      | `completed`    | Handler succeeds                   | Both        |
| `running`      | `failed`       | Handler returns error              | Classic     |
| `running`      | `paused`       | DLQ mode enabled, retries exhausted| DLQ         |
| `failed`       | `compensation` | Engine begins rollback             | Classic     |
| `compensation` | `rolled_back`  | Compensation succeeds              | Classic     |
| `compensation` | `failed`       | Compensation exhausted all retries | Classic     |
| `paused`       | `pending`      | Requeue from DLQ                   | DLQ         |

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

## 8. Human-in-the-Loop Steps

### 8.1 Overview

`StepTypeHuman` enables interactive workflow steps that pause execution and wait for human decisions. This allows workflows to incorporate human judgment and approval processes.

### 8.2 Human Step Definition

```go
builder.WaitHumanConfirm("approval_step")
```

Human steps are defined using the `WaitHumanConfirm` method in the Builder DSL.

### 8.3 Human Step Lifecycle

Human steps have a specialized lifecycle:

```
pending → running → waiting_decision → (confirmed | rejected)
```

### 8.4 Human Decision States

| State | Description |
|-------|-------------|
| `waiting_decision` | Step is paused, waiting for human input |
| `confirmed` | Human approved the step, workflow continues |
| `rejected` | Human rejected the step, workflow aborts |

### 8.5 Making Human Decisions

Use the `MakeHumanDecision` method to provide human input:

```go
err := engine.MakeHumanDecision(ctx, stepID, "manager@company.com", 
    floxy.HumanDecisionConfirmed, &comment)
```

Parameters:
- `stepID`: ID of the human step
- `decidedBy`: Identifier of the person making the decision
- `decision`: Either `HumanDecisionConfirmed` or `HumanDecisionRejected`
- `comment`: Optional comment explaining the decision

### 8.6 Workflow Behavior

**On Confirmation:**
- Step status → `confirmed`
- Workflow continues to next steps
- Human decision is logged with metadata

**On Rejection:**
- Step status → `rejected`
- Workflow status → `aborted`
- No further steps are executed

### 8.7 Decision Tracking

All human decisions are stored in the `workflow_human_decisions` table:

| Field | Description |
|-------|-------------|
| `step_id` | Reference to the human step |
| `decided_by` | Person who made the decision |
| `decision` | `confirmed` or `rejected` |
| `comment` | Optional decision comment |
| `decided_at` | Timestamp of the decision |

### 8.8 Example Usage

```go
// Define workflow with human approval
workflow, err := floxy.NewBuilder("document-approval", 1).
    Step("process-document", "document-processor").
    WaitHumanConfirm("human-approval").
    Then("approve-document", "approval").
    Build()

// Start workflow
instanceID, err := engine.Start(ctx, "document-approval-v1", input)

// Wait for human step to be ready
steps, _ := engine.GetSteps(ctx, instanceID)
var humanStepID int64
for _, step := range steps {
    if step.StepName == "human-approval" && step.Status == floxy.StepStatusWaitingDecision {
        humanStepID = step.ID
        break
    }
}

// Make human decision
err = engine.MakeHumanDecision(ctx, humanStepID, "manager@company.com", 
    floxy.HumanDecisionConfirmed, &comment)
```

---

## 9. Workflow Control Operations

### 9.1 Overview

Floxy Engine provides two primary workflow control operations for managing running workflows:

- **Cancel Workflow** — Gracefully cancels a workflow with compensation
- **Abort Workflow** — Immediately stops a workflow without compensation

These operations allow external systems or users to control workflow execution based on business requirements or error conditions.

### 9.2 Cancel Workflow

The `CancelWorkflow` operation provides graceful cancellation with rollback semantics:

```go
err := engine.CancelWorkflow(ctx, instanceID, "admin@company.com", "User requested cancellation")
```

**Behavior:**
1. **Status Check** — Verifies workflow is not in terminal state
2. **Active Steps** — Stops all currently running steps (`running` → `skipped`)
3. **Parallel Steps** — Cancels all parallel/fork steps simultaneously using step-level context tracking
4. **Compensation** — Executes rollback chain for completed steps
5. **Final Status** — Sets workflow to `cancelled`

**Parameters:**
- `instanceID` — ID of the workflow instance to cancel
- `requestedBy` — Identifier of the person/system requesting cancellation
- `reason` — Human-readable reason for cancellation

**Compensation Process:**
- Finds the last completed step
- Executes rollback chain from that step to root
- Runs `OnFailure` handlers for each step in reverse order
- If compensation fails, workflow status becomes `failed`

### 9.3 Abort Workflow

The `AbortWorkflow` operation provides immediate termination without compensation:

```go
err := engine.AbortWorkflow(ctx, instanceID, "system@company.com", "Critical error detected")
```

**Behavior:**
1. **Status Check** — Verifies workflow is not in terminal state
2. **Active Steps** — Stops all currently running steps (`running` → `skipped`)
3. **Parallel Steps** — Aborts all parallel/fork steps simultaneously using step-level context tracking
4. **No Compensation** — Skips rollback process entirely
5. **Final Status** — Sets workflow to `aborted`

**Use Cases:**
- Critical system errors requiring immediate stop
- Security violations
- Resource exhaustion
- External system failures

### 9.4 Parallel Steps Handling

Floxy Engine provides robust support for canceling/aborting workflows with parallel execution:

**Context Tracking:**
- Each step execution registers its own cancellation context
- Multiple parallel steps can be tracked simultaneously per workflow instance
- Context registration uses `instanceID + stepID` for unique identification

**Parallel Step Cancellation:**
- **Fork Steps** — All parallel branches are cancelled simultaneously
- **Join Steps** — Cancelled if any of their waiting steps are cancelled
- **Nested Parallelism** — Deep parallel structures are fully supported

**Example Parallel Workflow Cancellation:**
```go
// Workflow: start -> fork(step1, step2) -> join -> final
// When cancelled:
// - step1 and step2 contexts are cancelled simultaneously
// - join step is skipped
// - final step is skipped
// - Compensation runs for completed steps
```

**Implementation Details:**
- Uses `map[instanceID]map[stepID]CancelFunc` for context storage
- `processCancelRequests()` iterates through all contexts for an instance
- All parallel contexts are cancelled atomically
- Memory cleanup happens automatically when steps complete

### 9.5 Workflow States

Both operations respect workflow terminal states:

| Current Status | Cancel | Abort | Result |
|----------------|--------|-------|--------|
| `running` | ✅ | ✅ | Operation succeeds |
| `pending` | ✅ | ✅ | Operation succeeds |
| `completed` | ❌ | ❌ | Error: already in terminal state |
| `failed` | ❌ | ❌ | Error: already in terminal state |
| `cancelled` | ❌ | ❌ | Error: already in terminal state |
| `aborted` | ❌ | ❌ | Error: already in terminal state |

### 9.6 Step Status Changes

During cancellation/abortion, step statuses change as follows:

**Active Steps (running/pending):**
- Status → `skipped`
- Reason → "Stopped due to workflow cancellation/abort"

**Completed Steps (during Cancel only):**
- Status → `rolled_back` (after successful compensation)
- Status → `failed` (if compensation fails)

**Human Steps:**
- `waiting_decision` → `skipped`
- `confirmed` → `rolled_back` (during Cancel)
- `rejected` → unchanged

### 9.7 Event Logging

All control operations generate comprehensive event logs:

**Cancel Events:**
```
Event: cancellation_started
Data: {requested_by: "admin@company.com", reason: "User requested cancellation"}

Event: step_failed
Data: {step_name: "active_step", reason: "workflow_stopped"}

Event: workflow_cancelled
Data: {requested_by: "admin@company.com", reason: "User requested cancellation"}
```

**Abort Events:**
```
Event: abort_started
Data: {requested_by: "system@company.com", reason: "Critical error detected"}

Event: step_failed
Data: {step_name: "active_step", reason: "workflow_stopped"}

Event: workflow_aborted
Data: {requested_by: "system@company.com", reason: "Critical error detected"}
```

### 9.8 Example Usage

**Graceful Cancellation with Compensation:**
```go
// User-initiated cancellation
func handleUserCancellation(ctx context.Context, instanceID int64, userID string) error {
    reason := "User requested cancellation via UI"
    return engine.CancelWorkflow(ctx, instanceID, userID, reason)
}

// System-initiated cancellation (e.g., timeout)
func handleTimeoutCancellation(ctx context.Context, instanceID int64) error {
    reason := "Workflow execution timeout exceeded"
    return engine.CancelWorkflow(ctx, instanceID, "system", reason)
}
```

**Immediate Abortion:**
```go
// Critical error handling
func handleCriticalError(ctx context.Context, instanceID int64, errorMsg string) error {
    reason := fmt.Sprintf("Critical system error: %s", errorMsg)
    return engine.AbortWorkflow(ctx, instanceID, "error-handler", reason)
}

// Security violation
func handleSecurityViolation(ctx context.Context, instanceID int64) error {
    reason := "Security policy violation detected"
    return engine.AbortWorkflow(ctx, instanceID, "security-monitor", reason)
}
```

**Parallel Workflow Cancellation:**
```go
// Cancel workflow with parallel steps
func cancelParallelWorkflow(ctx context.Context, instanceID int64, userID string) error {
    reason := "User cancelled parallel processing workflow"
    return engine.CancelWorkflow(ctx, instanceID, userID, reason)
}

// Example workflow: start -> fork(process1, process2) -> join -> final
// When cancelled:
// - Both process1 and process2 are cancelled simultaneously
// - Join step is skipped
// - Final step is skipped
// - Compensation runs for completed steps (start, fork)
```

**API Integration:**
```go
// REST API endpoint for cancellation
func cancelWorkflowHandler(w http.ResponseWriter, r *http.Request) {
    instanceID := getInstanceIDFromPath(r)
    userID := extractUserFromAuth(r)
    
    var req CancelRequest
    json.NewDecoder(r.Body).Decode(&req)
    
    err := engine.CancelWorkflow(r.Context(), instanceID, userID, req.Reason)
    if err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }
    
    w.WriteHeader(http.StatusNoContent)
}
```

**Monitoring and Alerting:**
```go
// Monitor workflow states
func monitorWorkflows(ctx context.Context) {
    instances, _ := engine.GetActiveInstances(ctx)
    
    for _, instance := range instances {
        if instance.Status == StatusCancelled {
            alertManager.SendAlert("Workflow cancelled", instance.ID)
        }
        if instance.Status == StatusAborted {
            alertManager.SendAlert("Workflow aborted", instance.ID)
        }
    }
}
```

**Testing Parallel Cancellation:**
```go
func TestCancelWorkflowWithParallelSteps(t *testing.T) {
    // Create workflow with parallel steps
    workflowDef, err := NewBuilder("test_parallel", 1).
        Step("start", "handler").
        Fork("fork", func(branch1 *Builder) {
            branch1.Step("parallel1", "slow-handler")
        }, func(branch2 *Builder) {
            branch2.Step("parallel2", "slow-handler")
        }).
        JoinStep("join", []string{"parallel1", "parallel2"}, JoinStrategyAll).
        Then("final", "handler").
        Build()
    
    // Start workflow and let parallel steps begin execution
    instanceID, _ := engine.Start(ctx, "test_parallel-v1", input)
    
    // Cancel while parallel steps are running
    err = engine.CancelWorkflow(ctx, instanceID, "test-user", "test cancellation")
    
    // Verify all parallel steps were cancelled
    steps, _ := engine.GetSteps(ctx, instanceID)
    for _, step := range steps {
        if step.StepName == "parallel1" || step.StepName == "parallel2" {
            assert.Equal(t, StepStatusRolledBack, step.Status)
        }
    }
}
```

---

## 10. Condition Steps

### 10.1 Overview

`StepTypeCondition` enables conditional branching based on runtime data. The condition is evaluated using Go template syntax with built-in comparison functions.

### 10.2 Condition Expression

Conditions use Go templates with the following functions:

| Function | Description                    | Example                    |
|----------|--------------------------------|----------------------------|
| `eq`     | Equality comparison            | `{{ eq .count 5 }}`        |
| `ne`     | Inequality comparison          | `{{ ne .status "active" }}`|
| `gt`     | Greater than (numeric)         | `{{ gt .amount 100 }}`     |
| `lt`     | Less than (numeric)            | `{{ lt .price 50 }}`       |
| `ge`     | Greater than or equal          | `{{ ge .age 18 }}`         |
| `le`     | Less than or equal             | `{{ le .count 10 }}`       |

### 10.3 Condition Step Definition

```go
builder.Condition("check_funds", "{{ gt .balance 100 }}", func(elseBranch *Builder) {
    elseBranch.Step("insufficient_funds", "HandleInsufficientFunds")
}).
Then("process_payment", "ProcessPayment")
```

### 10.4 Execution Flow

1. **Condition Evaluation**: The engine evaluates the condition expression against the current step context.
2. **Branch Selection**: 
   - If `true` → execute `Next` steps
   - If `false` → execute `Else` step (if defined)
3. **Context Passing**: The same input context is passed to the selected branch.

### 10.5 Data Access

Conditions can access:
- **Input data**: `{{ .field_name }}`
- **Nested objects**: `{{ .user.age }}`
- **Step context**: `{{ .instance_id }}`, `{{ .step_name }}`

### 10.6 Type Safety

The engine automatically handles type conversions for numeric comparisons:
- `int`, `int64`, `float32`, `float64` are all supported
- String comparisons use exact matching
- Missing fields default to `0` for numeric operations

---

## 11. Dead Letter Queue (DLQ)

### 11.1 Overview

Floxy Engine supports two different error handling modes for failed steps:

1. **Classic Saga Mode** (default): Automatic rollback with compensation handlers
2. **DLQ Mode**: Manual recovery with paused workflows

When DLQ is enabled for a workflow, failed steps do **not** trigger rollback/compensation. Instead, the workflow is paused in `dlq` status for manual investigation and recovery.

### 11.2 DLQ Configuration

Enable DLQ for a workflow during definition using the `WithDLQEnabled` builder option:

```go
workflow, err := floxy.NewBuilder("my-workflow", 1, floxy.WithDLQEnabled(true)).
    Step("step1", "handler", floxy.WithStepMaxRetries(3)).
    Then("step2", "handler").
    Build()
```

When `WithDLQEnabled(true)` is set:
- Workflow definition includes `DLQEnabled: true` flag
- All failed steps are sent to DLQ instead of rollback
- Compensation handlers are **not** executed on failure
- Workflow status is set to `dlq` (non-terminal, can be resumed)
- All active `running` steps are set to `paused` status
- Instance queue is cleared to prevent further execution

### 11.3 DLQ Behavior

**When a step fails with DLQ enabled:**

1. **Retry Exhausted**: After all retry attempts are exhausted
2. **Step Paused**: Failed step status → `paused` (not `failed`)
3. **DLQ Record Created**: Step information is stored in `workflows.workflow_dlq` table:
   - Instance ID and Workflow ID
   - Step ID, name, and type
   - Original input data
   - Error message
   - Failure reason: "dlq enabled: rollback/compensation skipped"
4. **Workflow Paused**: Instance status → `dlq` (non-terminal, resumable)
5. **Active Steps Frozen**: All `running` steps are set to `paused` status
6. **Queue Cleared**: Instance queue is cleared to prevent further execution
7. **No Rollback**: Compensation and rollback are **completely skipped**

**DLQ Record Structure:**

| Field | Description |
|-------|-------------|
| `instance_id` | Workflow instance ID |
| `workflow_id` | Workflow definition ID |
| `step_id` | Failed step ID |
| `step_name` | Failed step name |
| `step_type` | Type of the failed step |
| `input` | Step input data at time of failure |
| `error` | Error message from step failure |
| `reason` | Why step was sent to DLQ |
| `created_at` | Timestamp when record was created |

### 11.4 Compensation Failure Handling

When DLQ is **not enabled** and compensation fails after exhausting all retries, the failed step is automatically sent to DLQ:

```go
// Workflow without DLQ enabled
workflow, err := floxy.NewBuilder("order", 1).
    Step("charge-payment", "payment").
        OnFailure("refund-payment", "refund", floxy.WithStepMaxRetries(0)).
    Then("ship-order", "shipping").
    Build()
```

**Behavior:**
1. `charge-payment` step fails
2. `refund-payment` compensation runs and fails (MaxRetries = 0)
3. `charge-payment` step is sent to DLQ with reason: "compensation max retries exceeded"
4. Workflow status → `failed`

This ensures that even without explicit DLQ enablement, severe failures are captured for manual resolution.

### 11.5 Requeueing from DLQ

Use the `RequeueFromDLQ` method to restore a failed step from DLQ:

```go
// Requeue with original input
err := engine.RequeueFromDLQ(ctx, dlqID, nil)

// Requeue with modified input
newInput := json.RawMessage(`{"status": "fixed", "data": "corrected"}`)
err := engine.RequeueFromDLQ(ctx, dlqID, &newInput)
```

**Requeue Process:**

1. **Step Reset**: Step status → `pending`
   - Error cleared
   - Retry count reset to 0
   - Compensation retry count reset to 0
   - Timestamps reset (`started_at`, `completed_at` set to NULL)
2. **Input Update**: If `newInput` is provided, step input is updated
3. **Queue Enqueue**: Step is added to workflow queue for execution
4. **Instance Recovery**: Instance status `dlq` → `running`
5. **Join Step Activation**: Any `paused` join steps are set to `pending` for automatic continuation
6. **DLQ Deletion**: DLQ record is removed

**Fork/Join Behavior During Requeue:**
- If join step exists in `paused` status (because instance was in `dlq` when dependencies completed)
- Join transitions to `pending` after requeue
- Workflow can automatically progress past join after requeued step completes

**Implementation:**

The `RequeueFromDLQ` operation is atomic and uses a CTE to ensure:
- DLQ record is locked during the operation
- Step and instance updates happen in a single transaction
- Queue insertion is done after step update
- DLQ record is deleted only after successful requeue

### 11.6 Use Cases

| Use Case | Description |
|----------|-------------|
| **Transient Failures** | External services temporarily unavailable (e.g., payment gateway timeouts) |
| **Data Issues** | Malformed input requiring manual correction (e.g., invalid customer ID) |
| **Infrastructure Failures** | Temporary infrastructure issues (e.g., database connection limits) |
| **Rate Limiting** | External API rate limits requiring human intervention |
| **Business Logic Errors** | Issues requiring code fixes or data corrections |
| **Compensation Failures** | When compensation steps themselves fail after retry exhaustion |

### 11.7 Example Usage

**Basic DLQ Workflow:**

```go
// Define workflow with DLQ
workflow, err := floxy.NewBuilder("payment-processing", 1, floxy.WithDLQEnabled(true)).
    Step("validate-payment", "payment-validator", floxy.WithStepMaxRetries(2)).
    Then("process-payment", "payment-processor", floxy.WithStepMaxRetries(3)).
    Then("notify-user", "notification-service", floxy.WithStepMaxRetries(1)).
    Build()

// Register and start
err = engine.RegisterWorkflow(ctx, workflow)
instanceID, err := engine.Start(ctx, "payment-processing-v1", input)

// Process workflow steps
for {
    empty, err := engine.ExecuteNext(ctx, "worker1")
    if err != nil {
        log.Printf("Error: %v", err)
    }
    if empty {
        break
    }
    time.Sleep(10 * time.Millisecond)
}

// Check for DLQ records (manual query or UI)
// When ready to retry:
err = engine.RequeueFromDLQ(ctx, dlqID, nil)
```

**Requeue with Modified Input:**

```go
// Investigate failure and prepare corrected input
investigationResult := struct {
    PaymentID  string `json:"payment_id"`
    Amount     float64 `json:"amount"`
    Currency   string `json:"currency"`
    Status     string `json:"status"` // corrected from invalid to valid
}{
    PaymentID: "fixed-payment-123",
    Amount:    100.0,
    Currency:  "USD",
    Status:    "valid",
}

newInput, _ := json.Marshal(investigationResult)
err := engine.RequeueFromDLQ(ctx, dlqID, &newInput)
```

**Integration Testing:**

See `dlq_integration_test.go` for comprehensive DLQ testing:
- `TestDLQ_BasicWorkflowFailure`: Tests DLQ record creation on failure
- `TestDLQ_RequeueFromDLQ`: Tests requeuing from DLQ
- `TestDLQ_RequeueWithNewInput`: Tests requeuing with modified input
- `TestDLQ_CompensationMaxRetriesExceeded`: Tests DLQ on compensation failures

---

## 12. State Determinism

Floxy ensures deterministic workflow execution:

* Step transitions are pure and state-based.
* Retries and compensations are predictable.
* `NoIdempotent` steps execute at most once.
* Replays are safe — idempotent steps can repeat without side effects.

---

## 13. Failure Semantics

| Scenario                    | Classic Saga Behavior                                    | DLQ Mode Behavior                                         |
|----------------------------|----------------------------------------------------------|----------------------------------------------------------|
| Handler error              | Step → `failed`, compensation starts                     | Step → `paused`, instance → `dlq`, DLQ record created    |
| Compensation error         | Step remains `compensation`, retry counter incremented   | Not applicable (no compensation in DLQ mode)              |
| Retries exhausted          | Step → `failed`, rollback to SavePoint                    | Step → `paused`, instance → `dlq`, queue cleared           |
| No `OnFailure` defined     | Step → `rolled_back`                                     | Step → `paused`, instance → `dlq`, queue cleared          |
| SavePoint found            | Rollback stops at save point                             | Not applicable (no rollback in DLQ mode)                  |
| Cancel operation           | Step → `skipped`, compensation executed                   | Step → `skipped`, compensation executed (respects mode)   |
| Abort operation            | Step → `skipped`, no compensation                        | Step → `skipped`, no compensation                         |
| DLQ enabled + fork/join    | Standard fork/join behavior                              | Parallel branches complete, join → `paused` if instance in `dlq` |
| Requeue from DLQ           | Not applicable                                           | Instance `dlq` → `running`, join `paused` → `pending`       |

---

## 14. Example Saga Flow

### 14.1 Basic Saga

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

### 14.2 Saga with Condition Steps

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

## 15. Design Principles

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
* **Workflow Control** — external cancellation and abortion capabilities.
* **Human Integration** — interactive workflow steps with decision tracking.
* **Dead Letter Queue** — store failed steps for manual investigation and re-execution.

---

## 16. Summary

Floxy Runtime provides a consistent, lightweight saga orchestration engine:

* **deterministic** — predictable state transitions and rollback behavior,
* **transactional** — saga pattern with compensating transactions,
* **idempotency-aware** — supports both idempotent and one-shot operations,
* **conditionally intelligent** — smart branching and rollback for complex workflows,
* **parallel-safe** — proper handling of condition steps in concurrent flows,
* **human-integrated** — interactive workflow steps with decision tracking,
* **controllable** — external cancellation and abortion capabilities,
* **resilient** — dead letter queue for handling transient failures,
* and **easy to extend** with custom step types or policies.
