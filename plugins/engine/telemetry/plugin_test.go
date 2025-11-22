package telemetry

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/rom8726/floxy"
)

func TestTelemetryPlugin_New(t *testing.T) {
	tracer := otel.Tracer("test")
	plugin := New(tracer)

	if plugin == nil {
		t.Fatal("New() returned nil")
	}
	if plugin.Name() != "telemetry" {
		t.Errorf("Name() = %q, want %q", plugin.Name(), "telemetry")
	}
	if plugin.Priority() != floxy.PriorityHigh {
		t.Errorf("Priority() = %v, want %v", plugin.Priority(), floxy.PriorityHigh)
	}
	if plugin.defaultTTL != 1*time.Hour {
		t.Errorf("defaultTTL = %v, want %v", plugin.defaultTTL, 1*time.Hour)
	}
	if plugin.humanStepTTL != 24*time.Hour {
		t.Errorf("humanStepTTL = %v, want %v", plugin.humanStepTTL, 24*time.Hour)
	}
}

func TestTelemetryPlugin_NewWithOptions(t *testing.T) {
	tracer := otel.Tracer("test")
	customDefaultTTL := 2 * time.Hour
	customHumanTTL := 48 * time.Hour

	plugin := New(tracer, WithDefaultTTL(customDefaultTTL), WithHumanStepTTL(customHumanTTL))

	if plugin.defaultTTL != customDefaultTTL {
		t.Errorf("defaultTTL = %v, want %v", plugin.defaultTTL, customDefaultTTL)
	}
	if plugin.humanStepTTL != customHumanTTL {
		t.Errorf("humanStepTTL = %v, want %v", plugin.humanStepTTL, customHumanTTL)
	}
}

func TestTelemetryPlugin_NewWithNilTracer(t *testing.T) {
	plugin := New(nil)

	if plugin == nil {
		t.Fatal("New() returned nil")
	}
	if plugin.tracer == nil {
		t.Fatal("tracer should not be nil when nil is passed")
	}
}

func TestTelemetryPlugin_WorkflowLifecycle(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(
		trace.WithBatcher(exporter),
	)
	tracer := tp.Tracer("test")

	plugin := New(tracer)

	inst := &floxy.WorkflowInstance{
		ID:         1001,
		WorkflowID: "test-workflow",
		Status:     floxy.StatusRunning,
		CreatedAt:  time.Now(),
	}

	// Start workflow
	if err := plugin.OnWorkflowStart(context.Background(), inst); err != nil {
		t.Fatalf("OnWorkflowStart() error = %v", err)
	}

	// Check that span and context are stored
	plugin.mu.RLock()
	spanKey := "workflow:1001"
	if _, ok := plugin.spans[spanKey]; !ok {
		t.Error("workflow span not stored")
	}
	if _, ok := plugin.workflowCtxs[inst.ID]; !ok {
		t.Error("workflow context not stored")
	}
	plugin.mu.RUnlock()

	// Complete workflow
	inst.Status = floxy.StatusCompleted
	if err := plugin.OnWorkflowComplete(context.Background(), inst); err != nil {
		t.Fatalf("OnWorkflowComplete() error = %v", err)
	}

	// Check that span and context are removed
	plugin.mu.RLock()
	if _, ok := plugin.spans[spanKey]; ok {
		t.Error("workflow span should be removed after completion")
	}
	if _, ok := plugin.workflowCtxs[inst.ID]; ok {
		t.Error("workflow context should be removed after completion")
	}
	plugin.mu.RUnlock()
}

func TestTelemetryPlugin_WorkflowFailed(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(
		trace.WithBatcher(exporter),
	)
	tracer := tp.Tracer("test")

	plugin := New(tracer)

	inst := &floxy.WorkflowInstance{
		ID:         2002,
		WorkflowID: "failed-workflow",
		Status:     floxy.StatusFailed,
		CreatedAt:  time.Now(),
	}
	errorMsg := "test error"
	inst.Error = &errorMsg

	// Start workflow
	if err := plugin.OnWorkflowStart(context.Background(), inst); err != nil {
		t.Fatalf("OnWorkflowStart() error = %v", err)
	}

	// Fail workflow
	if err := plugin.OnWorkflowFailed(context.Background(), inst); err != nil {
		t.Fatalf("OnWorkflowFailed() error = %v", err)
	}

	// Check that span and context are removed
	plugin.mu.RLock()
	spanKey := "workflow:2002"
	if _, ok := plugin.spans[spanKey]; ok {
		t.Error("workflow span should be removed after failure")
	}
	if _, ok := plugin.workflowCtxs[inst.ID]; ok {
		t.Error("workflow context should be removed after failure")
	}
	plugin.mu.RUnlock()
}

