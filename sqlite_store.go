package floxy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Ensure interface compliance
var _ Store = (*SQLiteStore)(nil)

// SQLiteStore provides a lightweight Store backed by SQLite.
// It implements only the subset of capabilities required by the SQLite tests.
// Non‑essential methods return a not-implemented error.
type SQLiteStore struct {
	db           *sql.DB
	agingEnabled bool
	agingRate    float64
	mu           sync.Mutex // serialize critical sections for SQLite
}

// NewSQLiteInMemoryStore creates an in-memory SQLite database and initializes schema.
func NewSQLiteInMemoryStore() (*SQLiteStore, error) {
	// Use a unique in-memory database per store to avoid cross-test leakage
	dsn := ":memory:"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// pragma for better concurrency in tests
	_, _ = db.Exec("PRAGMA journal_mode=WAL;")
	_, _ = db.Exec("PRAGMA foreign_keys=ON;")
	_, _ = db.Exec("PRAGMA busy_timeout=5000;")
	// single connection to keep :memory: consistent and avoid locks
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	store := &SQLiteStore{db: db, agingEnabled: true, agingRate: 0.5}
	if err := RunSQLiteMigrations(context.Background(), db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStore) SetAgingEnabled(enabled bool) { s.agingEnabled = enabled }
func (s *SQLiteStore) SetAgingRate(rate float64)    { s.agingRate = rate }

// Definitions
func (s *SQLiteStore) SaveWorkflowDefinition(ctx context.Context, def *WorkflowDefinition) error {
	definitionJSON, err := json.Marshal(def.Definition)
	if err != nil {
		return err
	}
	q := `INSERT INTO workflow_definitions (id, name, version, definition, created_at)
		VALUES(?, ?, ?, ?, ?)
		ON CONFLICT(name, version) DO UPDATE SET definition=excluded.definition`
	if _, err := s.db.ExecContext(ctx, q, def.ID, def.Name, def.Version, definitionJSON, time.Now()); err != nil {
		return err
	}
	// We keep provided ID/CreatedAt; in SQLite we don't fetch RETURNING here.
	return nil
}

func (s *SQLiteStore) GetWorkflowDefinition(ctx context.Context, id string) (*WorkflowDefinition, error) {
	q := `SELECT id, name, version, definition, created_at FROM workflow_definitions WHERE id=?`
	row := s.db.QueryRowContext(ctx, q, id)
	var def WorkflowDefinition
	var defJSON []byte
	if err := row.Scan(&def.ID, &def.Name, &def.Version, &defJSON, &def.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrEntityNotFound
		}
		return nil, err
	}
	if err := json.Unmarshal(defJSON, &def.Definition); err != nil {
		return nil, err
	}
	return &def, nil
}

// Instances
func (s *SQLiteStore) CreateInstance(ctx context.Context, workflowID string, input json.RawMessage) (*WorkflowInstance, error) {
	now := time.Now()
	q := `INSERT INTO workflow_instances (workflow_id, status, input, created_at, updated_at)
		VALUES(?, ?, ?, ?, ?)`
	res, err := s.db.ExecContext(ctx, q, workflowID, StatusPending, input, now, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.GetInstance(ctx, id)
}

func (s *SQLiteStore) UpdateInstanceStatus(ctx context.Context, instanceID int64, status WorkflowStatus, output json.RawMessage, errMsg *string) error {
	q := `UPDATE workflow_instances SET status=?, output=?, error=?, updated_at=? WHERE id=?`
	_, err := s.db.ExecContext(ctx, q, status, output, errMsg, time.Now(), instanceID)
	return err
}

func (s *SQLiteStore) GetInstance(ctx context.Context, instanceID int64) (*WorkflowInstance, error) {
	q := `SELECT id, workflow_id, status, input, output, error, started_at, completed_at, created_at, updated_at
		FROM workflow_instances WHERE id=?`
	row := s.db.QueryRowContext(ctx, q, instanceID)
	var inst WorkflowInstance
	var inputBytes, outputBytes []byte
	if err := row.Scan(
		&inst.ID, &inst.WorkflowID, &inst.Status, &inputBytes, &outputBytes, &inst.Error,
		&inst.StartedAt, &inst.CompletedAt, &inst.CreatedAt, &inst.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrEntityNotFound
		}
		return nil, err
	}
	inst.Input = json.RawMessage(inputBytes)
	if outputBytes != nil {
		inst.Output = json.RawMessage(outputBytes)
	} else {
		inst.Output = nil
	}
	return &inst, nil
}

