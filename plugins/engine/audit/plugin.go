package audit

import (
	"context"
	"encoding/json"
	"time"

	"github.com/rom8726/floxy"
)

var _ floxy.Plugin = (*AuditPlugin)(nil)

type AuditLogEntry struct {
	Timestamp  time.Time       `json:"timestamp"`
	EventType  string          `json:"event_type"`
	InstanceID int64           `json:"instance_id"`
	WorkflowID string          `json:"workflow_id"`
	StepID     *int64          `json:"step_id,omitempty"`
	StepName   string          `json:"step_name,omitempty"`
	Status     string          `json:"status"`
	Error      string          `json:"error,omitempty"`
	Duration   *time.Duration  `json:"duration,omitempty"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
}

type Writer interface {
	Write(ctx context.Context, entry *AuditLogEntry) error
}

type AuditPlugin struct {
	floxy.BasePlugin

	writer Writer
}

func New(writer Writer) *AuditPlugin {
	return &AuditPlugin{
		BasePlugin: floxy.NewBasePlugin("audit", floxy.PriorityNormal),
		writer:     writer,
	}
}

func (p *AuditPlugin) OnWorkflowFailed(ctx context.Context, instance *floxy.WorkflowInstance) error {
	var errorMsg string
	if instance.Error != nil {
		errorMsg = *instance.Error
	}

	entry := AuditLogEntry{
		Timestamp:  time.Now(),
		EventType:  "workflow_failed",
		InstanceID: instance.ID,
		WorkflowID: instance.WorkflowID,
		Status:     string(instance.Status),
		Error:      errorMsg,
		Metadata:   instance.Output,
	}

	return p.logEvent(ctx, &entry)
}

func (p *AuditPlugin) OnWorkflowStart(ctx context.Context, instance *floxy.WorkflowInstance) error {
	entry := AuditLogEntry{
		Timestamp:  time.Now(),
		EventType:  "workflow_start",
		InstanceID: instance.ID,
		WorkflowID: instance.WorkflowID,
		Status:     string(instance.Status),
		Metadata:   instance.Input,
	}

	return p.logEvent(ctx, &entry)
}

func (p *AuditPlugin) OnWorkflowComplete(ctx context.Context, instance *floxy.WorkflowInstance) error {
	entry := AuditLogEntry{
		Timestamp:  time.Now(),
		EventType:  "workflow_complete",
		InstanceID: instance.ID,
		WorkflowID: instance.WorkflowID,
		Status:     string(instance.Status),
		Metadata:   instance.Output,
	}

	return p.logEvent(ctx, &entry)
}

func (p *AuditPlugin) OnStepStart(
	ctx context.Context,
	instance *floxy.WorkflowInstance,
	step *floxy.WorkflowStep,
) error {
	entry := AuditLogEntry{
		Timestamp:  time.Now(),
		EventType:  "step_start",
		InstanceID: step.InstanceID,
		WorkflowID: instance.WorkflowID,
		StepID:     &step.ID,
		StepName:   step.StepName,
		Status:     string(step.Status),
		Metadata:   step.Input,
	}

	return p.logEvent(ctx, &entry)
}

func (p *AuditPlugin) OnStepFailed(
	ctx context.Context,
	instance *floxy.WorkflowInstance,
	step *floxy.WorkflowStep,
	err error,
) error {
	var errorMsg string
	if step.Error != nil {
		errorMsg = *step.Error
	} else if err != nil {
		errorMsg = err.Error()
	}

	entry := AuditLogEntry{
		Timestamp:  time.Now(),
		EventType:  "step_failed",
		InstanceID: step.InstanceID,
		WorkflowID: instance.WorkflowID,
		StepID:     &step.ID,
		StepName:   step.StepName,
		Status:     string(step.Status),
		Error:      errorMsg,
		Metadata:   step.Input,
	}

	return p.logEvent(ctx, &entry)
}

func (p *AuditPlugin) OnStepComplete(ctx context.Context, instance *floxy.WorkflowInstance, step *floxy.WorkflowStep) error {
	entry := AuditLogEntry{
		Timestamp:  time.Now(),
		EventType:  "step_complete",
		InstanceID: step.InstanceID,
		WorkflowID: instance.WorkflowID,
		StepID:     &step.ID,
		StepName:   step.StepName,
		Status:     string(step.Status),
		Metadata:   step.Output,
	}

	return p.logEvent(ctx, &entry)
}

func (p *AuditPlugin) logEvent(ctx context.Context, entry *AuditLogEntry) error {
	return p.writer.Write(ctx, entry)
}
