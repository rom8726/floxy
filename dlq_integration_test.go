package floxy

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDLQ_BasicWorkflowFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	container, pool := setupTestDatabase(t)
	t.Cleanup(func() {
		pool.Close()
		_ = container.Terminate(context.Background())
	})

	ctx := context.Background()
	engine := NewEngine(pool)
	defer engine.Shutdown()

	// Register handlers
	engine.RegisterHandler(&DLQSimpleTestHandler{})
	engine.RegisterHandler(&DLQFailingTestHandler{})

	// Create workflow with DLQ enabled
	workflowDef, err := NewBuilder("dlq_test", 1, WithDLQEnabled(true)).
		Step("success", "simple-test", WithStepMaxRetries(1)).
		Then("failure", "failing-test", WithStepMaxRetries(1)).
		Then("final", "simple-test", WithStepMaxRetries(1)).
		Build()

	require.NoError(t, err)
	err = engine.RegisterWorkflow(ctx, workflowDef)
	require.NoError(t, err)

	// Start workflow
	input := json.RawMessage(`{"task_id": "TASK-001", "status": "pending"}`)
	instanceID, err := engine.Start(ctx, "dlq_test-v1", input)
	require.NoError(t, err)

	// Execute workflow steps
	for i := 0; i < 20; i++ {
		empty, err := engine.ExecuteNext(ctx, "worker1")
		if err != nil {
			t.Logf("ExecuteNext error: %v", err)
		}
		if empty {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	time.Sleep(time.Second)

	// Check workflow status - should be in DLQ state (not failed)
	time.Sleep(2 * time.Second)
	status, err := engine.GetStatus(ctx, instanceID)
	require.NoError(t, err)
	assert.Equal(t, StatusDLQ, status, "Workflow should be in DLQ state when DLQ mode is enabled")

	// Check that DLQ record was created
	dlqRecords, err := getDLQRecordsForInstance(ctx, pool, instanceID)
	require.NoError(t, err)
	require.Len(t, dlqRecords, 1, "Expected one DLQ record")

	dlqRecord := dlqRecords[0]
	assert.Equal(t, instanceID, dlqRecord.InstanceID)
	assert.Equal(t, "failure", dlqRecord.StepName)
	assert.NotNil(t, dlqRecord.Error)
	assert.Contains(t, *dlqRecord.Error, "intentional failure")

	// Check that failed step is in paused state
	steps, err := engine.GetSteps(ctx, instanceID)
	require.NoError(t, err)

	for _, step := range steps {
		if step.StepName == "failure" {
			assert.Equal(t, StepStatusPaused, step.Status, "Failed step should be paused in DLQ mode")
		}
	}
}

func TestDLQ_RequeueFromDLQ(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	container, pool := setupTestDatabase(t)
	t.Cleanup(func() {
		pool.Close()
		_ = container.Terminate(context.Background())
	})

	ctx := context.Background()
	engine := NewEngine(pool)
	defer engine.Shutdown()

	// Register handlers
	engine.RegisterHandler(&DLQSimpleTestHandler{})

	// Create workflow with DLQ enabled
	workflowDef, err := NewBuilder("dlq_requeue_test", 1, WithDLQEnabled(true)).
		Step("step1", "simple-test", WithStepMaxRetries(1)).
		Then("step2", "simple-test", WithStepMaxRetries(1)).
		Then("step3", "simple-test", WithStepMaxRetries(1)).
		Build()

	require.NoError(t, err)
	err = engine.RegisterWorkflow(ctx, workflowDef)
	require.NoError(t, err)

	// Start workflow
	input := json.RawMessage(`{"task_id": "TASK-002", "status": "pending"}`)
	instanceID, err := engine.Start(ctx, "dlq_requeue_test-v1", input)
	require.NoError(t, err)

	// Execute workflow - let it complete first step
	empty, err := engine.ExecuteNext(ctx, "worker1")
	require.NoError(t, err)
	require.False(t, empty)

	time.Sleep(500 * time.Millisecond)

	// Manually fail step2 and create DLQ record
	_, err = pool.Exec(ctx, `
		UPDATE workflows.workflow_steps 
		SET status = 'failed', error = 'Manual failure for testing'
		WHERE instance_id = $1 AND step_name = 'step2'`, instanceID)
	require.NoError(t, err)

	// Create DLQ record manually
	_, err = pool.Exec(ctx, `
		INSERT INTO workflows.workflow_dlq 
		(instance_id, workflow_id, step_id, step_name, step_type, input, error, reason)
		SELECT $1, $2, id, 'step2', 'task', input, 'Manual failure for testing', 'manual test'
		FROM workflows.workflow_steps 
		WHERE instance_id = $1 AND step_name = 'step2'
		LIMIT 1`, instanceID, "dlq_requeue_test-v1")
	require.NoError(t, err)

	// Get DLQ record
	dlqRecords, err := getDLQRecordsForInstance(ctx, pool, instanceID)
	require.NoError(t, err)
	require.Len(t, dlqRecords, 1)

	dlqID := dlqRecords[0].ID
	t.Logf("Found DLQ record: %d for step: %s", dlqID, dlqRecords[0].StepName)

	// Requeue the failed step
	err = engine.RequeueFromDLQ(ctx, dlqID, nil)
	require.NoError(t, err)

	// Execute workflow steps
	for i := 0; i < 30; i++ {
		empty, err := engine.ExecuteNext(ctx, "worker1")
		if err != nil {
			t.Logf("ExecuteNext error: %v", err)
		}
		if empty {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	time.Sleep(2 * time.Second)

	// Verify DLQ record was deleted
	dlqRecords, err = getDLQRecordsForInstance(ctx, pool, instanceID)
	require.NoError(t, err)
	assert.Len(t, dlqRecords, 0, "DLQ record should be deleted after requeue")

	// Check steps
	steps, err := engine.GetSteps(ctx, instanceID)
	require.NoError(t, err)

	stepStatuses := make(map[string]StepStatus)
	for _, step := range steps {
		stepStatuses[step.StepName] = step.Status
		t.Logf("Step %s: %s", step.StepName, step.Status)
	}

	// Verify step2 status
	assert.Contains(t, []StepStatus{StepStatusCompleted, StepStatusPending, StepStatusRunning}, stepStatuses["step2"])
}

func TestDLQ_RequeueWithNewInput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	container, pool := setupTestDatabase(t)
	t.Cleanup(func() {
		pool.Close()
		_ = container.Terminate(context.Background())
	})

	ctx := context.Background()
	engine := NewEngine(pool)
	defer engine.Shutdown()

	// Register handlers
	engine.RegisterHandler(&DLQSimpleTestHandler{})

	// Create workflow with DLQ enabled
	workflowDef, err := NewBuilder("dlq_newinput_test", 1, WithDLQEnabled(true)).
		Step("start", "simple-test", WithStepMaxRetries(1)).
		Then("retry", "simple-test", WithStepMaxRetries(1)).
		Then("final", "simple-test", WithStepMaxRetries(1)).
		Build()

	require.NoError(t, err)
	err = engine.RegisterWorkflow(ctx, workflowDef)
	require.NoError(t, err)

	// Start workflow
	input := json.RawMessage(`{"task_id": "TASK-003", "value": "old"}`)
	instanceID, err := engine.Start(ctx, "dlq_newinput_test-v1", input)
	require.NoError(t, err)

	// Execute first step to create workflow steps
	empty, err := engine.ExecuteNext(ctx, "worker1")
	require.NoError(t, err)
	require.False(t, empty)

	time.Sleep(500 * time.Millisecond)

	// Manually fail the retry step by marking it as failed
	// This simulates a DLQ scenario
	_, err = pool.Exec(ctx, `
		UPDATE workflows.workflow_steps 
		SET status = 'failed', error = 'Simulated failure'
		WHERE instance_id = $1 AND step_name = 'retry'`, instanceID)
	require.NoError(t, err)

	// Create DLQ record manually
	_, err = pool.Exec(ctx, `
		INSERT INTO workflows.workflow_dlq 
		(instance_id, workflow_id, step_id, step_name, step_type, input, error, reason)
		SELECT $1, $2, id, 'retry', 'task', input, 'Simulated failure', 'manual test'
		FROM workflows.workflow_steps 
		WHERE instance_id = $1 AND step_name = 'retry'
		LIMIT 1`, instanceID, "dlq_newinput_test-v1")
	require.NoError(t, err)

	// Get DLQ record
	dlqRecords, err := getDLQRecordsForInstance(ctx, pool, instanceID)
	require.NoError(t, err)
	require.Len(t, dlqRecords, 1)

	dlqID := dlqRecords[0].ID

	// Requeue with new input
	newInput := json.RawMessage(`{"task_id": "TASK-003", "value": "new"}`)
	err = engine.RequeueFromDLQ(ctx, dlqID, &newInput)
	require.NoError(t, err)

	// Execute workflow steps
	for i := 0; i < 20; i++ {
		empty, err := engine.ExecuteNext(ctx, "worker1")
		if err != nil {
			t.Logf("ExecuteNext error: %v", err)
		}
		if empty {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	time.Sleep(time.Second)

	// Check workflow status
	status, err := engine.GetStatus(ctx, instanceID)
	require.NoError(t, err)

	// Check steps - retry should now be completed with new input
	steps, err := engine.GetSteps(ctx, instanceID)
	require.NoError(t, err)

	for _, step := range steps {
		if step.StepName == "retry" {
			t.Logf("Retry step status: %s", step.Status)
			assert.Contains(t, []StepStatus{StepStatusCompleted, StepStatusPending, StepStatusRunning}, step.Status)
		}
	}

	t.Logf("Final status: %s", status)
}

func TestDLQ_CompensationMaxRetriesExceeded(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	container, pool := setupTestDatabase(t)
	t.Cleanup(func() {
		pool.Close()
		_ = container.Terminate(context.Background())
	})

	ctx := context.Background()
	engine := NewEngine(pool)
	defer engine.Shutdown()

	// Register handlers
	engine.RegisterHandler(&DLQSimpleTestHandler{})
	engine.RegisterHandler(&DLQFailingTestHandler{})
	engine.RegisterHandler(&FailingCompensationHandler{})

	// Create workflow with compensation and low max retries (without DLQ to test compensation failure)
	workflowDef, err := NewBuilder("dlq_compensation_test", 1).
		Step("main", "failing-test", WithStepMaxRetries(0)).
		OnFailure("compensation", "failing-compensation", WithStepMaxRetries(0)).
		Then("final", "simple-test", WithStepMaxRetries(1)).
		Build()

	require.NoError(t, err)
	err = engine.RegisterWorkflow(ctx, workflowDef)
	require.NoError(t, err)

	// Start workflow
	input := json.RawMessage(`{"task_id": "TASK-004", "status": "pending"}`)
	instanceID, err := engine.Start(ctx, "dlq_compensation_test-v1", input)
	require.NoError(t, err)

	// Execute workflow steps
	for i := 0; i < 20; i++ {
		empty, err := engine.ExecuteNext(ctx, "worker1")
		if err != nil {
			t.Logf("ExecuteNext error: %v", err)
		}
		if empty {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	time.Sleep(time.Second)

	// Check workflow status - should be failed
	status, err := engine.GetStatus(ctx, instanceID)
	require.NoError(t, err)
	assert.Equal(t, StatusFailed, status)

	// Check that DLQ record was created due to compensation failure
	dlqRecords, err := getDLQRecordsForInstance(ctx, pool, instanceID)
	require.NoError(t, err)
	require.Len(t, dlqRecords, 1, "Expected one DLQ record due to compensation failure")

	dlqRecord := dlqRecords[0]
	assert.Equal(t, instanceID, dlqRecord.InstanceID)
	assert.Equal(t, "main", dlqRecord.StepName)
	assert.Equal(t, "compensation max retries exceeded", dlqRecord.Reason)
}

// TestDLQ_ForkJoinParallelFlow tests DLQ behavior with parallel fork-join flow
func TestDLQ_ForkJoinParallelFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	container, pool := setupTestDatabase(t)
	t.Cleanup(func() {
		pool.Close()
		_ = container.Terminate(context.Background())
	})

	ctx := context.Background()
	engine := NewEngine(pool)
	defer engine.Shutdown()

	// Register handlers
	engine.RegisterHandler(&DLQSimpleTestHandler{})
	engine.RegisterHandler(&DLQFailingTestHandler{})

	// Create workflow with fork-join and DLQ enabled
	workflowDef, err := NewBuilder("dlq_fork_join_test", 1, WithDLQEnabled(true)).
		Fork("start",
			func(branch *Builder) {
				branch.Step("branch1-step1", "simple-test", WithStepMaxRetries(1)).
					Then("branch1-step2", "simple-test", WithStepMaxRetries(1))
			},
			func(branch *Builder) {
				branch.Step("branch2-failing", "failing-test", WithStepMaxRetries(1)).
					Then("branch2-step2", "simple-test", WithStepMaxRetries(1))
			},
			func(branch *Builder) {
				branch.Step("branch3-step1", "simple-test", WithStepMaxRetries(1)).
					Then("branch3-step2", "simple-test", WithStepMaxRetries(1))
			},
		).
		Join("join", JoinStrategyAll).
		Then("final", "simple-test", WithStepMaxRetries(1)).
		Build()

	require.NoError(t, err)
	err = engine.RegisterWorkflow(ctx, workflowDef)
	require.NoError(t, err)

	// Start workflow
	input := json.RawMessage(`{"task_id": "FORK-JOIN-001", "status": "pending"}`)
	instanceID, err := engine.Start(ctx, "dlq_fork_join_test-v1", input)
	require.NoError(t, err)

	// Execute workflow steps until it hits the failure
	for i := 0; i < 50; i++ {
		empty, err := engine.ExecuteNext(ctx, "worker1")
		if err != nil {
			t.Logf("ExecuteNext error: %v", err)
		}
		if empty {
			// Wait a bit in case of race condition
			time.Sleep(100 * time.Millisecond)
			// Try once more
			empty, _ = engine.ExecuteNext(ctx, "worker1")
			if empty {
				break
			}
			continue
		}
		time.Sleep(50 * time.Millisecond)
	}

	time.Sleep(2 * time.Second)

	// Check workflow status - should be in DLQ state
	status, err := engine.GetStatus(ctx, instanceID)
	require.NoError(t, err)
	assert.Equal(t, StatusDLQ, status, "Workflow should be in DLQ state when one branch fails")

	// Check that DLQ record was created
	dlqRecords, err := getDLQRecordsForInstance(ctx, pool, instanceID)
	require.NoError(t, err)
	require.Len(t, dlqRecords, 1, "Expected one DLQ record for the failing branch")

	dlqRecord := dlqRecords[0]
	assert.Equal(t, instanceID, dlqRecord.InstanceID)
	assert.Equal(t, "branch2-failing", dlqRecord.StepName)
	assert.NotNil(t, dlqRecord.Error)

	// Check that the failing step is paused
	steps, err := engine.GetSteps(ctx, instanceID)
	require.NoError(t, err)

	var failingStep *WorkflowStep
	var joinStep *WorkflowStep
	var otherStepsCompleted int

	for _, step := range steps {
		if step.StepName == "branch2-failing" {
			failingStep = &step
		}
		if step.StepName == "join" {
			joinStep = &step
		}
		if step.StepName != "branch2-failing" && step.StepName != "join" && step.StepName != "final" && step.StepName != "start" {
			if step.Status == StepStatusCompleted {
				otherStepsCompleted++
			}
		}
	}

	require.NotNil(t, failingStep, "Failing step should exist")
	assert.Equal(t, StepStatusPaused, failingStep.Status, "Failing step should be paused")

	// Join step may exist or not depending on execution timing
	// If it exists, it should be paused when the instance is in DLQ state
	if joinStep != nil {
		assert.Equal(t, StepStatusPaused, joinStep.Status, "Join step should be paused when in DLQ mode")
	}

	// At least some other steps should have been completed
	//assert.GreaterOrEqual(t, otherStepsCompleted, 2, "Other branches should have started their steps")

	// Now test requeue
	dlqID := dlqRecords[0].ID
	err = engine.RequeueFromDLQ(ctx, dlqID, nil)
	require.NoError(t, err)

	// Execute workflow steps again
	for i := 0; i < 30; i++ {
		empty, err := engine.ExecuteNext(ctx, "worker1")
		if err != nil {
			t.Logf("ExecuteNext error: %v", err)
		}
		if empty {
			time.Sleep(100 * time.Millisecond)
			empty, _ = engine.ExecuteNext(ctx, "worker1")
			if empty {
				break
			}
			continue
		}
		time.Sleep(50 * time.Millisecond)
	}

	time.Sleep(2 * time.Second)

	// After requeue, workflow should complete or be running or remain in dlq if join is needed
	status, err = engine.GetStatus(ctx, instanceID)
	require.NoError(t, err)
	// Acceptable states after requeue
	assert.Contains(t, []WorkflowStatus{StatusRunning, StatusCompleted, StatusDLQ}, status,
		"Workflow should be running, completed, or in dlq after requeue")

	// Log for debugging
	t.Logf("Final workflow status: %s", status)

	// Check that join step was converted from paused to pending if it existed
	steps, err = engine.GetSteps(ctx, instanceID)
	require.NoError(t, err)

	var foundJoin bool
	for _, step := range steps {
		if step.StepName == "join" {
			foundJoin = true
			assert.NotEqual(t, StepStatusPaused, step.Status, "Join step should not be paused after requeue")
		}
	}

	if !foundJoin {
		t.Log("Join step not yet created, which is acceptable")
	}
}

// Helper function to get DLQ records for an instance
func getDLQRecordsForInstance(ctx context.Context, pool *pgxpool.Pool, instanceID int64) ([]DeadLetterRecord, error) {
	const query = `
		SELECT id, instance_id, workflow_id, step_id, step_name, step_type, 
		       input, error, reason, created_at
		FROM workflows.workflow_dlq
		WHERE instance_id = $1
		ORDER BY created_at DESC`

	rows, err := pool.Query(ctx, query, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := make([]DeadLetterRecord, 0)
	for rows.Next() {
		var rec DeadLetterRecord
		err := rows.Scan(
			&rec.ID,
			&rec.InstanceID,
			&rec.WorkflowID,
			&rec.StepID,
			&rec.StepName,
			&rec.StepType,
			&rec.Input,
			&rec.Error,
			&rec.Reason,
			&rec.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		records = append(records, rec)
	}

	return records, rows.Err()
}

// Test handlers

type DLQSimpleTestHandler struct{}

func (h *DLQSimpleTestHandler) Name() string { return "simple-test" }

func (h *DLQSimpleTestHandler) Execute(ctx context.Context, stepCtx StepContext, input json.RawMessage) (json.RawMessage, error) {
	return input, nil
}

type DLQFailingTestHandler struct{}

func (h *DLQFailingTestHandler) Name() string { return "failing-test" }

func (h *DLQFailingTestHandler) Execute(ctx context.Context, stepCtx StepContext, input json.RawMessage) (json.RawMessage, error) {
	return nil, fmt.Errorf("intentional failure for testing")
}

type FailingCompensationHandler struct{}

func (h *FailingCompensationHandler) Name() string { return "failing-compensation" }

func (h *FailingCompensationHandler) Execute(ctx context.Context, stepCtx StepContext, input json.RawMessage) (json.RawMessage, error) {
	return nil, fmt.Errorf("compensation handler failed")
}
