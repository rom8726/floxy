package metrics

import (
	"context"
	"sync"
	"time"

	"github.com/rom8726/floxy"
)

var _ floxy.Plugin = (*MetricsPlugin)(nil)

type MetricsPlugin struct {
	floxy.BasePlugin

	collector          MetricsCollector
	workflowStartTimes map[int64]time.Time
	stepStartTimes     map[int64]time.Time
	mu                 sync.RWMutex
}

func New(collector MetricsCollector) *MetricsPlugin {
	return &MetricsPlugin{
		BasePlugin:         floxy.NewBasePlugin("metrics", floxy.PriorityHigh),
		collector:          collector,
		workflowStartTimes: make(map[int64]time.Time),
		stepStartTimes:     make(map[int64]time.Time),
	}
}

func (p *MetricsPlugin) OnWorkflowStart(ctx context.Context, instance *floxy.WorkflowInstance) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.workflowStartTimes[instance.ID] = time.Now()

	if p.collector != nil {
		p.collector.RecordWorkflowStarted(instance.ID, instance.WorkflowID)
		p.collector.RecordWorkflowStatus(instance.ID, instance.WorkflowID, instance.Status)
	}

	return nil
}

func (p *MetricsPlugin) OnWorkflowComplete(ctx context.Context, instance *floxy.WorkflowInstance) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	startTime, ok := p.workflowStartTimes[instance.ID]
	if !ok {
		return nil
	}

	duration := time.Since(startTime)
	delete(p.workflowStartTimes, instance.ID)

	if p.collector != nil {
		p.collector.RecordWorkflowCompleted(instance.ID, instance.WorkflowID, duration, instance.Status)
		p.collector.RecordWorkflowStatus(instance.ID, instance.WorkflowID, instance.Status)
	}

	return nil
}

func (p *MetricsPlugin) OnWorkflowFailed(ctx context.Context, instance *floxy.WorkflowInstance) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	startTime, ok := p.workflowStartTimes[instance.ID]
	if !ok {
		return nil
	}

	duration := time.Since(startTime)
	delete(p.workflowStartTimes, instance.ID)

	if p.collector != nil {
		p.collector.RecordWorkflowFailed(instance.ID, instance.WorkflowID, duration)
		p.collector.RecordWorkflowStatus(instance.ID, instance.WorkflowID, instance.Status)
	}

	return nil
}

func (p *MetricsPlugin) OnStepStart(
	ctx context.Context,
	instance *floxy.WorkflowInstance,
	step *floxy.WorkflowStep,
) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.stepStartTimes[step.ID] = time.Now()

	if p.collector != nil {
		p.collector.RecordStepStarted(step.InstanceID, instance.WorkflowID, step.StepName, step.StepType)
		p.collector.RecordStepStatus(step.InstanceID, instance.WorkflowID, step.StepName, step.Status)
	}

	return nil
}

func (p *MetricsPlugin) OnStepComplete(
	ctx context.Context,
	instance *floxy.WorkflowInstance,
	step *floxy.WorkflowStep,
) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	startTime, ok := p.stepStartTimes[step.ID]
	if !ok {
		return nil
	}

	duration := time.Since(startTime)
	delete(p.stepStartTimes, step.ID)

	if p.collector != nil {
		p.collector.RecordStepCompleted(step.InstanceID, instance.WorkflowID, step.StepName, step.StepType, duration)
		p.collector.RecordStepStatus(step.InstanceID, instance.WorkflowID, step.StepName, step.Status)
	}

	return nil
}

func (p *MetricsPlugin) OnStepFailed(ctx context.Context, instance *floxy.WorkflowInstance, step *floxy.WorkflowStep, err error) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	startTime, ok := p.stepStartTimes[step.ID]
	if !ok {
		return nil
	}

	duration := time.Since(startTime)
	delete(p.stepStartTimes, step.ID)

	if p.collector != nil {
		p.collector.RecordStepFailed(step.InstanceID, instance.WorkflowID, step.StepName, step.StepType, duration)
		p.collector.RecordStepStatus(step.InstanceID, instance.WorkflowID, step.StepName, step.Status)
	}

	return nil
}
