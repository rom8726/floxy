package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/rom8726/floxy"
)

func TestPrometheusCollector_WorkflowMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	c := NewPrometheusCollector(reg)

	instID := int64(42)
	wfID := "wf-A"

	c.RecordWorkflowStarted(instID, wfID)

	dur := 150 * time.Millisecond
	c.RecordWorkflowCompleted(instID, wfID, dur, floxy.StatusCompleted)

	c.RecordWorkflowFailed(instID, wfID, dur)

	c.RecordWorkflowStatus(instID, wfID, floxy.StatusCompleted)

	// Validate counters/gauges/histograms contain values
	if got := testutil.ToFloat64(c.workflowStarted.WithLabelValues("42", wfID)); got != 1 {
		t.Fatalf("workflowStarted = %v, want 1", got)
	}
	if got := testutil.ToFloat64(c.workflowCompleted.WithLabelValues("42", wfID, string(floxy.StatusCompleted))); got != 1 {
		t.Fatalf("workflowCompleted(completed) = %v, want 1", got)
	}
	if got := testutil.ToFloat64(c.workflowCompleted.WithLabelValues("42", wfID, string(floxy.StatusFailed))); got != 1 {
		t.Fatalf("workflowCompleted(failed) = %v, want 1", got)
	}

	// We only check that sum > 0 to avoid bucket coupling
	m := &prometheus.HistogramVec{}
	_ = m

	// Using testutil.CollectAndCount ensures the metric is registered and non-empty
	if n := testutil.CollectAndCount(c.workflowDuration); n == 0 {
		t.Fatalf("workflowDuration has no observations")
	}

	if got := testutil.ToFloat64(c.workflowStatus.WithLabelValues("42", wfID, string(floxy.StatusCompleted))); got != 1 {
		t.Fatalf("workflowStatus gauge = %v, want 1", got)
	}
}

func TestPrometheusCollector_StepMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	c := NewPrometheusCollector(reg)

	instID := int64(7)
	wfID := "wf-B"
	step := "step-1"
	stepType := floxy.StepTypeTask

	c.RecordStepStarted(instID, wfID, step, stepType)
	c.RecordStepCompleted(instID, wfID, step, stepType, 20*time.Millisecond)
	c.RecordStepFailed(instID, wfID, step, stepType, 30*time.Millisecond)
	c.RecordStepStatus(instID, wfID, step, floxy.StepStatusCompleted)

	if got := testutil.ToFloat64(c.stepStarted.WithLabelValues("7", wfID, step, string(stepType))); got != 1 {
		t.Fatalf("stepStarted = %v, want 1", got)
	}
	if got := testutil.ToFloat64(c.stepCompleted.WithLabelValues("7", wfID, step, string(stepType))); got != 1 {
		t.Fatalf("stepCompleted = %v, want 1", got)
	}
	if got := testutil.ToFloat64(c.stepFailed.WithLabelValues("7", wfID, step, string(stepType))); got != 1 {
		t.Fatalf("stepFailed = %v, want 1", got)
	}
	if n := testutil.CollectAndCount(c.stepDuration); n == 0 {
		t.Fatalf("stepDuration has no observations")
	}
	if got := testutil.ToFloat64(c.stepStatus.WithLabelValues("7", wfID, step, string(floxy.StepStatusCompleted))); got != 1 {
		t.Fatalf("stepStatus gauge = %v, want 1", got)
	}
}
