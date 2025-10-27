package metrics

import (
	"time"

	"github.com/rom8726/floxy"
)

type MetricsCollector interface {
	RecordWorkflowStarted(instanceID int64, workflowID string)
	RecordWorkflowCompleted(instanceID int64, workflowID string, duration time.Duration, status floxy.WorkflowStatus)
	RecordWorkflowFailed(instanceID int64, workflowID string, duration time.Duration)
	RecordStepStarted(instanceID int64, workflowID string, stepName string, stepType floxy.StepType)
	RecordStepCompleted(instanceID int64, workflowID string, stepName string, stepType floxy.StepType, duration time.Duration)
	RecordStepFailed(instanceID int64, workflowID string, stepName string, stepType floxy.StepType, duration time.Duration)
	RecordWorkflowStatus(instanceID int64, workflowID string, status floxy.WorkflowStatus)
	RecordStepStatus(instanceID int64, workflowID string, stepName string, status floxy.StepStatus)
}
