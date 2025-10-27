package metrics

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/rom8726/floxy"
)

type PrometheusCollector struct {
	workflowStarted   *prometheus.CounterVec
	workflowCompleted *prometheus.CounterVec
	workflowDuration  *prometheus.HistogramVec
	workflowStatus    *prometheus.GaugeVec

	stepStarted   *prometheus.CounterVec
	stepCompleted *prometheus.CounterVec
	stepFailed    *prometheus.CounterVec
	stepDuration  *prometheus.HistogramVec
	stepStatus    *prometheus.GaugeVec
}

func NewPrometheusCollector(registry prometheus.Registerer) *PrometheusCollector {
	if registry == nil {
		registry = prometheus.DefaultRegisterer
	}

	return &PrometheusCollector{
		workflowStarted: promauto.With(registry).NewCounterVec(
			prometheus.CounterOpts{
				Name: "floxy_workflow_started_total",
				Help: "Total number of workflow instances started",
			},
			[]string{"instance_id", "workflow_id"},
		),
		workflowCompleted: promauto.With(registry).NewCounterVec(
			prometheus.CounterOpts{
				Name: "floxy_workflow_completed_total",
				Help: "Total number of completed workflow instances",
			},
			[]string{"instance_id", "workflow_id", "status"},
		),
		workflowDuration: promauto.With(registry).NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "floxy_workflow_duration_seconds",
				Help:    "Duration of workflow execution in seconds",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"instance_id", "workflow_id", "status"},
		),
		workflowStatus: promauto.With(registry).NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "floxy_workflow_current_status",
				Help: "Current status of workflow instances",
			},
			[]string{"instance_id", "workflow_id", "status"},
		),
		stepStarted: promauto.With(registry).NewCounterVec(
			prometheus.CounterOpts{
				Name: "floxy_step_started_total",
				Help: "Total number of step executions started",
			},
			[]string{"instance_id", "workflow_id", "step_name", "step_type"},
		),
		stepCompleted: promauto.With(registry).NewCounterVec(
			prometheus.CounterOpts{
				Name: "floxy_step_completed_total",
				Help: "Total number of completed step executions",
			},
			[]string{"instance_id", "workflow_id", "step_name", "step_type"},
		),
		stepFailed: promauto.With(registry).NewCounterVec(
			prometheus.CounterOpts{
				Name: "floxy_step_failed_total",
				Help: "Total number of failed step executions",
			},
			[]string{"instance_id", "workflow_id", "step_name", "step_type"},
		),
		stepDuration: promauto.With(registry).NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "floxy_step_duration_seconds",
				Help:    "Duration of step execution in seconds",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"instance_id", "workflow_id", "step_name", "step_type"},
		),
		stepStatus: promauto.With(registry).NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "floxy_step_current_status",
				Help: "Current status of step executions",
			},
			[]string{"instance_id", "workflow_id", "step_name", "status"},
		),
	}
}

func (c *PrometheusCollector) RecordWorkflowStarted(instanceID int64, workflowID string) {
	c.workflowStarted.WithLabelValues(intToString(instanceID), workflowID).Inc()
}

func (c *PrometheusCollector) RecordWorkflowCompleted(
	instanceID int64,
	workflowID string,
	duration time.Duration,
	status floxy.WorkflowStatus,
) {
	c.workflowCompleted.WithLabelValues(intToString(instanceID), workflowID, string(status)).Inc()
	c.workflowDuration.WithLabelValues(intToString(instanceID), workflowID, string(status)).Observe(duration.Seconds())
}

func (c *PrometheusCollector) RecordWorkflowFailed(instanceID int64, workflowID string, duration time.Duration) {
	c.workflowCompleted.WithLabelValues(intToString(instanceID), workflowID, string(floxy.StatusFailed)).Inc()
	c.workflowDuration.WithLabelValues(intToString(instanceID), workflowID, string(floxy.StatusFailed)).
		Observe(duration.Seconds())
}

func (c *PrometheusCollector) RecordStepStarted(instanceID int64, workflowID string, stepName string, stepType floxy.StepType) {
	c.stepStarted.WithLabelValues(intToString(instanceID), workflowID, stepName, string(stepType)).Inc()
}

func (c *PrometheusCollector) RecordStepCompleted(
	instanceID int64,
	workflowID string,
	stepName string,
	stepType floxy.StepType,
	duration time.Duration,
) {
	c.stepCompleted.WithLabelValues(intToString(instanceID), workflowID, stepName, string(stepType)).Inc()
	c.stepDuration.WithLabelValues(intToString(instanceID), workflowID, stepName, string(stepType)).Observe(duration.Seconds())
}

func (c *PrometheusCollector) RecordStepFailed(
	instanceID int64,
	workflowID string,
	stepName string,
	stepType floxy.StepType,
	duration time.Duration,
) {
	c.stepFailed.WithLabelValues(intToString(instanceID), workflowID, stepName, string(stepType)).Inc()
	c.stepDuration.WithLabelValues(intToString(instanceID), workflowID, stepName, string(stepType)).Observe(duration.Seconds())
}

func (c *PrometheusCollector) RecordWorkflowStatus(instanceID int64, workflowID string, status floxy.WorkflowStatus) {
	c.workflowStatus.WithLabelValues(intToString(instanceID), workflowID, string(status)).Set(1)
}

func (c *PrometheusCollector) RecordStepStatus(instanceID int64, workflowID string, stepName string, status floxy.StepStatus) {
	c.stepStatus.WithLabelValues(intToString(instanceID), workflowID, stepName, string(status)).Set(1)
}

func intToString(v int64) string {
	return strconv.FormatInt(v, 10)
}
