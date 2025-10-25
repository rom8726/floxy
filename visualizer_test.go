package floxy

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestVisualizer_RenderGraph_WithHumanStep(t *testing.T) {
	visualizer := NewVisualizer()

	// Create a workflow definition with human step
	workflowDef := &WorkflowDefinition{
		Name:    "approval_workflow",
		Version: 1,
		Definition: GraphDefinition{
			Start: "start",
			Steps: map[string]*StepDefinition{
				"start": {
					Name:    "start",
					Type:    StepTypeTask,
					Handler: "start-handler",
				},
				"human_approval": {
					Name:    "human_approval",
					Type:    StepTypeHuman,
					Handler: "approval-handler",
				},
				"final": {
					Name:    "final",
					Type:    StepTypeTask,
					Handler: "final-handler",
				},
			},
		},
	}

	// Set up the workflow structure
	workflowDef.Definition.Steps["start"].Next = []string{"human_approval"}
	workflowDef.Definition.Steps["human_approval"].Next = []string{"final"}

	result := visualizer.RenderGraph(workflowDef)

	// Check that the result contains expected elements
	assert.Contains(t, result, "Workflow: approval_workflow (v1)")
	assert.Contains(t, result, "👤 human_approval [human]")
	assert.Contains(t, result, "👥 requires human confirmation")
	assert.Contains(t, result, "⚙ start [task]")
	assert.Contains(t, result, "⚙ final [task]")
}

func TestVisualizer_RenderInstanceStatus_WithHumanStep(t *testing.T) {
	visualizer := NewVisualizer()

	// Create a workflow instance
	instance := &WorkflowInstance{
		ID:         123,
		WorkflowID: "approval_workflow-v1",
		Status:     StatusRunning,
		Input:      []byte(`{"document_id": "DOC-001"}`),
		CreatedAt:  time.Now(),
	}

	// Create steps with different statuses including human step
	steps := []WorkflowStep{
		{
			ID:         1,
			InstanceID: 123,
			StepName:   "start",
			StepType:   StepTypeTask,
			Status:     StepStatusCompleted,
		},
		{
			ID:         2,
			InstanceID: 123,
			StepName:   "human_approval",
			StepType:   StepTypeHuman,
			Status:     StepStatusWaitingDecision,
		},
		{
			ID:         3,
			InstanceID: 123,
			StepName:   "final",
			StepType:   StepTypeTask,
			Status:     StepStatusPending,
		},
	}

	result := visualizer.RenderInstanceStatus(instance, steps)

	// Check that the result contains expected elements
	assert.Contains(t, result, "Workflow Instance: 123")
	assert.Contains(t, result, "Status: running")
	assert.Contains(t, result, "Workflow: approval_workflow-v1")

	// Check for completed steps
	assert.Contains(t, result, "✅ completed (1 steps):")
	assert.Contains(t, result, "⚙ start")

	// Check for human step waiting for decision
	assert.Contains(t, result, "⏳ waiting_decision (1 steps):")
	assert.Contains(t, result, "👤 human_approval ⏳ waiting for human decision")

	// Check for pending steps
	assert.Contains(t, result, "⏸ pending (1 steps):")
	assert.Contains(t, result, "⚙ final")
}

func TestVisualizer_RenderInstanceStatus_HumanStepConfirmed(t *testing.T) {
	visualizer := NewVisualizer()

	instance := &WorkflowInstance{
		ID:         456,
		WorkflowID: "approval_workflow-v1",
		Status:     StatusCompleted,
		Input:      []byte(`{"document_id": "DOC-002"}`),
		CreatedAt:  time.Now(),
	}

	steps := []WorkflowStep{
		{
			ID:         1,
			InstanceID: 456,
			StepName:   "start",
			StepType:   StepTypeTask,
			Status:     StepStatusCompleted,
		},
		{
			ID:         2,
			InstanceID: 456,
			StepName:   "human_approval",
			StepType:   StepTypeHuman,
			Status:     StepStatusConfirmed,
		},
		{
			ID:         3,
			InstanceID: 456,
			StepName:   "final",
			StepType:   StepTypeTask,
			Status:     StepStatusCompleted,
		},
	}

	result := visualizer.RenderInstanceStatus(instance, steps)

	// Check for confirmed human step
	assert.Contains(t, result, "✅ confirmed (1 steps):")
	assert.Contains(t, result, "👤 human_approval ✅ confirmed by human")
}

func TestVisualizer_RenderInstanceStatus_HumanStepRejected(t *testing.T) {
	visualizer := NewVisualizer()

	instance := &WorkflowInstance{
		ID:         789,
		WorkflowID: "approval_workflow-v1",
		Status:     StatusAborted,
		Input:      []byte(`{"document_id": "DOC-003"}`),
		CreatedAt:  time.Now(),
	}

	steps := []WorkflowStep{
		{
			ID:         1,
			InstanceID: 789,
			StepName:   "start",
			StepType:   StepTypeTask,
			Status:     StepStatusCompleted,
		},
		{
			ID:         2,
			InstanceID: 789,
			StepName:   "human_approval",
			StepType:   StepTypeHuman,
			Status:     StepStatusRejected,
		},
	}

	result := visualizer.RenderInstanceStatus(instance, steps)

	// Check for rejected human step
	assert.Contains(t, result, "❌ rejected (1 steps):")
	assert.Contains(t, result, "👤 human_approval ❌ rejected by human")
}

func TestVisualizer_GetStepSymbol(t *testing.T) {
	visualizer := NewVisualizer()

	tests := []struct {
		stepType StepType
		expected string
	}{
		{StepTypeHuman, "👤"},
		{StepTypeTask, "⚙"},
		{StepTypeFork, "🔀"},
		{StepTypeJoin, "🔗"},
		{StepTypeCondition, "❓"},
		{StepTypeSavePoint, "💾"},
		{StepTypeParallel, "∥"},
	}

	for _, test := range tests {
		result := visualizer.getStepSymbol(test.stepType)
		assert.Equal(t, test.expected, result, "StepType: %s", test.stepType)
	}
}

func TestVisualizer_GetStatusSymbol(t *testing.T) {
	visualizer := NewVisualizer()

	tests := []struct {
		status   StepStatus
		expected string
	}{
		{StepStatusCompleted, "✅"},
		{StepStatusConfirmed, "✅"},
		{StepStatusRunning, "🔄"},
		{StepStatusWaitingDecision, "⏳"},
		{StepStatusPending, "⏸"},
		{StepStatusFailed, "❌"},
		{StepStatusRejected, "❌"},
		{StepStatusSkipped, "⏭"},
		{StepStatusRolledBack, "↩"},
		{StepStatusCompensation, "🔄"},
	}

	for _, test := range tests {
		result := visualizer.getStatusSymbol(test.status)
		assert.Equal(t, test.expected, result, "StepStatus: %s", test.status)
	}
}
