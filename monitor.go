package floxy

import (
	"context"
	"time"
)

type Monitor struct {
	store *StoreImpl
}

func NewMonitor(store *StoreImpl) *Monitor {
	return &Monitor{store: store}
}

type WorkflowStats struct {
	WorkflowName       string        `json:"workflow_name"`
	Version            int           `json:"version"`
	TotalInstances     int           `json:"total_instances"`
	CompletedInstances int           `json:"completed_instances"`
	FailedInstances    int           `json:"failed_instances"`
	RunningInstances   int           `json:"running_instances"`
	AverageDuration    time.Duration `json:"average_duration"`
}

func (m *Monitor) GetWorkflowStats(ctx context.Context) ([]WorkflowStats, error) {
	const query = `
SELECT 
	name,
	version,
	total_instances,
	completed,
	failed,
	running,
	avg_duration_seconds
FROM workflows.workflow_stats`

	rows, err := m.store.db.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []WorkflowStats
	for rows.Next() {
		var s WorkflowStats
		var avgSeconds *float64

		err := rows.Scan(
			&s.WorkflowName,
			&s.Version,
			&s.TotalInstances,
			&s.CompletedInstances,
			&s.FailedInstances,
			&s.RunningInstances,
			&avgSeconds,
		)
		if err != nil {
			return nil, err
		}

		if avgSeconds != nil {
			s.AverageDuration = time.Duration(*avgSeconds * float64(time.Second))
		}

		stats = append(stats, s)
	}

	return stats, rows.Err()
}

type ActiveWorkflow struct {
	InstanceID     int64          `json:"instance_id"`
	WorkflowID     string         `json:"workflow_id"`
	Status         WorkflowStatus `json:"status"`
	CreatedAt      time.Time      `json:"created_at"`
	Duration       time.Duration  `json:"duration"`
	TotalSteps     int            `json:"total_steps"`
	CompletedSteps int            `json:"completed_steps"`
	FailedSteps    int            `json:"failed_steps"`
	RunningSteps   int            `json:"running_steps"`
}

func (m *Monitor) GetActiveWorkflows(ctx context.Context) ([]ActiveWorkflow, error) {
	const query = `
SELECT 
	id,
	workflow_id,
	status,
	created_at,
	duration_seconds,
	total_steps,
	completed_steps,
	failed_steps,
	running_steps
FROM workflows.active_workflows`

	rows, err := m.store.db.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var workflows []ActiveWorkflow
	for rows.Next() {
		var wf ActiveWorkflow
		var durationSeconds float64

		err := rows.Scan(
			&wf.InstanceID,
			&wf.WorkflowID,
			&wf.Status,
			&wf.CreatedAt,
			&durationSeconds,
			&wf.TotalSteps,
			&wf.CompletedSteps,
			&wf.FailedSteps,
			&wf.RunningSteps,
		)
		if err != nil {
			return nil, err
		}

		wf.Duration = time.Duration(durationSeconds * float64(time.Second))
		workflows = append(workflows, wf)
	}

	return workflows, rows.Err()
}

func (m *Monitor) GetQueueLength(ctx context.Context) (int, error) {
	const query = `SELECT COUNT(*) FROM workflows.workflow_queue WHERE attempted_at IS NULL`

	var count int
	err := m.store.db.QueryRow(ctx, query).Scan(&count)

	return count, err
}

type CleanupService struct {
	store *StoreImpl
}

func NewCleanupService(store *StoreImpl) *CleanupService {
	return &CleanupService{store: store}
}

func (c *CleanupService) CleanupOldWorkflows(ctx context.Context, olderThan time.Duration) (int64, error) {
	const query = `SELECT FROM workflows.cleanup_workflows($1)`

	cutoffTime := time.Now().Add(-olderThan)
	result, err := c.store.db.Exec(ctx, query, cutoffTime)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected(), nil
}
