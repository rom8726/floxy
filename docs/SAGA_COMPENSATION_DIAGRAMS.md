# Saga Compensation Diagrams

This document contains PlantUML diagrams describing the saga compensation mechanism in the Floxy library.

## Sequence Diagram

Shows the step-by-step execution of the compensation process:

<div align="center">
  <img src="saga_seq.png" alt="Saga Compensation Sequence"/>
</div>

## State Diagram

Shows the transitions between step states:

<div align="center">
  <img src="saga_state.png" alt="Saga Compensation State Machine"/>
</div>

## Architecture Diagram

Shows system components and their interactions:

<div align="center">
  <img src="saga_components.png" alt="Saga Compensation Architecture"/>
</div>

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
