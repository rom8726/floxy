package metrics

import (
	"context"
	"testing"
	"time"

	"github.com/rom8726/floxy"
)

type fakeCollector struct {
	workflowStarted   int
	workflowCompleted int
	workflowFailed    int
	workflowStatus    int

	stepStarted   int
	stepCompleted int
	stepFailed    int
	stepStatus    int

	lastWorkflow struct {
		instanceID int64
		workflowID string
		status     floxy.WorkflowStatus
		duration   time.Duration
	}
	lastStep struct {
		instanceID int64
		workflowID string
		name       string
		typ        floxy.StepType
		status     floxy.StepStatus
		duration   time.Duration
	}
}

func (f *fakeCollector) RecordWorkflowStarted(instanceID int64, workflowID string) {
	f.workflowStarted++
	f.lastWorkflow.instanceID = instanceID
	f.lastWorkflow.workflowID = workflowID
}

func (f *fakeCollector) RecordWorkflowCompleted(instanceID int64, workflowID string, duration time.Duration, status floxy.WorkflowStatus) {
	f.workflowCompleted++
	f.lastWorkflow.instanceID = instanceID
	f.lastWorkflow.workflowID = workflowID
	f.lastWorkflow.duration = duration
	f.lastWorkflow.status = status
}

func (f *fakeCollector) RecordWorkflowFailed(instanceID int64, workflowID string, duration time.Duration) {
	f.workflowFailed++
	f.lastWorkflow.instanceID = instanceID
	f.lastWorkflow.workflowID = workflowID
	f.lastWorkflow.duration = duration
	f.lastWorkflow.status = floxy.StatusFailed
}

func (f *fakeCollector) RecordWorkflowStatus(instanceID int64, workflowID string, status floxy.WorkflowStatus) {
	f.workflowStatus++
	f.lastWorkflow.instanceID = instanceID
	f.lastWorkflow.workflowID = workflowID
	f.lastWorkflow.status = status
}

func (f *fakeCollector) RecordStepStarted(instanceID int64, workflowID string, stepName string, stepType floxy.StepType) {
	f.stepStarted++
	f.lastStep.instanceID = instanceID
	f.lastStep.workflowID = workflowID
	f.lastStep.name = stepName
	f.lastStep.typ = stepType
}

func (f *fakeCollector) RecordStepCompleted(instanceID int64, workflowID string, stepName string, stepType floxy.StepType, duration time.Duration) {
	f.stepCompleted++
	f.lastStep.instanceID = instanceID
	f.lastStep.workflowID = workflowID
	f.lastStep.name = stepName
	f.lastStep.typ = stepType
	f.lastStep.duration = duration
}

func (f *fakeCollector) RecordStepFailed(instanceID int64, workflowID string, stepName string, stepType floxy.StepType, duration time.Duration) {
	f.stepFailed++
	f.lastStep.instanceID = instanceID
	f.lastStep.workflowID = workflowID
	f.lastStep.name = stepName
	f.lastStep.typ = stepType
	f.lastStep.duration = duration
}

func (f *fakeCollector) RecordStepStatus(instanceID int64, workflowID string, stepName string, status floxy.StepStatus) {
	f.stepStatus++
	f.lastStep.instanceID = instanceID
	f.lastStep.workflowID = workflowID
	f.lastStep.name = stepName
	f.lastStep.status = status
}