// Steps
func (s *SQLiteStore) CreateStep(ctx context.Context, step *WorkflowStep) error {
	now := time.Now()
	q := `INSERT INTO workflow_steps (
		instance_id, step_name, step_type, status, input, output, error, retry_count,
		max_retries, compensation_retry_count, idempotency_key, started_at, completed_at, created_at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	res, err := s.db.ExecContext(ctx, q,
		step.InstanceID, step.StepName, step.StepType, step.Status, step.Input, step.Output, step.Error,
		step.RetryCount, step.MaxRetries, step.CompensationRetryCount, step.IdempotencyKey,
		step.StartedAt, step.CompletedAt, now,
	)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	step.ID = id
	step.CreatedAt = now
	return nil
}

func (s *SQLiteStore) UpdateStep(ctx context.Context, stepID int64, status StepStatus, output json.RawMessage, errMsg *string) error {
	q := `UPDATE workflow_steps SET status=?, output=?, error=?, completed_at=CASE WHEN ? IN ('completed','rolled_back','failed') THEN ? ELSE completed_at END WHERE id=?`
	_, err := s.db.ExecContext(ctx, q, status, output, errMsg, status, time.Now(), stepID)
	return err
}

func (s *SQLiteStore) GetStepsByInstance(ctx context.Context, instanceID int64) ([]WorkflowStep, error) {
	q := `SELECT id, instance_id, step_name, step_type, status, input, output, error, retry_count,
		max_retries, compensation_retry_count, idempotency_key, started_at, completed_at, created_at
		FROM workflow_steps WHERE instance_id=? ORDER BY id`
	rows, err := s.db.QueryContext(ctx, q, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []WorkflowStep
	for rows.Next() {
		var srec WorkflowStep
		var inputBytes, outputBytes []byte
		if err := rows.Scan(&srec.ID, &srec.InstanceID, &srec.StepName, &srec.StepType, &srec.Status,
			&inputBytes, &outputBytes, &srec.Error, &srec.RetryCount, &srec.MaxRetries,
			&srec.CompensationRetryCount, &srec.IdempotencyKey, &srec.StartedAt, &srec.CompletedAt, &srec.CreatedAt); err != nil {
			return nil, err
		}
		srec.Input = json.RawMessage(inputBytes)
		if outputBytes != nil {
			srec.Output = json.RawMessage(outputBytes)
		}
		res = append(res, srec)
	}
	return res, nil
}

// Queue
func (s *SQLiteStore) EnqueueStep(ctx context.Context, instanceID int64, stepID *int64, priority Priority, delay time.Duration) error {
	sched := time.Now().Add(delay)
	q := `INSERT INTO queue (instance_id, step_id, scheduled_at, priority) VALUES(?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, q, instanceID, stepID, sched, int(priority))
	return err
}

func (s *SQLiteStore) UpdateStepCompensationRetry(ctx context.Context, stepID int64, retryCount int, status StepStatus) error {
	q := `UPDATE workflow_steps SET compensation_retry_count=?, status=? WHERE id=?`
	_, err := s.db.ExecContext(ctx, q, retryCount, status, stepID)
	return err
}

