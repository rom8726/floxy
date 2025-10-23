# Saga Compensation Diagrams

This document contains PlantUML diagrams describing the saga compensation mechanism in the Floxy library.

## Sequence Diagram

Shows the step-by-step execution of the compensation process:

```plantuml
@startuml Saga Compensation Sequence

title Saga Compensation Flow in Floxy

participant "Engine" as E
participant "Store" as S
participant "StepHandler" as H
participant "CompensationHandler" as CH
participant "Database" as DB

== Normal Step Execution ==
E -> S: ExecuteNext()
S -> DB: DequeueStep()
DB --> S: QueueItem
S --> E: QueueItem

E -> S: GetInstance()
S --> E: WorkflowInstance

E -> S: GetStepsByInstance()
S --> E: WorkflowStep[]

E -> S: UpdateStep(status=running)
S -> DB: UPDATE workflow_steps SET status='running'
DB --> S: OK
S --> E: OK

E -> H: Execute(stepCtx, input)
H --> E: output/error

alt Step Success
    E -> S: UpdateStep(status=completed, output)
    S -> DB: UPDATE workflow_steps SET status='completed'
    DB --> S: OK
    S --> E: OK
else Step Failure
    E -> S: UpdateStep(status=failed, error)
    S -> DB: UPDATE workflow_steps SET status='failed'
    DB --> S: OK
    S --> E: OK
    
    == Rollback Process ==
    E -> E: rollbackToSavePointOrRoot()
    E -> E: rollbackStepChain()
    
    loop For each step in rollback chain
        E -> S: GetWorkflowDefinition()
        S --> E: WorkflowDefinition
        
        E -> E: rollbackStep()
        
        alt Has OnFailure handler
            E -> S: UpdateStepCompensationRetry(\nretryCount++, status=compensation)
            S -> DB: UPDATE workflow_steps SET\ncompensation_retry_count++, status='compensation'
            DB --> S: OK
            S --> E: OK
            
            E -> S: EnqueueStep(stepID)
            S -> DB: INSERT INTO workflow_queue
            DB --> S: OK
            S --> E: OK
            
            E -> S: LogEvent(compensation_started)
            S -> DB: INSERT INTO workflow_events
            DB --> S: OK
            S --> E: OK
        else No OnFailure handler
            E -> S: UpdateStep(status=rolled_back)
            S -> DB: UPDATE workflow_steps SET status='rolled_back'
            DB --> S: OK
            S --> E: OK
        end
    end
    
    E -> S: UpdateInstanceStatus(status=failed)
    S -> DB: UPDATE workflow_instances SET status='failed'
    DB --> S: OK
    S --> E: OK
end

== Compensation Execution ==
E -> S: ExecuteNext()
S -> DB: DequeueStep()
DB --> S: QueueItem (step with status=compensation)
S --> E: QueueItem

E -> S: GetStepsByInstance()
S --> E: WorkflowStep[] (with compensation status)

E -> E: executeCompensationStep()

E -> S: GetWorkflowDefinition()
S --> E: WorkflowDefinition

E -> S: GetStepsByInstance()
S --> E: WorkflowStep[]

E -> CH: Execute(stepCtx, input)
note right: stepCtx.retryCount = step.CompensationRetryCount

alt Compensation Success
    E -> S: UpdateStep(status=rolled_back)
    S -> DB: UPDATE workflow_steps SET status='rolled_back'
    DB --> S: OK
    S --> E: OK
    
    E -> S: LogEvent(compensation_success)
    S -> DB: INSERT INTO workflow_events
    DB --> S: OK
    S --> E: OK
    
else Compensation Failure
    alt Can Retry (CompensationRetryCount < MaxRetries)
        E -> S: UpdateStepCompensationRetry(\nretryCount++, status=compensation)
        S -> DB: UPDATE workflow_steps SET\ncompensation_retry_count++, status='compensation'
        DB --> S: OK
        S --> E: OK
        
        E -> S: EnqueueStep(stepID)
        S -> DB: INSERT INTO workflow_queue
        DB --> S: OK
        S --> E: OK
        
        E -> S: LogEvent(compensation_retry)
        S -> DB: INSERT INTO workflow_events
        DB --> S: OK
        S --> E: OK
        
    else Max Retries Exceeded
        E -> S: UpdateStep(status=failed, error)
        S -> DB: UPDATE workflow_steps SET status='failed'
        DB --> S: OK
        S --> E: OK
        
        E -> S: LogEvent(compensation_max_retries_exceeded)
        S -> DB: INSERT INTO workflow_events
        DB --> S: OK
        S --> E: OK
    end
end

@enduml
```

