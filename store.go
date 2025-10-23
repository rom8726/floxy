package floxy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/lib/pq"
)

var _ Store = (*StoreImpl)(nil)

type StoreImpl struct {
	db Tx
}

func NewStore(pool *pgxpool.Pool) *StoreImpl {
	return &StoreImpl{db: pool}
}

func (store *StoreImpl) SaveWorkflowDefinition(ctx context.Context, def *WorkflowDefinition) error {
	executor := store.getExecutor(ctx)

	const query = `
INSERT INTO workflows.workflow_definitions (id, name, version, definition, created_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (name, version) DO UPDATE
SET definition = EXCLUDED.definition
RETURNING id, created_at`

	definitionJSON, err := json.Marshal(def.Definition)
	if err != nil {
		return fmt.Errorf("marshal definition: %w", err)
	}

	return executor.QueryRow(ctx, query,
		def.ID, def.Name, def.Version, definitionJSON, time.Now(),
	).Scan(&def.ID, &def.CreatedAt)
}

func (store *StoreImpl) GetWorkflowDefinition(ctx context.Context, id string) (*WorkflowDefinition, error) {
	executor := store.getExecutor(ctx)

	const query = `
SELECT id, name, version, definition, created_at
FROM workflows.workflow_definitions
WHERE id = $1`

	var def WorkflowDefinition
	var definitionJSON []byte

	err := executor.QueryRow(ctx, query, id).Scan(
		&def.ID, &def.Name, &def.Version, &definitionJSON, &def.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(definitionJSON, &def.Definition); err != nil {
		return nil, fmt.Errorf("unmarshal definition: %w", err)
	}

	return &def, nil
}

func (store *StoreImpl) CreateInstance(
	ctx context.Context,
	workflowID string,
	input json.RawMessage,
) (*WorkflowInstance, error) {
	executor := store.getExecutor(ctx)

	const query = `
INSERT INTO workflows.workflow_instances (workflow_id, status, input, created_at, updated_at)
VALUES ($1, $2, $3, $4, $4)
RETURNING id, workflow_id, status, input, created_at, updated_at`

	now := time.Now()
	instance := &WorkflowInstance{}

	err := executor.QueryRow(ctx, query,
		workflowID, StatusPending, input, now,
	).Scan(
		&instance.ID, &instance.WorkflowID, &instance.Status,
		&instance.Input, &instance.CreatedAt, &instance.UpdatedAt,
	)

	return instance, err
}

func (store *StoreImpl) UpdateInstanceStatus(
	ctx context.Context,
	instanceID int64,
	status WorkflowStatus,
	output json.RawMessage,
	errMsg *string,
) error {
	executor := store.getExecutor(ctx)

	const query = `
UPDATE workflows.workflow_instances
SET status = $2, output = $3, error = $4, updated_at = $5,
	completed_at = CASE WHEN $2 IN ('completed', 'failed', 'cancelled') THEN $5 ELSE completed_at END,
	started_at = CASE WHEN started_at IS NULL AND $2 = 'running' THEN $5 ELSE started_at END
WHERE id = $1`

	_, err := executor.Exec(ctx, query, instanceID, status, output, errMsg, time.Now())

	return err
}

func (store *StoreImpl) GetInstance(ctx context.Context, instanceID int64) (*WorkflowInstance, error) {
	executor := store.getExecutor(ctx)

	const query = `
SELECT id, workflow_id, status, input, output, error,
	   started_at, completed_at, created_at, updated_at
FROM workflows.workflow_instances
WHERE id = $1`

	instance := &WorkflowInstance{}
	err := executor.QueryRow(ctx, query, instanceID).Scan(
		&instance.ID, &instance.WorkflowID, &instance.Status,
		&instance.Input, &instance.Output, &instance.Error,
		&instance.StartedAt, &instance.CompletedAt,
		&instance.CreatedAt, &instance.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	return instance, nil
}

func (store *StoreImpl) CreateStep(ctx context.Context, step *WorkflowStep) error {
	executor := store.getExecutor(ctx)

	const query = `
INSERT INTO workflows.workflow_steps 
(instance_id, step_name, step_type, status, input, max_retries, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, created_at`

	return executor.QueryRow(ctx, query,
		step.InstanceID, step.StepName, step.StepType,
		step.Status, step.Input, step.MaxRetries, time.Now(),
	).Scan(&step.ID, &step.CreatedAt)
}

func (store *StoreImpl) UpdateStep(
	ctx context.Context,
	stepID int64,
	status StepStatus,
	output json.RawMessage,
	errMsg *string,
) error {
	executor := store.getExecutor(ctx)

	const query = `
UPDATE workflows.workflow_steps
SET status = $2, output = $3, error = $4,
	completed_at = CASE WHEN $2 IN ('completed', 'failed', 'skipped') THEN $5 ELSE completed_at END,
	started_at = CASE WHEN started_at IS NULL AND $2 = 'running' THEN $5 ELSE started_at END,
	retry_count = CASE WHEN $2 = 'failed' THEN retry_count + 1 ELSE retry_count END
WHERE id = $1`

	_, err := executor.Exec(ctx, query, stepID, status, output, errMsg, time.Now())

	return err
}

func (store *StoreImpl) GetStepsByInstance(ctx context.Context, instanceID int64) ([]*WorkflowStep, error) {
	executor := store.getExecutor(ctx)

	const query = `
SELECT id, instance_id, step_name, step_type, status, input, output, error,
	retry_count, max_retries, started_at, completed_at, created_at
FROM workflows.workflow_steps
WHERE instance_id = $1
ORDER BY created_at`

	rows, err := executor.Query(ctx, query, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var steps []*WorkflowStep
	for rows.Next() {
		step := &WorkflowStep{}
		err := rows.Scan(
			&step.ID, &step.InstanceID, &step.StepName, &step.StepType,
			&step.Status, &step.Input, &step.Output, &step.Error,
			&step.RetryCount, &step.MaxRetries,
			&step.StartedAt, &step.CompletedAt, &step.CreatedAt,
		)
		if err != nil {
			return nil, err
		}

		steps = append(steps, step)
	}

	return steps, rows.Err()
}

func (store *StoreImpl) EnqueueStep(
	ctx context.Context,
	instanceID int64,
	stepID *int64,
	priority int,
	delay time.Duration,
) error {
	executor := store.getExecutor(ctx)

	const query = `
INSERT INTO workflows.workflow_queue (instance_id, step_id, scheduled_at, priority)
VALUES ($1, $2, $3, $4)`

	scheduledAt := time.Now().Add(delay)
	_, err := executor.Exec(ctx, query, instanceID, stepID, scheduledAt, priority)

	return err
}

func (store *StoreImpl) DequeueStep(ctx context.Context, workerID string) (*QueueItem, error) {
	executor := store.getExecutor(ctx)

	const query = `
WITH next_item AS (
	SELECT id
	FROM workflows.workflow_queue
	WHERE scheduled_at <= $1 AND attempted_at IS NULL
	ORDER BY priority DESC, scheduled_at ASC
	LIMIT 1
	FOR UPDATE SKIP LOCKED
)
UPDATE workflows.workflow_queue
SET attempted_at = $1, attempted_by = $2
FROM next_item
WHERE workflows.workflow_queue.id = next_item.id
RETURNING workflows.workflow_queue.id, instance_id, step_id, scheduled_at, attempted_at, attempted_by, priority`

	item := &QueueItem{}
	err := executor.QueryRow(ctx, query, time.Now(), workerID).Scan(
		&item.ID, &item.InstanceID, &item.StepID,
		&item.ScheduledAt, &item.AttemptedAt, &item.AttemptedBy, &item.Priority,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	return item, err
}

func (store *StoreImpl) RemoveFromQueue(ctx context.Context, queueID int64) error {
	executor := store.getExecutor(ctx)

	const query = `DELETE FROM workflows.workflow_queue WHERE id = $1`
	_, err := executor.Exec(ctx, query, queueID)

	return err
}

func (store *StoreImpl) LogEvent(
	ctx context.Context,
	instanceID int64,
	stepID *int64,
	eventType string,
	payload any,
) error {
	executor := store.getExecutor(ctx)

	const query = `
INSERT INTO workflows.workflow_events (instance_id, step_id, event_type, payload, created_at)
VALUES ($1, $2, $3, $4, $5)`

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	_, err = executor.Exec(ctx, query, instanceID, stepID, eventType, payloadJSON, time.Now())

	return err
}

func (store *StoreImpl) CreateJoinState(
	ctx context.Context,
	instanceID int64,
	joinStepName string,
	waitingFor []string,
	strategy JoinStrategy,
) error {
	executor := store.getExecutor(ctx)

	const query = `
INSERT INTO workflows.workflow_join_state
    (instance_id, join_step_name, waiting_for, join_strategy, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $5)
ON CONFLICT (instance_id, join_step_name) DO NOTHING`

	waitingForJSON, err := json.Marshal(waitingFor)
	if err != nil {
		return err
	}

	if strategy == "" {
		strategy = "all"
	}

	_, err = executor.Exec(ctx, query, instanceID, joinStepName, waitingForJSON, strategy, time.Now())

	return err
}

func (store *StoreImpl) UpdateJoinState(
	ctx context.Context,
	instanceID int64,
	joinStepName, completedStep string,
	success bool,
) (bool, error) {
	executor := store.getExecutor(ctx)

	const query = `
SELECT waiting_for, completed, failed, join_strategy
FROM workflows.workflow_join_state
WHERE instance_id = $1 AND join_step_name = $2
FOR UPDATE`

	var waitingForJSON, completedJSON, failedJSON []byte
	var strategy JoinStrategy

	err := executor.QueryRow(ctx, query, instanceID, joinStepName).Scan(
		&waitingForJSON, &completedJSON, &failedJSON, &strategy,
	)
	if err != nil {
		return false, err
	}

	var waitingFor, completed, failed []string
	_ = json.Unmarshal(waitingForJSON, &waitingFor)
	_ = json.Unmarshal(completedJSON, &completed)
	_ = json.Unmarshal(failedJSON, &failed)

	if success {
		completed = append(completed, completedStep)
	} else {
		failed = append(failed, completedStep)
	}

	isReady := store.checkJoinReady(waitingFor, completed, failed, strategy)

	const updateQuery = `
UPDATE workflows.workflow_join_state
SET completed = $1, failed = $2, is_ready = $3, updated_at = $4
WHERE instance_id = $5 AND join_step_name = $6`

	completedJSON, _ = json.Marshal(completed)
	failedJSON, _ = json.Marshal(failed)

	_, err = executor.Exec(ctx, updateQuery,
		completedJSON, failedJSON, isReady, time.Now(), instanceID, joinStepName,
	)
	if err != nil {
		return false, err
	}

	return isReady, nil
}

func (store *StoreImpl) GetJoinState(ctx context.Context, instanceID int64, joinStepName string) (*JoinState, error) {
	executor := store.getExecutor(ctx)

	const query = `
SELECT instance_id, join_step_name, waiting_for, completed, failed, join_strategy, is_ready, created_at, updated_at
FROM workflows.workflow_join_state
WHERE instance_id = $1 AND join_step_name = $2`

	var state JoinState
	var waitingForJSON, completedJSON, failedJSON []byte

	err := executor.QueryRow(ctx, query, instanceID, joinStepName).Scan(
		&state.InstanceID,
		&state.JoinStepName,
		&waitingForJSON,
		&completedJSON,
		&failedJSON,
		&state.JoinStrategy,
		&state.IsReady,
		&state.CreatedAt,
		&state.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	_ = json.Unmarshal(waitingForJSON, &state.WaitingFor)
	_ = json.Unmarshal(completedJSON, &state.Completed)
	_ = json.Unmarshal(failedJSON, &state.Failed)

	return &state, nil
}

func (store *StoreImpl) checkJoinReady(waitingFor, completed, failed []string, strategy JoinStrategy) bool {
	if strategy == JoinStrategyAny {
		return len(completed) > 0 || len(failed) > 0
	}

	totalProcessed := len(completed) + len(failed)

	return totalProcessed >= len(waitingFor)
}

func (store *StoreImpl) UpdateStepCompensationRetry(
	ctx context.Context,
	stepID int64,
	retryCount int,
	status StepStatus,
) error {
	executor := store.getExecutor(ctx)

	const query = `
UPDATE workflows.workflow_steps 
SET compensation_retry_count = $1, status = $2
WHERE id = $3`

	_, err := executor.Exec(ctx, query, retryCount, string(status), stepID)
	return err
}

func (store *StoreImpl) getExecutor(ctx context.Context) Tx {
	if tx := TxFromContext(ctx); tx != nil {
		return tx
	}

	return store.db
}