func TestMetricsPlugin_WorkflowLifecycle(t *testing.T) {
	fc := &fakeCollector{}
	p := New(fc)

	inst := &floxy.WorkflowInstance{ID: 1001, WorkflowID: "wf-m", Status: floxy.StatusRunning}

	if err := p.OnWorkflowStart(context.Background(), inst); err != nil {
		t.Fatalf("OnWorkflowStart error: %v", err)
	}
	time.Sleep(10 * time.Millisecond)

	// complete with final status
	inst.Status = floxy.StatusCompleted
	if err := p.OnWorkflowComplete(context.Background(), inst); err != nil {
		t.Fatalf("OnWorkflowComplete error: %v", err)
	}

	if fc.workflowStarted != 1 || fc.workflowCompleted != 1 {
		t.Fatalf("workflow metrics counts: started=%d completed=%d", fc.workflowStarted, fc.workflowCompleted)
	}
	// status recorded on start and on complete
	if fc.workflowStatus != 2 {
		t.Fatalf("workflow status updates = %d, want 2", fc.workflowStatus)
	}
	if fc.lastWorkflow.instanceID != 1001 || fc.lastWorkflow.workflowID != "wf-m" {
		t.Fatalf("last workflow labels mismatch: %+v", fc.lastWorkflow)
	}
	if fc.lastWorkflow.status != floxy.StatusCompleted {
		t.Fatalf("last workflow status = %s, want completed", fc.lastWorkflow.status)
	}
	if fc.lastWorkflow.duration <= 0 {
		t.Fatalf("expected positive duration, got %v", fc.lastWorkflow.duration)
	}
}

func TestMetricsPlugin_WorkflowFailedWithoutStartIsNoop(t *testing.T) {
	fc := &fakeCollector{}
	p := New(fc)

	inst := &floxy.WorkflowInstance{ID: 2002, WorkflowID: "wf-x", Status: floxy.StatusFailed}

	// No start called
	if err := p.OnWorkflowFailed(context.Background(), inst); err != nil {
		t.Fatalf("OnWorkflowFailed error: %v", err)
	}

	if fc.workflowFailed != 0 && fc.workflowCompleted != 0 {
		t.Fatalf("expected no metrics recorded without start, got failed=%d completed=%d", fc.workflowFailed, fc.workflowCompleted)
	}
}

func TestMetricsPlugin_StepLifecycle(t *testing.T) {
	fc := &fakeCollector{}
	p := New(fc)

	inst := &floxy.WorkflowInstance{ID: 3003, WorkflowID: "wf-s", Status: floxy.StatusRunning}
	step := &floxy.WorkflowStep{ID: 11, InstanceID: 3003, StepName: "A", StepType: floxy.StepTypeTask, Status: floxy.StepStatusRunning}

	if err := p.OnStepStart(context.Background(), inst, step); err != nil {
		t.Fatalf("OnStepStart error: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	step.Status = floxy.StepStatusCompleted
	if err := p.OnStepComplete(context.Background(), inst, step); err != nil {
		t.Fatalf("OnStepComplete error: %v", err)
	}

	if fc.stepStarted != 1 || fc.stepCompleted != 1 {
		t.Fatalf("step metrics counts: started=%d completed=%d", fc.stepStarted, fc.stepCompleted)
	}
	// status recorded on start and on complete
	if fc.stepStatus != 2 {
		t.Fatalf("step status updates = %d, want 2", fc.stepStatus)
	}
	if fc.lastStep.duration <= 0 {
		t.Fatalf("expected positive step duration, got %v", fc.lastStep.duration)
	}
}

func TestMetricsPlugin_StepFailedWithoutStartIsNoop(t *testing.T) {
	fc := &fakeCollector{}
	p := New(fc)

	inst := &floxy.WorkflowInstance{ID: 4004, WorkflowID: "wf-s2", Status: floxy.StatusRunning}
	step := &floxy.WorkflowStep{ID: 22, InstanceID: 4004, StepName: "B", StepType: floxy.StepTypeTask, Status: floxy.StepStatusFailed}

	// No start called
	if err := p.OnStepFailed(context.Background(), inst, step, assertErr{}); err != nil {
		t.Fatalf("OnStepFailed error: %v", err)
	}

	if fc.stepFailed != 0 && fc.stepCompleted != 0 {
		t.Fatalf("expected no step metrics recorded without start, got failed=%d completed=%d", fc.stepFailed, fc.stepCompleted)
	}
}

type assertErr struct{}

func (assertErr) Error() string { return "x" }

func TestIntToString(t *testing.T) {
	if s := intToString(12345); s != "12345" {
		t.Fatalf("intToString = %q, want 12345", s)
	}
}
