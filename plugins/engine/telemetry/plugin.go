package telemetry

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/rom8726/floxy"
)

var _ floxy.Plugin = (*TelemetryPlugin)(nil)

type spanEntry struct {
	span      trace.Span
	createdAt time.Time
	stepType  floxy.StepType
}

type workflowCtxEntry struct {
	ctx       context.Context
	createdAt time.Time
}

type TelemetryPlugin struct {
	floxy.BasePlugin

	tracer       trace.Tracer
	mu           sync.RWMutex
	spans        map[string]*spanEntry
	workflowCtxs map[int64]*workflowCtxEntry
	defaultTTL   time.Duration
	humanStepTTL time.Duration
}

type TelemetryOption func(*TelemetryPlugin)

func WithDefaultTTL(ttl time.Duration) TelemetryOption {
	return func(p *TelemetryPlugin) {
		p.defaultTTL = ttl
	}
}

func WithHumanStepTTL(ttl time.Duration) TelemetryOption {
	return func(p *TelemetryPlugin) {
		p.humanStepTTL = ttl
	}
}

func New(tracer trace.Tracer, opts ...TelemetryOption) *TelemetryPlugin {
	if tracer == nil {
		tracer = otel.Tracer("floxy")
	}

	plugin := &TelemetryPlugin{
		BasePlugin:   floxy.NewBasePlugin("telemetry", floxy.PriorityHigh),
		tracer:       tracer,
		spans:        make(map[string]*spanEntry),
		workflowCtxs: make(map[int64]*workflowCtxEntry),
		defaultTTL:   1 * time.Hour,
		humanStepTTL: 24 * time.Hour,
	}

	for _, opt := range opts {
		opt(plugin)
	}

	return plugin
}

func (p *TelemetryPlugin) OnWorkflowStart(ctx context.Context, instance *floxy.WorkflowInstance) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	spanName := fmt.Sprintf("workflow.%s", instance.WorkflowID)
	workflowCtx, span := p.tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindServer))

	span.SetAttributes(
		attribute.Int64("instance.id", instance.ID),
		attribute.String("instance.workflow_id", instance.WorkflowID),
		attribute.String("instance.status", string(instance.Status)),
	)

	spanKey := fmt.Sprintf("workflow:%d", instance.ID)
	p.spans[spanKey] = &spanEntry{
		span:      span,
		createdAt: time.Now(),
	}
	p.workflowCtxs[instance.ID] = &workflowCtxEntry{
		ctx:       workflowCtx,
		createdAt: time.Now(),
	}

	p.cleanupExpired()

	return nil
}

func (p *TelemetryPlugin) OnWorkflowComplete(ctx context.Context, instance *floxy.WorkflowInstance) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	spanKey := fmt.Sprintf("workflow:%d", instance.ID)
	if entry, ok := p.spans[spanKey]; ok {
		entry.span.SetAttributes(
			attribute.String("instance.status", string(instance.Status)),
		)
		entry.span.SetStatus(codes.Ok, "workflow completed")
		entry.span.End()
		delete(p.spans, spanKey)
	}
	delete(p.workflowCtxs, instance.ID)

	return nil
}

func (p *TelemetryPlugin) OnWorkflowFailed(ctx context.Context, instance *floxy.WorkflowInstance) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	spanKey := fmt.Sprintf("workflow:%d", instance.ID)
	if entry, ok := p.spans[spanKey]; ok {
		entry.span.SetAttributes(
			attribute.String("instance.status", string(instance.Status)),
		)
		if instance.Error != nil {
			entry.span.SetAttributes(attribute.String("instance.error", *instance.Error))
		}
		entry.span.SetStatus(codes.Error, "workflow failed")
		entry.span.End()
		delete(p.spans, spanKey)
	}
	delete(p.workflowCtxs, instance.ID)

	return nil
}

func (p *TelemetryPlugin) OnStepStart(
	ctx context.Context,
	instance *floxy.WorkflowInstance,
	step *floxy.WorkflowStep,
) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Use workflow context if available to link spans
	stepCtx := ctx
	if entry, ok := p.workflowCtxs[instance.ID]; ok {
		stepCtx = entry.ctx
	}

	spanName := fmt.Sprintf("step.%s", step.StepName)
	_, span := p.tracer.Start(stepCtx, spanName, trace.WithSpanKind(trace.SpanKindInternal))

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
	p.spans[spanKey] = &spanEntry{
		span:      span,
		createdAt: time.Now(),
		stepType:  step.StepType,
	}

	p.cleanupExpired()

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
	if entry, ok := p.spans[spanKey]; ok {
		entry.span.SetAttributes(
			attribute.String("step.status", string(step.Status)),
			attribute.Int("step.retry_count", step.RetryCount),
			attribute.Int("step.compensation_retry_count", step.CompensationRetryCount),
		)
		entry.span.SetStatus(codes.Ok, "step completed")
		entry.span.End()
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
	if entry, ok := p.spans[spanKey]; ok {
		entry.span.SetAttributes(
			attribute.String("step.status", string(step.Status)),
			attribute.Int("step.retry_count", step.RetryCount),
			attribute.Int("step.compensation_retry_count", step.CompensationRetryCount),
		)
		if step.Error != nil {
			entry.span.SetAttributes(attribute.String("step.error", *step.Error))
		}
		if err != nil {
			entry.span.RecordError(err)
		}
		entry.span.SetStatus(codes.Error, "step failed")
		entry.span.End()
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
	p.mu.Lock()
	defer p.mu.Unlock()

	// Use workflow context if available to link spans
	rollbackCtx := ctx
	if entry, ok := p.workflowCtxs[instanceID]; ok {
		rollbackCtx = entry.ctx
	}

	spanName := fmt.Sprintf("rollback.%s", stepName)
	_, span := p.tracer.Start(rollbackCtx, spanName, trace.WithSpanKind(trace.SpanKindInternal))

	span.SetAttributes(
		attribute.Int64("instance.id", instanceID),
		attribute.String("step.step_name", stepName),
		attribute.Int("rollback.depth", depth),
	)

	span.SetStatus(codes.Ok, "rollback completed")
	span.End()

	return nil
}

func (p *TelemetryPlugin) cleanupExpired() {
	now := time.Now()

	// Cleanup expired spans
	for key, entry := range p.spans {
		ttl := p.defaultTTL
		if entry.stepType == floxy.StepTypeHuman {
			ttl = p.humanStepTTL
		}

		if now.Sub(entry.createdAt) > ttl {
			entry.span.SetStatus(codes.Error, "span expired due to TTL")
			entry.span.End()
			delete(p.spans, key)
		}
	}

	// Cleanup expired workflow contexts
	for instanceID, entry := range p.workflowCtxs {
		if now.Sub(entry.createdAt) > p.defaultTTL {
			delete(p.workflowCtxs, instanceID)
		}
	}
}