func (s *SQLiteStore) DequeueStep(ctx context.Context, workerID string) (*QueueItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Simple transactional dequeue.
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	orderExpr := "priority"
	if s.agingEnabled {
		// effective_priority = min(100, priority + floor(wait_seconds * rate))
		orderExpr = fmt.Sprintf("MIN(100, priority + CAST(((strftime('%%s','now') - strftime('%%s', scheduled_at)) * %.6f) AS INTEGER))", s.agingRate)
	}

	row := tx.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT id, instance_id, step_id, scheduled_at, attempted_at, attempted_by, priority
		FROM queue
		WHERE scheduled_at <= ? AND (attempted_by IS NULL)
		ORDER BY %s DESC, scheduled_at ASC, id ASC
		LIMIT 1`, orderExpr), time.Now())
	var qi QueueItem
	if err := row.Scan(&qi.ID, &qi.InstanceID, &qi.StepID, &qi.ScheduledAt, &qi.AttemptedAt, &qi.AttemptedBy, &qi.Priority); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	// mark as attempted by worker
	now := time.Now()
	_, err = tx.ExecContext(ctx, `UPDATE queue SET attempted_at=?, attempted_by=? WHERE id=?`, now, workerID, qi.ID)
	if err != nil {
		return nil, err
	}
	qi.AttemptedAt = &now
	qi.AttemptedBy = &workerID

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return &qi, nil
}

func (s *SQLiteStore) RemoveFromQueue(ctx context.Context, queueID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM queue WHERE id=?`, queueID)
	return err
}

func (s *SQLiteStore) ReleaseQueueItem(ctx context.Context, queueID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE queue SET attempted_at=NULL, attempted_by=NULL WHERE id=?`, queueID)
	return err
}

func (s *SQLiteStore) RescheduleAndReleaseQueueItem(ctx context.Context, queueID int64, delay time.Duration) error {
	sched := time.Now().Add(delay)
	_, err := s.db.ExecContext(ctx, `UPDATE queue SET scheduled_at=?, attempted_at=NULL, attempted_by=NULL WHERE id=?`, sched, queueID)
	return err
}

// Events
func (s *SQLiteStore) LogEvent(ctx context.Context, instanceID int64, stepID *int64, eventType string, payload any) error {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO workflow_events (instance_id, step_id, event_type, payload, created_at) VALUES(?, ?, ?, ?, ?)`, instanceID, stepID, eventType, payloadJSON, time.Now())
	return err
}

