package floxy

import (
	"context"
	"encoding/json"
	"time"
)

type Store interface {
	SaveWorkflowDefinition(ctx context.Context, def *WorkflowDefinition) error
	GetWorkflowDefinition(ctx context.Context, id string) (*WorkflowDefinition, error)
	CreateInstance(
		ctx context.Context,
		workflowID string,
		input json.RawMessage,
	) (*WorkflowInstance, error)
	UpdateInstanceStatus(
		ctx context.Context,
		instanceID int64,
		status WorkflowStatus,
		output json.RawMessage,
		errMsg *string,
	) error
	GetInstance(ctx context.Context, instanceID int64) (*WorkflowInstance, error)
	CreateStep(ctx context.Context, step *WorkflowStep) error
	UpdateStep(
		ctx context.Context,
		stepID int64,
		status StepStatus,
		output json.RawMessage,
		errMsg *string,
	) error
	GetStepsByInstance(ctx context.Context, instanceID int64) ([]*WorkflowStep, error)
	EnqueueStep(
		ctx context.Context,
		instanceID int64,
		stepID *int64,
		priority int,
		delay time.Duration,
	) error
	UpdateStepCompensationRetry(
		ctx context.Context,
		stepID int64,
		retryCount int,
		status StepStatus,
	) error
	DequeueStep(ctx context.Context, workerID string) (*QueueItem, error)
	RemoveFromQueue(ctx context.Context, queueID int64) error
	LogEvent(
		ctx context.Context,
		instanceID int64,
		stepID *int64,
		eventType string,
		payload any,
	) error
	CreateJoinState(
		ctx context.Context,
		instanceID int64,
		joinStepName string,
		waitingFor []string,
		strategy JoinStrategy,
	) error
	UpdateJoinState(
		ctx context.Context,
		instanceID int64,
		joinStepName, completedStep string,
		success bool,
	) (bool, error)
	GetJoinState(ctx context.Context, instanceID int64, joinStepName string) (*JoinState, error)
}
