package telemetry

import (
	"context"
	"fmt"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/rom8726/floxy"
)

var _ floxy.Plugin = (*TelemetryPlugin)(nil)

type TelemetryPlugin struct {
	floxy.BasePlugin

	tracer trace.Tracer
	mu     sync.RWMutex
	spans  map[string]trace.Span
}

func New(tracer trace.Tracer) *TelemetryPlugin {
	if tracer == nil {
		tracer = otel.Tracer("floxy")
	}

	return &TelemetryPlugin{
		BasePlugin: floxy.NewBasePlugin("telemetry", floxy.PriorityHigh),
		tracer:     tracer,
		spans:      make(map[string]trace.Span),
	}
}

func (p *TelemetryPlugin) OnWorkflowStart(ctx context.Context, instance *floxy.WorkflowInstance) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	spanName := fmt.Sprintf("workflow.%s", instance.WorkflowID)
	ctx, span := p.tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindServer))

	span.SetAttributes(
		attribute.Int64("instance.id", instance.ID),
		attribute.String("instance.workflow_id", instance.WorkflowID),
		attribute.String("instance.status", string(instance.Status)),
	)

	spanKey := fmt.Sprintf("workflow:%d", instance.ID)
	p.spans[spanKey] = span

	return nil
}

func (p *TelemetryPlugin) OnWorkflowComplete(ctx context.Context, instance *floxy.WorkflowInstance) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	spanKey := fmt.Sprintf("workflow:%d", instance.ID)
	if span, ok := p.spans[spanKey]; ok {
		span.SetAttributes(
			attribute.String("instance.status", string(instance.Status)),
		)
		span.SetStatus(codes.Ok, "workflow completed")
		span.End()
		delete(p.spans, spanKey)
	}

	return nil
}

func (p *TelemetryPlugin) OnWorkflowFailed(ctx context.Context, instance *floxy.WorkflowInstance) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	spanKey := fmt.Sprintf("workflow:%d", instance.ID)
	if span, ok := p.spans[spanKey]; ok {
		span.SetAttributes(
			attribute.String("instance.status", string(instance.Status)),
		)
		if instance.Error != nil {
			span.SetAttributes(attribute.String("instance.error", *instance.Error))
		}
		span.SetStatus(codes.Error, "workflow failed")
		span.End()
		delete(p.spans, spanKey)
	}

	return nil
}

func (p *TelemetryPlugin) OnStepStart(
	ctx context.Context,
	instance *floxy.WorkflowInstance,
	step *floxy.WorkflowStep,
) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	spanName := fmt.Sprintf("step.%s", step.StepName)
	ctx, span := p.tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindInternal))

	attrs := []attribute.KeyValue{
		attribute.Int64("step.id", step.ID),
		attribute.Int64("step.instance_id", step.InstanceID),
		attribute.String("step.step_name", step.StepName),
		attribute.String("step.step_type", string(step.StepType)),
		attribute.String("step.status", string(step.Status)),
		attribute.Int("step.retry_count", step.RetryCount),
		attribute.Int("step.compensation_retry_count", step.CompensationRetryCount),
		attribute.String("step.idempotency_key", step.IdempotencyKey),
		attribute.Int64("instance.id", instance.ID),
		attribute.String("instance.workflow_id", instance.WorkflowID),
		attribute.String("instance.status", string(instance.Status)),
	}

	if step.Error != nil {
		attrs = append(attrs, attribute.String("step.error", *step.Error))
	}

	span.SetAttributes(attrs...)

	spanKey := fmt.Sprintf("step:%d", step.ID)
	p.spans[spanKey] = span

	return nil
}

func (p *TelemetryPlugin) OnStepComplete(
	ctx context.Context,
	instance *floxy.WorkflowInstance,
	step *floxy.WorkflowStep,
) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	spanKey := fmt.Sprintf("step:%d", step.ID)
	if span, ok := p.spans[spanKey]; ok {
		span.SetAttributes(
			attribute.String("step.status", string(step.Status)),
			attribute.Int("step.retry_count", step.RetryCount),
			attribute.Int("step.compensation_retry_count", step.CompensationRetryCount),
		)
		span.SetStatus(codes.Ok, "step completed")
		span.End()
		delete(p.spans, spanKey)
	}

	return nil
}

func (p *TelemetryPlugin) OnStepFailed(
	ctx context.Context,
	instance *floxy.WorkflowInstance,
	step *floxy.WorkflowStep,
	err error,
) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	spanKey := fmt.Sprintf("step:%d", step.ID)
	if span, ok := p.spans[spanKey]; ok {
		span.SetAttributes(
			attribute.String("step.status", string(step.Status)),
			attribute.Int("step.retry_count", step.RetryCount),
			attribute.Int("step.compensation_retry_count", step.CompensationRetryCount),
		)
		if step.Error != nil {
			span.SetAttributes(attribute.String("step.error", *step.Error))
		}
		if err != nil {
			span.RecordError(err)
		}
		span.SetStatus(codes.Error, "step failed")
		span.End()
		delete(p.spans, spanKey)
	}

	return nil
}

func (p *TelemetryPlugin) OnRollbackStepChain(
	ctx context.Context,
	instanceID int64,
	stepName string,
	depth int,
) error {
	spanName := fmt.Sprintf("rollback.%s", stepName)
	_, span := p.tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindInternal))

	span.SetAttributes(
		attribute.Int64("instance.id", instanceID),
		attribute.String("step.step_name", stepName),
		attribute.Int("rollback.depth", depth),
	)

	span.SetStatus(codes.Ok, "rollback completed")
	span.End()

	return nil
}