func (s *SQLiteStore) GetWorkflowEvents(ctx context.Context, instanceID int64) ([]WorkflowEvent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, instance_id, step_id, event_type, payload, created_at FROM workflow_events WHERE instance_id=? ORDER BY id`, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []WorkflowEvent
	for rows.Next() {
		var ev WorkflowEvent
		if err := rows.Scan(&ev.ID, &ev.InstanceID, &ev.StepID, &ev.EventType, &ev.Payload, &ev.CreatedAt); err != nil {
			return nil, err
		}
		res = append(res, ev)
	}
	return res, nil
}

// Joins and other auxiliary features — minimal or not implemented for now
func (s *SQLiteStore) CreateJoinState(ctx context.Context, instanceID int64, joinStepName string, waitingFor []string, strategy JoinStrategy) error {
	wf, _ := json.Marshal(waitingFor)
	now := time.Now()
	_, err := s.db.ExecContext(ctx, `INSERT INTO join_states (instance_id, join_step_name, waiting_for, completed, failed, join_strategy, is_ready, created_at, updated_at) VALUES(?, ?, ?, '[]', '[]', ?, 0, ?, ?)`, instanceID, joinStepName, string(wf), strategy, now, now)
	return err
}
func (s *SQLiteStore) UpdateJoinState(ctx context.Context, instanceID int64, joinStepName, completedStep string, success bool) (bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT waiting_for, completed, failed, join_strategy FROM join_states WHERE instance_id=? AND join_step_name=?`, instanceID, joinStepName)
	var waitingJSON, completedJSON, failedJSON string
	var strategy JoinStrategy
	if err := row.Scan(&waitingJSON, &completedJSON, &failedJSON, &strategy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// create default with this single step
			_ = s.CreateJoinState(ctx, instanceID, joinStepName, []string{completedStep}, JoinStrategyAll)
			waitingJSON = "[\"" + completedStep + "\"]"
			completedJSON = "[]"
			failedJSON = "[]"
			strategy = JoinStrategyAll
		} else {
			return false, err
		}
	}
	var waitingFor, completed, failed []string
	_ = json.Unmarshal([]byte(waitingJSON), &waitingFor)
	_ = json.Unmarshal([]byte(completedJSON), &completed)
	_ = json.Unmarshal([]byte(failedJSON), &failed)
	// append to completed or failed if not present
	target := &completed
	if !success {
		target = &failed
	}
	found := false
	for _, v := range *target {
		if v == completedStep {
			found = true
			break
		}
	}
	if !found {
		*target = append(*target, completedStep)
	}
	// recompute readiness
	isReady := false
	if strategy == JoinStrategyAny {
		isReady = len(completed) > 0 || len(failed) > 0
	} else {
		totalProcessed := len(completed) + len(failed)
		isReady = totalProcessed >= len(waitingFor)
	}
	compJSON, _ := json.Marshal(completed)
	failJSON, _ := json.Marshal(failed)
	_, err := s.db.ExecContext(ctx, `UPDATE join_states SET completed=?, failed=?, is_ready=?, updated_at=? WHERE instance_id=? AND join_step_name=?`, string(compJSON), string(failJSON), boolToInt(isReady), time.Now(), instanceID, joinStepName)
	return isReady, err
}
func (s *SQLiteStore) GetJoinState(ctx context.Context, instanceID int64, joinStepName string) (*JoinState, error) {
	row := s.db.QueryRowContext(ctx, `SELECT waiting_for, completed, failed, join_strategy, is_ready, created_at, updated_at FROM join_states WHERE instance_id=? AND join_step_name=?`, instanceID, joinStepName)
	var waitingJSON, completedJSON, failedJSON string
	var strategy JoinStrategy
	var isReadyInt int
	var createdAt, updatedAt time.Time
	if err := row.Scan(&waitingJSON, &completedJSON, &failedJSON, &strategy, &isReadyInt, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrEntityNotFound
		}
		return nil, err
	}
	var waitingFor, completed, failed []string
	_ = json.Unmarshal([]byte(waitingJSON), &waitingFor)
	_ = json.Unmarshal([]byte(completedJSON), &completed)
	_ = json.Unmarshal([]byte(failedJSON), &failed)
	return &JoinState{InstanceID: instanceID, JoinStepName: joinStepName, WaitingFor: waitingFor, Completed: completed, Failed: failed, JoinStrategy: strategy, IsReady: isReadyInt == 1, CreatedAt: createdAt, UpdatedAt: updatedAt}, nil
}
func (s *SQLiteStore) AddToJoinWaitFor(ctx context.Context, instanceID int64, joinStepName, stepToAdd string) error {
	row := s.db.QueryRowContext(ctx, `SELECT waiting_for, completed, failed, join_strategy FROM join_states WHERE instance_id=? AND join_step_name=?`, instanceID, joinStepName)
	var waitingJSON, completedJSON, failedJSON string
	var strategy JoinStrategy
	if err := row.Scan(&waitingJSON, &completedJSON, &failedJSON, &strategy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return s.CreateJoinState(ctx, instanceID, joinStepName, []string{stepToAdd}, strategy)
		}
		return err
	}
	var waitingFor, completed, failed []string
	_ = json.Unmarshal([]byte(waitingJSON), &waitingFor)
	_ = json.Unmarshal([]byte(completedJSON), &completed)
	_ = json.Unmarshal([]byte(failedJSON), &failed)
	waitingFor = append(waitingFor, stepToAdd)
	isReady := checkJoinReady(waitingFor, completed, failed, strategy)
	wfJSON, _ := json.Marshal(waitingFor)
	_, err := s.db.ExecContext(ctx, `UPDATE join_states SET waiting_for=?, is_ready=?, updated_at=? WHERE instance_id=? AND join_step_name=?`, string(wfJSON), boolToInt(isReady), time.Now(), instanceID, joinStepName)
	return err
}
func (s *SQLiteStore) ReplaceInJoinWaitFor(ctx context.Context, instanceID int64, joinStepName, virtualStep, realStep string) error {
	row := s.db.QueryRowContext(ctx, `SELECT waiting_for, completed, failed, join_strategy FROM join_states WHERE instance_id=? AND join_step_name=?`, instanceID, joinStepName)
	var waitingJSON, completedJSON, failedJSON string
	var strategy JoinStrategy
	if err := row.Scan(&waitingJSON, &completedJSON, &failedJSON, &strategy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return s.CreateJoinState(ctx, instanceID, joinStepName, []string{realStep}, JoinStrategyAll)
		}
		return err
	}
	var waitingFor, completed, failed []string
	_ = json.Unmarshal([]byte(waitingJSON), &waitingFor)
	_ = json.Unmarshal([]byte(completedJSON), &completed)
	_ = json.Unmarshal([]byte(failedJSON), &failed)
	found := false
	for i, w := range waitingFor {
		if w == virtualStep {
			waitingFor[i] = realStep
			found = true
			break
		}
	}
	if !found {
		waitingFor = append(waitingFor, realStep)
	}
	isReady := checkJoinReady(waitingFor, completed, failed, strategy)
	wfJSON, _ := json.Marshal(waitingFor)
	_, err := s.db.ExecContext(ctx, `UPDATE join_states SET waiting_for=?, is_ready=?, updated_at=? WHERE instance_id=? AND join_step_name=?`, string(wfJSON), boolToInt(isReady), time.Now(), instanceID, joinStepName)
	return err
}