## State Diagram

Shows the transitions between step states:

```plantuml
@startuml Saga Compensation States

title Saga Compensation State Machine

state "Step Lifecycle" as lifecycle {
    [*] --> Pending : Step Created
    
    Pending --> Running : ExecuteNext()
    Running --> Completed : Handler Success
    Running --> Failed : Handler Error
    
    Completed --> [*] : Workflow Complete
    Failed --> Compensation : Has OnFailure Handler
    Failed --> RolledBack : No OnFailure Handler
}

state "Compensation Process" as compensation {
    Compensation --> Compensation : Retry (if retryCount < maxRetries)
    Compensation --> RolledBack : Success
    Compensation --> Failed : Max Retries Exceeded
}

state "Final States" as final {
    RolledBack : Step Successfully Compensated
    Failed : Step Failed (No Compensation or Max Retries)
}

lifecycle --> compensation
compensation --> final

note right of Pending
  Initial state when step is created
end note

note right of Running
  Step is being executed by handler
end note

note right of Completed
  Step executed successfully
end note

note right of Failed
  Step execution failed
end note

note right of Compensation
  Step is being compensated
  Uses CompensationRetryCount
end note

note right of RolledBack
  Step successfully compensated
  or no compensation needed
end note

@enduml
```

## Architecture Diagram

Shows system components and their interactions:

```plantuml
@startuml Saga Compensation Architecture

title Floxy Saga Compensation Architecture

package "Engine Layer" {
    class Engine {
        +ExecuteNext()
        +executeStep()
        +executeCompensationStep()
        +handleStepFailure()
        +rollbackStep()
        +rollbackToSavePointOrRoot()
    }
    
    class StepHandler {
        +Execute()
    }
    
    class CompensationHandler {
        +Execute()
    }
}

package "Store Layer" {
    interface Store {
        +EnqueueStep()
        +DequeueStep()
        +UpdateStep()
        +UpdateStepCompensationRetry()
        +GetStepsByInstance()
    }
    
    class StoreImpl {
        +EnqueueStep()
        +DequeueStep()
        +UpdateStep()
        +UpdateStepCompensationRetry()
    }
}

package "Models" {
    class WorkflowStep {
        -ID: int64
        -Status: StepStatus
        -RetryCount: int
        -CompensationRetryCount: int
        -MaxRetries: int
    }
    
    class QueueItem {
        -ID: int64
        -InstanceID: int64
        -StepID: *int64
        -ScheduledAt: time.Time
    }
    
    enum StepStatus {
        Pending
        Running
        Completed
        Failed
        Compensation
        RolledBack
        Skipped
    }
}

package "Database" {
    class workflow_steps {
        +id
        +status
        +retry_count
        +compensation_retry_count
        +max_retries
    }
    
    class workflow_queue {
        +id
        +instance_id
        +step_id
        +scheduled_at
    }
    
    class workflow_events {
        +id
        +instance_id
        +step_id
        +event_type
        +payload
    }
}

Engine --> Store : uses
Store --> StoreImpl : implements
StoreImpl --> Database : queries

Engine --> WorkflowStep : manages
Engine --> QueueItem : processes
Engine --> StepStatus : uses

StoreImpl --> workflow_steps : queries
StoreImpl --> workflow_queue : queries
StoreImpl --> workflow_events : queries

Engine --> StepHandler : executes
Engine --> CompensationHandler : executes compensation

note right of Engine
  Main orchestration engine
  Handles step execution and
  compensation logic
end note

note right of Store
  Data access layer
  Abstracts database operations
end note

note right of WorkflowStep
  Core model representing
  a workflow step with
  separate retry counters
end note

note right of StepStatus
  Compensation status
  indicates step is being
  compensated
end note

@enduml
```

## Key Features

### Separate Retry Counters
- `RetryCount` - for normal step execution attempts
- `CompensationRetryCount` - for compensation attempts

### Step Statuses
- `StepStatusCompensation` - step is being compensated
- `StepStatusRolledBack` - step successfully rolled back
- `StepStatusFailed` - compensation exhausted all attempts

### Compensation Logic
1. On step failure, rollback process is initiated
2. If OnFailure handler exists, step is marked as Compensation
3. Compensation executes with retry logic
4. On success, step is marked as RolledBack
5. On failure - retry attempt or final Failed status