func TestTelemetryPlugin_StepLifecycle(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(
		trace.WithBatcher(exporter),
	)
	tracer := tp.Tracer("test")

	plugin := New(tracer)

	inst := &floxy.WorkflowInstance{
		ID:         3003,
		WorkflowID: "test-workflow",
		Status:     floxy.StatusRunning,
		CreatedAt:  time.Now(),
	}

	step := &floxy.WorkflowStep{
		ID:                     11,
		InstanceID:             3003,
		StepName:               "test-step",
		StepType:               floxy.StepTypeTask,
		Status:                 floxy.StepStatusRunning,
		RetryCount:             0,
		CompensationRetryCount: 0,
		IdempotencyKey:         "key-123",
		CreatedAt:              time.Now(),
	}

	// Start workflow first
	if err := plugin.OnWorkflowStart(context.Background(), inst); err != nil {
		t.Fatalf("OnWorkflowStart() error = %v", err)
	}

	// Start step
	if err := plugin.OnStepStart(context.Background(), inst, step); err != nil {
		t.Fatalf("OnStepStart() error = %v", err)
	}

	// Check that step span is stored
	plugin.mu.RLock()
	spanKey := "step:11"
	entry, ok := plugin.spans[spanKey]
	if !ok {
		t.Error("step span not stored")
	} else if entry.stepType != floxy.StepTypeTask {
		t.Errorf("stepType = %v, want %v", entry.stepType, floxy.StepTypeTask)
	}
	plugin.mu.RUnlock()

	// Complete step
	step.Status = floxy.StepStatusCompleted
	if err := plugin.OnStepComplete(context.Background(), inst, step); err != nil {
		t.Fatalf("OnStepComplete() error = %v", err)
	}

	// Check that step span is removed
	plugin.mu.RLock()
	if _, ok := plugin.spans[spanKey]; ok {
		t.Error("step span should be removed after completion")
	}
	plugin.mu.RUnlock()
}

func TestTelemetryPlugin_StepFailed(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(
		trace.WithBatcher(exporter),
	)
	tracer := tp.Tracer("test")

	plugin := New(tracer)

	inst := &floxy.WorkflowInstance{
		ID:         4004,
		WorkflowID: "test-workflow",
		Status:     floxy.StatusRunning,
		CreatedAt:  time.Now(),
	}

	step := &floxy.WorkflowStep{
		ID:         22,
		InstanceID: 4004,
		StepName:   "failed-step",
		StepType:   floxy.StepTypeTask,
		Status:     floxy.StepStatusFailed,
		CreatedAt:  time.Now(),
	}
	errorMsg := "step error"
	step.Error = &errorMsg

	// Start workflow first
	if err := plugin.OnWorkflowStart(context.Background(), inst); err != nil {
		t.Fatalf("OnWorkflowStart() error = %v", err)
	}

	// Start step
	if err := plugin.OnStepStart(context.Background(), inst, step); err != nil {
		t.Fatalf("OnStepStart() error = %v", err)
	}

	// Fail step
	testErr := &testError{msg: "test error"}
	if err := plugin.OnStepFailed(context.Background(), inst, step, testErr); err != nil {
		t.Fatalf("OnStepFailed() error = %v", err)
	}

	// Check that step span is removed
	plugin.mu.RLock()
	spanKey := "step:22"
	if _, ok := plugin.spans[spanKey]; ok {
		t.Error("step span should be removed after failure")
	}
	plugin.mu.RUnlock()
}