func (s *SQLiteStore) GetSummaryStats(ctx context.Context) (*SummaryStats, error) {
	row := s.db.QueryRowContext(ctx, `SELECT 
		COUNT(*) as total,
		SUM(CASE WHEN status='completed' THEN 1 ELSE 0 END),
		SUM(CASE WHEN status='failed' THEN 1 ELSE 0 END),
		SUM(CASE WHEN status='running' THEN 1 ELSE 0 END),
		SUM(CASE WHEN status='pending' THEN 1 ELSE 0 END)
		FROM workflow_instances`)
	var stats SummaryStats
	if err := row.Scan(&stats.TotalWorkflows, &stats.CompletedWorkflows, &stats.FailedWorkflows, &stats.RunningWorkflows, &stats.PendingWorkflows); err != nil {
		return nil, err
	}
	// active = running+pending in this simplified model
	stats.ActiveWorkflows = uint(stats.RunningWorkflows + stats.PendingWorkflows)
	return &stats, nil
}
func (s *SQLiteStore) GetActiveInstances(ctx context.Context) ([]ActiveWorkflowInstance, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT wi.id, wi.workflow_id, COALESCE(wd.name,''), wi.status, wi.created_at, wi.updated_at,
		(SELECT step_name FROM workflow_steps WHERE instance_id=wi.id AND status='running' LIMIT 1) as current_step,
		(SELECT COUNT(*) FROM workflow_steps WHERE instance_id=wi.id) as total_steps,
		(SELECT COUNT(*) FROM workflow_steps WHERE instance_id=wi.id AND status='completed') as completed_steps,
		(SELECT COUNT(*) FROM workflow_steps WHERE instance_id=wi.id AND status='rolled_back') as rolled_back_steps
		FROM workflow_instances wi LEFT JOIN workflow_definitions wd ON wi.workflow_id = wd.id WHERE wi.status IN ('running','pending','dlq') ORDER BY wi.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []ActiveWorkflowInstance
	for rows.Next() {
		var a ActiveWorkflowInstance
		var currentStep *string
		if err := rows.Scan(&a.ID, &a.WorkflowID, &a.WorkflowName, &a.Status, &a.StartedAt, &a.UpdatedAt, &currentStep, &a.TotalSteps, &a.CompletedSteps, &a.RolledBackSteps); err != nil {
			return nil, err
		}
		if currentStep != nil {
			a.CurrentStep = *currentStep
		}
		res = append(res, a)
	}
	return res, nil
}
func (s *SQLiteStore) GetWorkflowDefinitions(ctx context.Context) ([]WorkflowDefinition, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, version, definition, created_at FROM workflow_definitions ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []WorkflowDefinition
	for rows.Next() {
		var d WorkflowDefinition
		var defJSON []byte
		if err := rows.Scan(&d.ID, &d.Name, &d.Version, &defJSON, &d.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(defJSON, &d.Definition)
		res = append(res, d)
	}
	return res, nil
}
func (s *SQLiteStore) GetWorkflowInstances(ctx context.Context, workflowID string) ([]WorkflowInstance, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, workflow_id, status, input, output, error, started_at, completed_at, created_at, updated_at FROM workflow_instances WHERE workflow_id=? ORDER BY id`, workflowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []WorkflowInstance
	for rows.Next() {
		var inst WorkflowInstance
		var inb, outb []byte
		if err := rows.Scan(&inst.ID, &inst.WorkflowID, &inst.Status, &inb, &outb, &inst.Error, &inst.StartedAt, &inst.CompletedAt, &inst.CreatedAt, &inst.UpdatedAt); err != nil {
			return nil, err
		}
		inst.Input = json.RawMessage(inb)
		if outb != nil {
			inst.Output = json.RawMessage(outb)
		}
		res = append(res, inst)
	}
	return res, nil
}
func (s *SQLiteStore) GetAllWorkflowInstances(ctx context.Context) ([]WorkflowInstance, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, workflow_id, status, input, output, error, started_at, completed_at, created_at, updated_at FROM workflow_instances ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []WorkflowInstance
	for rows.Next() {
		var inst WorkflowInstance
		var inb, outb []byte
		if err := rows.Scan(&inst.ID, &inst.WorkflowID, &inst.Status, &inb, &outb, &inst.Error, &inst.StartedAt, &inst.CompletedAt, &inst.CreatedAt, &inst.UpdatedAt); err != nil {
			return nil, err
		}
		inst.Input = json.RawMessage(inb)
		if outb != nil {
			inst.Output = json.RawMessage(outb)
		}
		res = append(res, inst)
	}
	return res, nil
}
func (s *SQLiteStore) GetWorkflowSteps(ctx context.Context, instanceID int64) ([]WorkflowStep, error) {
	return s.GetStepsByInstance(ctx, instanceID)
}
func (s *SQLiteStore) GetActiveStepsForUpdate(ctx context.Context, instanceID int64) ([]WorkflowStep, error) {
	return s.GetStepsByInstance(ctx, instanceID)
}

func (s *SQLiteStore) CreateCancelRequest(ctx context.Context, req *WorkflowCancelRequest) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO cancel_requests (instance_id, requested_by, cancel_type, reason, created_at) VALUES(?, ?, ?, ?, ?)`, req.InstanceID, req.RequestedBy, req.CancelType, req.Reason, time.Now())
	return err
}
func (s *SQLiteStore) GetCancelRequest(ctx context.Context, instanceID int64) (*WorkflowCancelRequest, error) {
	row := s.db.QueryRowContext(ctx, `SELECT instance_id, requested_by, cancel_type, reason, created_at FROM cancel_requests WHERE instance_id=?`, instanceID)
	var req WorkflowCancelRequest
	if err := row.Scan(&req.InstanceID, &req.RequestedBy, &req.CancelType, &req.Reason, &req.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrEntityNotFound
		}
		return nil, err
	}
	return &req, nil
}
func (s *SQLiteStore) DeleteCancelRequest(ctx context.Context, instanceID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM cancel_requests WHERE instance_id=?`, instanceID)
	return err
}

func (s *SQLiteStore) CreateHumanDecision(ctx context.Context, decision *HumanDecisionRecord) error {
	now := time.Now()
	res, err := s.db.ExecContext(ctx, `INSERT INTO human_decisions (instance_id, step_id, decided_by, decision, comment, decided_at, created_at) VALUES(?, ?, ?, ?, ?, ?, ?)`, decision.InstanceID, decision.StepID, decision.DecidedBy, decision.Decision, decision.Comment, decision.DecidedAt, now)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	decision.ID = id
	decision.CreatedAt = now
	return nil
}
func (s *SQLiteStore) GetHumanDecision(ctx context.Context, stepID int64) (*HumanDecisionRecord, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, instance_id, step_id, decided_by, decision, comment, decided_at, created_at FROM human_decisions WHERE step_id=?`, stepID)
	var d HumanDecisionRecord
	if err := row.Scan(&d.ID, &d.InstanceID, &d.StepID, &d.DecidedBy, &d.Decision, &d.Comment, &d.DecidedAt, &d.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrEntityNotFound
		}
		return nil, err
	}
	return &d, nil
}
func (s *SQLiteStore) UpdateStepStatus(ctx context.Context, stepID int64, status StepStatus) error {
	_, err := s.db.ExecContext(ctx, `UPDATE workflow_steps SET status=? WHERE id=?`, status, stepID)
	return err
}
func (s *SQLiteStore) GetStepByID(ctx context.Context, stepID int64) (*WorkflowStep, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, instance_id, step_name, step_type, status, input, output, error, retry_count,
		max_retries, compensation_retry_count, idempotency_key, started_at, completed_at, created_at FROM workflow_steps WHERE id=?`, stepID)
	var step WorkflowStep
	var inputBytes, outputBytes []byte
	if err := row.Scan(&step.ID, &step.InstanceID, &step.StepName, &step.StepType, &step.Status, &inputBytes, &outputBytes, &step.Error,
		&step.RetryCount, &step.MaxRetries, &step.CompensationRetryCount, &step.IdempotencyKey, &step.StartedAt, &step.CompletedAt, &step.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrEntityNotFound
		}
		return nil, err
	}
	step.Input = json.RawMessage(inputBytes)
	if outputBytes != nil {
		step.Output = json.RawMessage(outputBytes)
	}
	return &step, nil
}
func (s *SQLiteStore) GetHumanDecisionStepByInstanceID(ctx context.Context, instanceID int64) (*WorkflowStep, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, instance_id, step_name, step_type, status, input, output, error, retry_count, max_retries, compensation_retry_count, idempotency_key, started_at, completed_at, created_at FROM workflow_steps WHERE instance_id=? AND step_type='human' ORDER BY created_at DESC LIMIT 1`, instanceID)
	var step WorkflowStep
	var inb, outb []byte
	if err := row.Scan(&step.ID, &step.InstanceID, &step.StepName, &step.StepType, &step.Status, &inb, &outb, &step.Error, &step.RetryCount, &step.MaxRetries, &step.CompensationRetryCount, &step.IdempotencyKey, &step.StartedAt, &step.CompletedAt, &step.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrEntityNotFound
		}
		return nil, err
	}
	step.Input = json.RawMessage(inb)
	if outb != nil {
		step.Output = json.RawMessage(outb)
	}
	return &step, nil
}

