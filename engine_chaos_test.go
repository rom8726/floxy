package floxy

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rom8726/chaoskit"
	"github.com/rom8726/chaoskit/injectors"
	chaostesting "github.com/rom8726/chaoskit/testing"
	"github.com/rom8726/chaoskit/validators"
	"github.com/stretchr/testify/require"
)

func TestNestedWithChaos(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	store, txManager, cleanup := setupTestStore(t)
	t.Cleanup(cleanup)

	pool := txManager.(*TxManagerImpl).pool

	workflowDef := nestedWorkflow(t)
	input := map[string]any{
		"order_id": fmt.Sprintf("ORD-%d", time.Now().UnixNano()),
		"user_id":  fmt.Sprintf("user-%d", rand.Intn(1000)),
		"amount":   float64(rand.Intn(500) + 50),
		"items":    rand.Intn(10) + 1,
	}
	target, err := NewFloxyStressTarget(store, txManager, workflowDef, input)
	require.NoError(t, err)
	defer func() { _ = target.engine.Shutdown(time.Second) }()

	chaostesting.RunChaos(t,
		"nested-workflow-chaos",
		target,
		func(builder *chaoskit.ScenarioBuilder) *chaoskit.ScenarioBuilder {
			builder.
				Step("execute-workflow", RunChaosTestWorkflow).
				Inject("context-delay", injectors.RandomDelayWithProbability(time.Millisecond*20, time.Millisecond*100, 0.15)).
				Inject("context-panic", injectors.PanicProbability(0.15)).
				Inject("context-error", injectors.ErrorWithProbability("chaos error", 0.35)).
				Assert("no-slow-iteration", validators.NoSlowIteration(5*time.Second)).
				Assert("no-infinite-loops", validators.NoInfiniteLoop(10*time.Second)).
				// Database consistency validators
				Assert("queue-empty", NewQueueEmptyValidator(pool)).
				Assert("completed-instances-consistency", NewCompletedInstancesValidator(pool)).
				Assert("failed-instances-consistency", NewFailedInstancesValidator(pool))

			return builder
		},
		chaostesting.WithRepeat(20),
		chaostesting.WithStrictThresholds(),
	)
}

func RunChaosTestWorkflow(ctx context.Context, target chaoskit.Target) error {
	floxyTarget, ok := target.(*FloxyStressTarget)
	if !ok {
		return fmt.Errorf("target is not FloxyStressTarget")
	}

	return floxyTarget.Execute(ctx)
}

// QueueEmptyValidator checks that the workflow_queue table is empty
type QueueEmptyValidator struct {
	pool *pgxpool.Pool
}

func NewQueueEmptyValidator(pool *pgxpool.Pool) *QueueEmptyValidator {
	return &QueueEmptyValidator{pool: pool}
}

func (v *QueueEmptyValidator) Name() string {
	return "queue-empty"
}

func (v *QueueEmptyValidator) Severity() chaoskit.ValidationSeverity {
	return chaoskit.SeverityCritical
}

func (v *QueueEmptyValidator) Validate(ctx context.Context, target chaoskit.Target) error {
	var count int
	err := v.pool.QueryRow(ctx, "SELECT COUNT(*) FROM workflows.workflow_queue").Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to query workflow_queue: %w", err)
	}

	if count > 0 {
		return fmt.Errorf("workflow_queue is not empty: found %d entries", count)
	}

	return nil
}

// CompletedInstancesValidator checks that for completed instances, all steps are also completed
type CompletedInstancesValidator struct {
	pool *pgxpool.Pool
}

func NewCompletedInstancesValidator(pool *pgxpool.Pool) *CompletedInstancesValidator {
	return &CompletedInstancesValidator{pool: pool}
}

func (v *CompletedInstancesValidator) Name() string {
	return "completed-instances-consistency"
}

func (v *CompletedInstancesValidator) Severity() chaoskit.ValidationSeverity {
	return chaoskit.SeverityCritical
}

func (v *CompletedInstancesValidator) Validate(ctx context.Context, target chaoskit.Target) error {
	// Find all completed instances that have steps not in completed state
	query := `
SELECT DISTINCT wi.id, wi.workflow_id
FROM workflows.workflow_instances wi
INNER JOIN workflows.workflow_steps ws ON wi.id = ws.instance_id
WHERE wi.status = 'completed'
  AND ws.status != 'completed'`

	rows, err := v.pool.Query(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to query completed instances: %w", err)
	}
	defer rows.Close()

	var violations []string
	for rows.Next() {
		var instanceID int64
		var workflowID string
		if err := rows.Scan(&instanceID, &workflowID); err != nil {
			return fmt.Errorf("failed to scan row: %w", err)
		}
		violations = append(violations, fmt.Sprintf("instance_id=%d workflow_id=%s", instanceID, workflowID))
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating rows: %w", err)
	}

	if len(violations) > 0 {
		return fmt.Errorf("found %d completed instances with non-completed steps: %v", len(violations), violations)
	}

	return nil
}

// FailedInstancesValidator checks step states for failed instances
type FailedInstancesValidator struct {
	pool *pgxpool.Pool
}