func TestTelemetryPlugin_StepWithoutWorkflow(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(
		trace.WithBatcher(exporter),
	)
	tracer := tp.Tracer("test")

	plugin := New(tracer)

	inst := &floxy.WorkflowInstance{
		ID:         5005,
		WorkflowID: "test-workflow",
		Status:     floxy.StatusRunning,
		CreatedAt:  time.Now(),
	}

	step := &floxy.WorkflowStep{
		ID:         33,
		InstanceID: 5005,
		StepName:   "orphan-step",
		StepType:   floxy.StepTypeTask,
		Status:     floxy.StepStatusRunning,
		CreatedAt:  time.Now(),
	}

	// Start step without workflow start (should still work)
	if err := plugin.OnStepStart(context.Background(), inst, step); err != nil {
		t.Fatalf("OnStepStart() error = %v", err)
	}

	// Check that step span is stored
	plugin.mu.RLock()
	spanKey := "step:33"
	if _, ok := plugin.spans[spanKey]; !ok {
		t.Error("step span not stored")
	}
	plugin.mu.RUnlock()
}

func TestTelemetryPlugin_OnRollbackStepChain(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(
		trace.WithBatcher(exporter),
	)
	tracer := tp.Tracer("test")

	plugin := New(tracer)

	inst := &floxy.WorkflowInstance{
		ID:         6006,
		WorkflowID: "test-workflow",
		Status:     floxy.StatusRunning,
		CreatedAt:  time.Now(),
	}

	// Start workflow first
	if err := plugin.OnWorkflowStart(context.Background(), inst); err != nil {
		t.Fatalf("OnWorkflowStart() error = %v", err)
	}

	// Rollback step chain
	if err := plugin.OnRollbackStepChain(context.Background(), inst.ID, "rollback-step", 2); err != nil {
		t.Fatalf("OnRollbackStepChain() error = %v", err)
	}

	// Rollback spans are not stored, so nothing to check
}

func TestTelemetryPlugin_TTLCleanup(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(
		trace.WithBatcher(exporter),
	)
	tracer := tp.Tracer("test")

	// Use very short TTL for testing
	shortTTL := 10 * time.Millisecond
	plugin := New(tracer, WithDefaultTTL(shortTTL))

	inst := &floxy.WorkflowInstance{
		ID:         7007,
		WorkflowID: "test-workflow",
		Status:     floxy.StatusRunning,
		CreatedAt:  time.Now(),
	}

	step := &floxy.WorkflowStep{
		ID:         44,
		InstanceID: 7007,
		StepName:   "expired-step",
		StepType:   floxy.StepTypeTask,
		Status:     floxy.StepStatusRunning,
		CreatedAt:  time.Now(),
	}

	// Start workflow
	if err := plugin.OnWorkflowStart(context.Background(), inst); err != nil {
		t.Fatalf("OnWorkflowStart() error = %v", err)
	}

	// Start step
	if err := plugin.OnStepStart(context.Background(), inst, step); err != nil {
		t.Fatalf("OnStepStart() error = %v", err)
	}

	// Wait for TTL to expire
	time.Sleep(shortTTL + 10*time.Millisecond)

	// Add new element to trigger cleanup
	newInst := &floxy.WorkflowInstance{
		ID:         8008,
		WorkflowID: "new-workflow",
		Status:     floxy.StatusRunning,
		CreatedAt:  time.Now(),
	}
	if err := plugin.OnWorkflowStart(context.Background(), newInst); err != nil {
		t.Fatalf("OnWorkflowStart() error = %v", err)
	}

	// Check that expired spans are cleaned up
	plugin.mu.RLock()
	if _, ok := plugin.spans["workflow:7007"]; ok {
		t.Error("expired workflow span should be cleaned up")
	}
	if _, ok := plugin.spans["step:44"]; ok {
		t.Error("expired step span should be cleaned up")
	}
	if _, ok := plugin.workflowCtxs[7007]; ok {
		t.Error("expired workflow context should be cleaned up")
	}
	plugin.mu.RUnlock()
}