func (s *SQLiteStore) CreateDeadLetterRecord(ctx context.Context, rec *DeadLetterRecord) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO workflow_dlq (instance_id, workflow_id, step_id, step_name, step_type, input, error, reason, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`, rec.InstanceID, rec.WorkflowID, rec.StepID, rec.StepName, rec.StepType, rec.Input, rec.Error, rec.Reason, time.Now())
	return err
}
func (s *SQLiteStore) RequeueDeadLetter(ctx context.Context, dlqID int64, newInput *json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	var instanceID, stepID int64
	var input []byte
	if err := tx.QueryRowContext(ctx, `SELECT instance_id, step_id, input FROM workflow_dlq WHERE id=?`, dlqID).Scan(&instanceID, &stepID, &input); err != nil {
		return err
	}
	var setInput any
	if newInput != nil {
		setInput = *newInput
	} else {
		setInput = input
	}
	if _, err := tx.ExecContext(ctx, `UPDATE workflow_steps SET status='pending', input=?, error=NULL, retry_count=0, compensation_retry_count=0, started_at=NULL, completed_at=NULL WHERE id=?`, setInput, stepID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO queue (instance_id, step_id, scheduled_at, priority) VALUES(?, ?, ?, ?)`, instanceID, stepID, time.Now(), int(PriorityNormal)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE workflow_instances SET status='running', error=NULL WHERE id=? AND status IN ('failed','dlq')`, instanceID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM workflow_dlq WHERE id=?`, dlqID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	tx = nil
	return nil
}
func (s *SQLiteStore) ListDeadLetters(ctx context.Context, offset int, limit int) ([]DeadLetterRecord, int64, error) {
	row := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM workflow_dlq`)
	var total int64
	if err := row.Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, instance_id, workflow_id, step_id, step_name, step_type, input, error, reason, created_at FROM workflow_dlq ORDER BY created_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var res []DeadLetterRecord
	for rows.Next() {
		var r DeadLetterRecord
		if err := rows.Scan(&r.ID, &r.InstanceID, &r.WorkflowID, &r.StepID, &r.StepName, &r.StepType, &r.Input, &r.Error, &r.Reason, &r.CreatedAt); err != nil {
			return nil, 0, err
		}
		res = append(res, r)
	}
	return res, total, nil
}
func (s *SQLiteStore) GetDeadLetterByID(ctx context.Context, id int64) (*DeadLetterRecord, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, instance_id, workflow_id, step_id, step_name, step_type, input, error, reason, created_at FROM workflow_dlq WHERE id=?`, id)
	var r DeadLetterRecord
	if err := row.Scan(&r.ID, &r.InstanceID, &r.WorkflowID, &r.StepID, &r.StepName, &r.StepType, &r.Input, &r.Error, &r.Reason, &r.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrEntityNotFound
		}
		return nil, err
	}
	return &r, nil
}
func (s *SQLiteStore) PauseActiveStepsAndClearQueue(ctx context.Context, instanceID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE workflow_steps SET status='paused' WHERE instance_id=? AND status IN ('running','pending','compensation')`, instanceID)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM queue WHERE instance_id=?`, instanceID)
	return err
}

