package floxy

import (
	"encoding/json"
	"time"
)

type WorkflowStatus string

const (
	StatusPending   WorkflowStatus = "pending"
	StatusRunning   WorkflowStatus = "running"
	StatusCompleted WorkflowStatus = "completed"
	StatusFailed    WorkflowStatus = "failed"
	StatusCancelled WorkflowStatus = "cancelled"
)

type StepStatus string

const (
	StepStatusPending   StepStatus = "pending"
	StepStatusRunning   StepStatus = "running"
	StepStatusCompleted StepStatus = "completed"
	StepStatusFailed    StepStatus = "failed"
	StepStatusSkipped   StepStatus = "skipped"
)

type StepType string

const (
	StepTypeTask      StepType = "task"
	StepTypeParallel  StepType = "parallel"
	StepTypeCondition StepType = "condition"
	StepTypeFork      StepType = "fork"
	StepTypeJoin      StepType = "join"
)

type JoinStrategy string

const (
	JoinStrategyAll JoinStrategy = "all"
	JoinStrategyAny JoinStrategy = "any"
)

type WorkflowDefinition struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Version    int             `json:"version"`
	Definition GraphDefinition `json:"definition"`
	CreatedAt  time.Time       `json:"created_at"`
}

type GraphDefinition struct {
	Steps map[string]*StepDefinition `json:"steps"`
	Start string                     `json:"start"`
}

type StepDefinition struct {
	Name         string            `json:"name"`
	Type         StepType          `json:"type"`
	Handler      string            `json:"handler"`
	MaxRetries   int               `json:"max_retries"`
	Next         []string          `json:"next"`
	OnFailure    string            `json:"on_failure"`    // compensation step
	Condition    string            `json:"condition"`     // for conditional transitions
	Parallel     []string          `json:"parallel"`      // for parallel steps (fork)
	WaitFor      []string          `json:"wait_for"`      // for join, we are waiting for these steps to be completed
	JoinStrategy JoinStrategy      `json:"join_strategy"` // "all" (default) or "any"
	Metadata     map[string]string `json:"metadata"`
}

type WorkflowInstance struct {
	ID          int64           `json:"id"`
	WorkflowID  string          `json:"workflow_id"`
	Status      WorkflowStatus  `json:"status"`
	Input       json.RawMessage `json:"input"`
	Output      json.RawMessage `json:"output"`
	Error       *string         `json:"error"`
	StartedAt   *time.Time      `json:"started_at"`
	CompletedAt *time.Time      `json:"completed_at"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

type WorkflowStep struct {
	ID          int64           `json:"id"`
	InstanceID  int64           `json:"instance_id"`
	StepName    string          `json:"step_name"`
	StepType    StepType        `json:"step_type"`
	Status      StepStatus      `json:"status"`
	Input       json.RawMessage `json:"input"`
	Output      json.RawMessage `json:"output"`
	Error       *string         `json:"error"`
	RetryCount  int             `json:"retry_count"`
	MaxRetries  int             `json:"max_retries"`
	StartedAt   *time.Time      `json:"started_at"`
	CompletedAt *time.Time      `json:"completed_at"`
	CreatedAt   time.Time       `json:"created_at"`
}

type QueueItem struct {
	ID          int64      `json:"id"`
	InstanceID  int64      `json:"instance_id"`
	StepID      *int64     `json:"step_id"`
	ScheduledAt time.Time  `json:"scheduled_at"`
	AttemptedAt *time.Time `json:"attempted_at"`
	AttemptedBy *string    `json:"attempted_by"`
	Priority    int        `json:"priority"`
}

type WorkflowEvent struct {
	ID         int64           `json:"id"`
	InstanceID int64           `json:"instance_id"`
	StepID     *int64          `json:"step_id"`
	EventType  string          `json:"event_type"`
	Payload    json.RawMessage `json:"payload"`
	CreatedAt  time.Time       `json:"created_at"`
}

type JoinState struct {
	InstanceID   int64        `json:"instance_id"`
	JoinStepName string       `json:"join_step_name"`
	WaitingFor   []string     `json:"waiting_for"`
	Completed    []string     `json:"completed"`
	Failed       []string     `json:"failed"`
	JoinStrategy JoinStrategy `json:"join_strategy"`
	IsReady      bool         `json:"is_ready"`
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
}