func NewFailedInstancesValidator(pool *pgxpool.Pool) *FailedInstancesValidator {
	return &FailedInstancesValidator{pool: pool}
}

func (v *FailedInstancesValidator) Name() string {
	return "failed-instances-consistency"
}

func (v *FailedInstancesValidator) Severity() chaoskit.ValidationSeverity {
	return chaoskit.SeverityCritical
}

func (v *FailedInstancesValidator) Validate(ctx context.Context, target chaoskit.Target) error {
	// Get all failed instances
	instancesQuery := `
SELECT id, workflow_id
FROM workflows.workflow_instances
WHERE status = 'failed'`

	instancesRows, err := v.pool.Query(ctx, instancesQuery)
	if err != nil {
		return fmt.Errorf("failed to query failed instances: %w", err)
	}
	defer instancesRows.Close()

	var violations []string

	for instancesRows.Next() {
		var instanceID int64
		var workflowID string
		if err := instancesRows.Scan(&instanceID, &workflowID); err != nil {
			return fmt.Errorf("failed to scan instance row: %w", err)
		}

		// Check if there's a savepoint for this instance
		// Get the latest savepoint (by created_at DESC, then by id DESC as tiebreaker)
		var savepointID *int64
		var savepointCreatedAt *string
		savepointQuery := `
			SELECT id, created_at::text
			FROM workflows.workflow_steps
			WHERE instance_id = $1 AND step_type = 'save_point'
			ORDER BY created_at DESC, id DESC
			LIMIT 1`
		err := v.pool.QueryRow(ctx, savepointQuery, instanceID).Scan(&savepointID, &savepointCreatedAt)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("failed to query savepoint for instance %d: %w", instanceID, err)
		}

		if savepointID == nil {
			// No savepoint - all steps must be in the rolled_back state
			nonRolledBackQuery := `
				SELECT id, step_name, status
				FROM workflows.workflow_steps
				WHERE instance_id = $1 AND status != 'rolled_back' AND status != 'skipped'`
			stepsRows, err := v.pool.Query(ctx, nonRolledBackQuery, instanceID)
			if err != nil {
				return fmt.Errorf("failed to query steps for instance %d: %w", instanceID, err)
			}

			var nonRolledBackSteps []string
			for stepsRows.Next() {
				var stepID int64
				var stepName, status string
				if err := stepsRows.Scan(&stepID, &stepName, &status); err != nil {
					stepsRows.Close()
					return fmt.Errorf("failed to scan step row: %w", err)
				}
				nonRolledBackSteps = append(nonRolledBackSteps, fmt.Sprintf("step_id=%d step_name=%s status=%s", stepID, stepName, status))
			}
			stepsRows.Close()

			if err := stepsRows.Err(); err != nil {
				return fmt.Errorf("error iterating steps: %w", err)
			}

			if len(nonRolledBackSteps) > 0 {
				violations = append(violations, fmt.Sprintf("instance_id=%d workflow_id=%s (no savepoint): non-rolled_back steps: %v", instanceID, workflowID, nonRolledBackSteps))
			}
		} else {
			// Savepoint exists - check that all steps created AFTER the latest savepoint are in rolled_back state
			// Steps created before or at the savepoint time (including the savepoint itself) are not checked
			nonRolledBackQuery := `
				SELECT ws.id, ws.step_name, ws.status, ws.created_at::text
				FROM workflows.workflow_steps ws
				WHERE ws.instance_id = $1
				  AND ws.status != 'rolled_back' AND status != 'skipped'
				  AND ws.created_at > (SELECT created_at FROM workflows.workflow_steps WHERE id = $2)
				ORDER BY ws.created_at`
			stepsRows, err := v.pool.Query(ctx, nonRolledBackQuery, instanceID, *savepointID)
			if err != nil {
				return fmt.Errorf("failed to query steps for instance %d: %w", instanceID, err)
			}

			var nonRolledBackSteps []string
			for stepsRows.Next() {
				var stepID int64
				var stepName, status, createdAt string
				if err := stepsRows.Scan(&stepID, &stepName, &status, &createdAt); err != nil {
					stepsRows.Close()
					return fmt.Errorf("failed to scan step row: %w", err)
				}
				nonRolledBackSteps = append(nonRolledBackSteps, fmt.Sprintf("step_id=%d step_name=%s status=%s created_at=%s", stepID, stepName, status, createdAt))
			}
			stepsRows.Close()

			if err := stepsRows.Err(); err != nil {
				return fmt.Errorf("error iterating steps: %w", err)
			}

			if len(nonRolledBackSteps) > 0 {
				violations = append(violations, fmt.Sprintf("instance_id=%d workflow_id=%s (latest savepoint_id=%d created_at=%s): non-rolled_back steps created after savepoint: %v", instanceID, workflowID, *savepointID, *savepointCreatedAt, nonRolledBackSteps))
			}
		}
	}

	if err := instancesRows.Err(); err != nil {
		return fmt.Errorf("error iterating instances: %w", err)
	}

	if len(violations) > 0 {
		return fmt.Errorf("found %d failed instances with inconsistent step states:\n%v", len(violations), violations)
	}

	return nil
}
