package floxy

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
)

type API struct {
	pool *pgxpool.Pool
}

func NewAPI(pool *pgxpool.Pool) *API {
	return &API{pool: pool}
}

func (a *API) GetWorkflowDefinitions(ctx context.Context) ([]WorkflowDefinition, error) {
	const query = `
SELECT id, name, version, definition, created_at
FROM workflows.workflow_definitions
ORDER BY name, version DESC`

	rows, err := a.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var definitions []WorkflowDefinition
	for rows.Next() {
		var def WorkflowDefinition
		var definitionBytes []byte
		err := rows.Scan(
			&def.ID,
			&def.Name,
			&def.Version,
			&definitionBytes,
			&def.CreatedAt,
		)
		if err != nil {
			return nil, err
		}

		if err := json.Unmarshal(definitionBytes, &def.Definition); err != nil {
			return nil, err
		}
		definitions = append(definitions, def)
	}

	return definitions, rows.Err()
}

func (a *API) GetWorkflowDefinition(ctx context.Context, id string) (*WorkflowDefinition, error) {
	const query = `
SELECT id, name, version, definition, created_at
FROM workflows.workflow_definitions
WHERE id = $1`

	var def WorkflowDefinition
	var definitionBytes []byte
	err := a.pool.QueryRow(ctx, query, id).Scan(
		&def.ID,
		&def.Name,
		&def.Version,
		&definitionBytes,
		&def.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(definitionBytes, &def.Definition); err != nil {
		return nil, err
	}

	return &def, nil
}

func (a *API) GetWorkflowInstances(ctx context.Context, workflowID string) ([]WorkflowInstance, error) {
	const query = `
SELECT id, workflow_id, status, input, output, error, 
		started_at, completed_at, created_at, updated_at
FROM workflows.workflow_instances
WHERE workflow_id = $1
ORDER BY created_at DESC`

	rows, err := a.pool.Query(ctx, query, workflowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var instances []WorkflowInstance
	for rows.Next() {
		var instance WorkflowInstance
		err := rows.Scan(
			&instance.ID,
			&instance.WorkflowID,
			&instance.Status,
			&instance.Input,
			&instance.Output,
			&instance.Error,
			&instance.StartedAt,
			&instance.CompletedAt,
			&instance.CreatedAt,
			&instance.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		instances = append(instances, instance)
	}

	return instances, rows.Err()
}

func (a *API) GetAllWorkflowInstances(ctx context.Context) ([]WorkflowInstance, error) {
	const query = `
SELECT id, workflow_id, status, input, output, error, 
		started_at, completed_at, created_at, updated_at
FROM workflows.workflow_instances
ORDER BY created_at DESC`

	rows, err := a.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var instances []WorkflowInstance
	for rows.Next() {
		var instance WorkflowInstance
		err := rows.Scan(
			&instance.ID,
			&instance.WorkflowID,
			&instance.Status,
			&instance.Input,
			&instance.Output,
			&instance.Error,
			&instance.StartedAt,
			&instance.CompletedAt,
			&instance.CreatedAt,
			&instance.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		instances = append(instances, instance)
	}

	return instances, rows.Err()
}

func (a *API) GetWorkflowInstance(ctx context.Context, instanceID int64) (*WorkflowInstance, error) {
	const query = `
SELECT id, workflow_id, status, input, output, error, 
		started_at, completed_at, created_at, updated_at
FROM workflows.workflow_instances
WHERE id = $1`

	var instance WorkflowInstance
	err := a.pool.QueryRow(ctx, query, instanceID).Scan(
		&instance.ID,
		&instance.WorkflowID,
		&instance.Status,
		&instance.Input,
		&instance.Output,
		&instance.Error,
		&instance.StartedAt,
		&instance.CompletedAt,
		&instance.CreatedAt,
		&instance.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	return &instance, nil
}

func (a *API) GetWorkflowSteps(ctx context.Context, instanceID int64) ([]WorkflowStep, error) {
	const query = `
SELECT id, instance_id, step_name, step_type, status, input, output, error,
		retry_count, max_retries, compensation_retry_count,
		started_at, completed_at, created_at
FROM workflows.workflow_steps
WHERE instance_id = $1
ORDER BY created_at`

	rows, err := a.pool.Query(ctx, query, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var steps []WorkflowStep
	for rows.Next() {
		var step WorkflowStep
		err := rows.Scan(
			&step.ID,
			&step.InstanceID,
			&step.StepName,
			&step.StepType,
			&step.Status,
			&step.Input,
			&step.Output,
			&step.Error,
			&step.RetryCount,
			&step.MaxRetries,
			&step.CompensationRetryCount,
			&step.StartedAt,
			&step.CompletedAt,
			&step.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		steps = append(steps, step)
	}

	return steps, rows.Err()
}

func (a *API) GetWorkflowEvents(ctx context.Context, instanceID int64) ([]WorkflowEvent, error) {
	const query = `
SELECT id, instance_id, step_id, event_type, payload, created_at
FROM workflows.workflow_events
WHERE instance_id = $1
ORDER BY created_at`

	rows, err := a.pool.Query(ctx, query, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []WorkflowEvent
	for rows.Next() {
		var event WorkflowEvent
		err := rows.Scan(
			&event.ID,
			&event.InstanceID,
			&event.StepID,
			&event.EventType,
			&event.Payload,
			&event.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}

	return events, rows.Err()
}
