package floxy

const (
	// Event types
	EventWorkflowStarted   = "workflow_started"
	EventWorkflowCompleted = "workflow_completed"
	EventStepStarted       = "step_started"
	EventStepCompleted     = "step_completed"
	EventStepRetry         = "step_retry"
	EventStepFailed        = "step_failed"
	EventForkStarted       = "fork_started"
	EventJoinStateCreated  = "join_state_created"
	EventJoinCheck         = "join_check"
	EventJoinCompleted     = "join_completed"
	EventJoinUpdated       = "join_updated"
	EventJoinReady         = "join_ready"

	// Event data keys
	KeyWorkflowID    = "workflow_id"
	KeyStepName      = "step_name"
	KeyStepType      = "step_type"
	KeyParallelSteps = "parallel_steps"
	KeyJoinStep      = "join_step"
	KeyWaitingFor    = "waiting_for"
	KeyStrategy      = "strategy"
	KeyCompleted     = "completed"
	KeyFailed        = "failed"
	KeyIsReady       = "is_ready"
	KeyOutputs       = "outputs"
	KeyStatus        = "status"
	KeyRetryCount    = "retry_count"
	KeyError         = "error"
	KeyCompletedStep = "completed_step"
	KeySuccess       = "success"
)
