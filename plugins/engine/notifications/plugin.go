package notifications

import (
	"context"

	"github.com/rom8726/floxy"
)

var _ floxy.Plugin = (*NotificationsPlugin)(nil)

type NotificationType string

const (
	NotificationTypeWorkflowStarted   NotificationType = "workflow_started"
	NotificationTypeWorkflowCompleted NotificationType = "workflow_completed"
	NotificationTypeWorkflowFailed    NotificationType = "workflow_failed"
	NotificationTypeStepStarted       NotificationType = "step_started"
	NotificationTypeStepCompleted     NotificationType = "step_completed"
	NotificationTypeStepFailed        NotificationType = "step_failed"
)

type Notification struct {
	Type       NotificationType
	InstanceID int64
	WorkflowID string
	StepID     *int64
	StepName   string
	Status     string
	Error      string
}

type NotificationChannel interface {
	Send(ctx context.Context, notification Notification) error
}

type NotificationsPlugin struct {
	floxy.BasePlugin

	channel NotificationChannel
}

func New(channel NotificationChannel) *NotificationsPlugin {
	return &NotificationsPlugin{
		BasePlugin: floxy.NewBasePlugin("notifications", floxy.PriorityNormal),
		channel:    channel,
	}
}

func (p *NotificationsPlugin) OnWorkflowStart(ctx context.Context, instance *floxy.WorkflowInstance) error {
	if p.channel == nil {
		return nil
	}

	notification := Notification{
		Type:       NotificationTypeWorkflowStarted,
		InstanceID: instance.ID,
		WorkflowID: instance.WorkflowID,
		Status:     string(instance.Status),
	}

	return p.channel.Send(ctx, notification)
}

func (p *NotificationsPlugin) OnWorkflowComplete(ctx context.Context, instance *floxy.WorkflowInstance) error {
	if p.channel == nil {
		return nil
	}

	notification := Notification{
		Type:       NotificationTypeWorkflowCompleted,
		InstanceID: instance.ID,
		WorkflowID: instance.WorkflowID,
		Status:     string(instance.Status),
	}

	return p.channel.Send(ctx, notification)
}

func (p *NotificationsPlugin) OnWorkflowFailed(ctx context.Context, instance *floxy.WorkflowInstance) error {
	if p.channel == nil {
		return nil
	}

	var errorMsg string
	if instance.Error != nil {
		errorMsg = *instance.Error
	}

	notification := Notification{
		Type:       NotificationTypeWorkflowFailed,
		InstanceID: instance.ID,
		WorkflowID: instance.WorkflowID,
		Status:     string(instance.Status),
		Error:      errorMsg,
	}

	return p.channel.Send(ctx, notification)
}

func (p *NotificationsPlugin) OnStepStart(
	ctx context.Context,
	instance *floxy.WorkflowInstance,
	step *floxy.WorkflowStep,
) error {
	if p.channel == nil {
		return nil
	}

	notification := Notification{
		Type:       NotificationTypeStepStarted,
		InstanceID: step.InstanceID,
		WorkflowID: instance.WorkflowID,
		StepID:     &step.ID,
		StepName:   step.StepName,
		Status:     string(step.Status),
	}

	return p.channel.Send(ctx, notification)
}

func (p *NotificationsPlugin) OnStepComplete(
	ctx context.Context,
	instance *floxy.WorkflowInstance,
	step *floxy.WorkflowStep,
) error {
	if p.channel == nil {
		return nil
	}

	notification := Notification{
		Type:       NotificationTypeStepCompleted,
		InstanceID: step.InstanceID,
		WorkflowID: instance.WorkflowID,
		StepID:     &step.ID,
		StepName:   step.StepName,
		Status:     string(step.Status),
	}

	return p.channel.Send(ctx, notification)
}

func (p *NotificationsPlugin) OnStepFailed(
	ctx context.Context,
	instance *floxy.WorkflowInstance,
	step *floxy.WorkflowStep,
	err error,
) error {
	if p.channel == nil {
		return nil
	}

	var errorMsg string
	if step.Error != nil {
		errorMsg = *step.Error
	} else if err != nil {
		errorMsg = err.Error()
	}

	notification := Notification{
		Type:       NotificationTypeStepFailed,
		InstanceID: step.InstanceID,
		WorkflowID: instance.WorkflowID,
		StepID:     &step.ID,
		StepName:   step.StepName,
		Status:     string(step.Status),
		Error:      errorMsg,
	}

	return p.channel.Send(ctx, notification)
}