func TestTelemetryPlugin_HumanStepTTL(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(
		trace.WithBatcher(exporter),
	)
	tracer := tp.Tracer("test")

	// Use very short default TTL but longer human TTL
	shortTTL := 10 * time.Millisecond
	longHumanTTL := 100 * time.Millisecond
	plugin := New(tracer, WithDefaultTTL(shortTTL), WithHumanStepTTL(longHumanTTL))

	inst := &floxy.WorkflowInstance{
		ID:         9009,
		WorkflowID: "test-workflow",
		Status:     floxy.StatusRunning,
		CreatedAt:  time.Now(),
	}

	humanStep := &floxy.WorkflowStep{
		ID:         55,
		InstanceID: 9009,
		StepName:   "human-step",
		StepType:   floxy.StepTypeHuman,
		Status:     floxy.StepStatusWaitingDecision,
		CreatedAt:  time.Now(),
	}

	regularStep := &floxy.WorkflowStep{
		ID:         56,
		InstanceID: 9009,
		StepName:   "regular-step",
		StepType:   floxy.StepTypeTask,
		Status:     floxy.StepStatusRunning,
		CreatedAt:  time.Now(),
	}

	// Start workflow
	if err := plugin.OnWorkflowStart(context.Background(), inst); err != nil {
		t.Fatalf("OnWorkflowStart() error = %v", err)
	}

	// Start human step
	if err := plugin.OnStepStart(context.Background(), inst, humanStep); err != nil {
		t.Fatalf("OnStepStart() error = %v", err)
	}

	// Start regular step
	if err := plugin.OnStepStart(context.Background(), inst, regularStep); err != nil {
		t.Fatalf("OnStepStart() error = %v", err)
	}

	// Wait for default TTL to expire (but not human TTL)
	time.Sleep(shortTTL + 10*time.Millisecond)

	// Trigger cleanup
	newInst := &floxy.WorkflowInstance{
		ID:         10010,
		WorkflowID: "new-workflow",
		Status:     floxy.StatusRunning,
		CreatedAt:  time.Now(),
	}
	if err := plugin.OnWorkflowStart(context.Background(), newInst); err != nil {
		t.Fatalf("OnWorkflowStart() error = %v", err)
	}

	// Check that regular step is cleaned up but human step is not
	plugin.mu.RLock()
	if _, ok := plugin.spans["step:56"]; ok {
		t.Error("regular step span should be cleaned up after default TTL")
	}
	if _, ok := plugin.spans["step:55"]; !ok {
		t.Error("human step span should not be cleaned up yet (longer TTL)")
	}
	plugin.mu.RUnlock()

	// Wait for human TTL to expire
	time.Sleep(longHumanTTL - shortTTL + 10*time.Millisecond)

	// Trigger cleanup again
	anotherInst := &floxy.WorkflowInstance{
		ID:         11011,
		WorkflowID: "another-workflow",
		Status:     floxy.StatusRunning,
		CreatedAt:  time.Now(),
	}
	if err := plugin.OnWorkflowStart(context.Background(), anotherInst); err != nil {
		t.Fatalf("OnWorkflowStart() error = %v", err)
	}

	// Check that human step is now cleaned up
	plugin.mu.RLock()
	if _, ok := plugin.spans["step:55"]; ok {
		t.Error("human step span should be cleaned up after human TTL")
	}
	plugin.mu.RUnlock()
}

func TestTelemetryPlugin_CompleteWithoutStart(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(
		trace.WithBatcher(exporter),
	)
	tracer := tp.Tracer("test")

	plugin := New(tracer)

	inst := &floxy.WorkflowInstance{
		ID:         12012,
		WorkflowID: "test-workflow",
		Status:     floxy.StatusCompleted,
		CreatedAt:  time.Now(),
	}

	step := &floxy.WorkflowStep{
		ID:         66,
		InstanceID: 12012,
		StepName:   "orphan-step",
		StepType:   floxy.StepTypeTask,
		Status:     floxy.StepStatusCompleted,
		CreatedAt:  time.Now(),
	}

	// Complete without start (should not panic)
	if err := plugin.OnWorkflowComplete(context.Background(), inst); err != nil {
		t.Fatalf("OnWorkflowComplete() error = %v", err)
	}

	if err := plugin.OnStepComplete(context.Background(), inst, step); err != nil {
		t.Fatalf("OnStepComplete() error = %v", err)
	}
}

type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}