func (s *SQLiteStore) CleanupOldWorkflows(ctx context.Context, daysToKeep int) (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -daysToKeep)
	// delete related rows first
	_, _ = s.db.ExecContext(ctx, `DELETE FROM workflow_events WHERE instance_id IN (SELECT id FROM workflow_instances WHERE updated_at < ?)`, cutoff)
	_, _ = s.db.ExecContext(ctx, `DELETE FROM workflow_steps WHERE instance_id IN (SELECT id FROM workflow_instances WHERE updated_at < ?)`, cutoff)
	res, err := s.db.ExecContext(ctx, `DELETE FROM workflow_instances WHERE updated_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	rows, _ := res.RowsAffected()
	return rows, nil
}

// Minimal implementation to satisfy interface; not used in SQLite tests
func (s *SQLiteStore) GetWorkflowStats(ctx context.Context) ([]WorkflowStats, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name, version, (SELECT COUNT(*) FROM workflow_instances WHERE workflow_id=wd.id) as total,
		(SELECT COUNT(*) FROM workflow_instances WHERE workflow_id=wd.id AND status='completed') as completed,
		(SELECT COUNT(*) FROM workflow_instances WHERE workflow_id=wd.id AND status='failed') as failed,
		(SELECT COUNT(*) FROM workflow_instances WHERE workflow_id=wd.id AND status='running') as running
		FROM workflow_definitions wd`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var stats []WorkflowStats
	for rows.Next() {
		var sst WorkflowStats
		var total, completed, failed, running int
		if err := rows.Scan(&sst.WorkflowName, &sst.Version, &total, &completed, &failed, &running); err != nil {
			return nil, err
		}
		sst.TotalInstances = total
		sst.CompletedInstances = completed
		sst.FailedInstances = failed
		sst.RunningInstances = running
		// average duration not tracked; leave zero
		stats = append(stats, sst)
	}
	return stats, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
