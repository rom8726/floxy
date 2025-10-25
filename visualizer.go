package floxy

import (
	"fmt"
)

type Visualizer struct{}

func NewVisualizer() *Visualizer {
	return &Visualizer{}
}

func (v *Visualizer) RenderGraph(def *WorkflowDefinition) string {
	output := fmt.Sprintf("Workflow: %s (v%d)\n", def.Name, def.Version)
	output += "======================================\n\n"

	visited := make(map[string]bool)
	output += v.renderStep(def.Definition.Steps, def.Definition.Start, 0, visited)

	return output
}

func (v *Visualizer) renderStep(
	steps map[string]*StepDefinition,
	stepName string,
	indent int,
	visited map[string]bool,
) string {
	if visited[stepName] {
		return fmt.Sprintf("%s↻ %s (already visited)\n", v.indent(indent), stepName)
	}

	step, ok := steps[stepName]
	if !ok {
		return fmt.Sprintf("%s⚠ %s (not found)\n", v.indent(indent), stepName)
	}

	visited[stepName] = true

	// Use different symbols for different step types
	var stepSymbol string
	switch step.Type {
	case StepTypeHuman:
		stepSymbol = "👤" // Human step requires user confirmation
	case StepTypeTask:
		stepSymbol = "⚙" // Task step
	case StepTypeFork:
		stepSymbol = "🔀" // Fork step
	case StepTypeJoin:
		stepSymbol = "🔗" // Join step
	case StepTypeCondition:
		stepSymbol = "❓" // Condition step
	case StepTypeSavePoint:
		stepSymbol = "💾" // Save point step
	case StepTypeParallel:
		stepSymbol = "∥" // Parallel step
	default:
		stepSymbol = "→" // Default arrow
	}

	output := fmt.Sprintf("%s%s %s [%s]\n", v.indent(indent), stepSymbol, stepName, step.Type)

	if step.Handler != "" {
		output += fmt.Sprintf("%s  handler: %s\n", v.indent(indent), step.Handler)
	}

	// Add special note for human steps
	if step.Type == StepTypeHuman {
		output += fmt.Sprintf("%s  👥 requires human confirmation\n", v.indent(indent))
	}

	if step.OnFailure != "" {
		output += fmt.Sprintf("%s  ⚡ on failure: %s\n", v.indent(indent), step.OnFailure)
	}

	if step.MaxRetries > 0 {
		output += fmt.Sprintf("%s  🔄 max retries: %d\n", v.indent(indent), step.MaxRetries)
	}

	if len(step.Parallel) > 0 {
		output += fmt.Sprintf("%s  ∥ parallel:\n", v.indent(indent))
		for _, p := range step.Parallel {
			output += v.renderStep(steps, p, indent+2, visited)
		}
	}

	for _, next := range step.Next {
		output += v.renderStep(steps, next, indent+1, visited)
	}

	// Render Else branch for condition steps
	if step.Else != "" {
		output += fmt.Sprintf("%s  ↳ else: %s\n", v.indent(indent), step.Else)
		output += v.renderStep(steps, step.Else, indent+2, visited)
	}

	return output
}

func (v *Visualizer) RenderInstanceStatus(instance *WorkflowInstance, steps []WorkflowStep) string {
	output := fmt.Sprintf("Workflow Instance: %d\n", instance.ID)
	output += fmt.Sprintf("Status: %s\n", instance.Status)
	output += fmt.Sprintf("Workflow: %s\n", instance.WorkflowID)
	output += "======================================\n\n"

	// Group steps by status
	statusGroups := make(map[StepStatus][]WorkflowStep)
	for _, step := range steps {
		statusGroups[step.Status] = append(statusGroups[step.Status], step)
	}

	// Render steps by status
	statusOrder := []StepStatus{
		StepStatusCompleted,
		StepStatusConfirmed,
		StepStatusRunning,
		StepStatusWaitingDecision,
		StepStatusPending,
		StepStatusFailed,
		StepStatusRejected,
		StepStatusSkipped,
		StepStatusRolledBack,
		StepStatusCompensation,
	}

	for _, status := range statusOrder {
		if steps, exists := statusGroups[status]; exists {
			statusSymbol := v.getStatusSymbol(status)
			output += fmt.Sprintf("%s %s (%d steps):\n", statusSymbol, status, len(steps))

			for _, step := range steps {
				stepSymbol := v.getStepSymbol(step.StepType)
				output += fmt.Sprintf("  %s %s", stepSymbol, step.StepName)

				// Add special indicators for human steps
				if step.StepType == StepTypeHuman {
					switch step.Status {
					case StepStatusWaitingDecision:
						output += " ⏳ waiting for human decision"
					case StepStatusConfirmed:
						output += " ✅ confirmed by human"
					case StepStatusRejected:
						output += " ❌ rejected by human"
					}
				}

				output += "\n"
			}
			output += "\n"
		}
	}

	return output
}

func (v *Visualizer) getStatusSymbol(status StepStatus) string {
	switch status {
	case StepStatusCompleted:
		return "✅"
	case StepStatusConfirmed:
		return "✅"
	case StepStatusRunning:
		return "🔄"
	case StepStatusWaitingDecision:
		return "⏳"
	case StepStatusPending:
		return "⏸"
	case StepStatusFailed:
		return "❌"
	case StepStatusRejected:
		return "❌"
	case StepStatusSkipped:
		return "⏭"
	case StepStatusRolledBack:
		return "↩"
	case StepStatusCompensation:
		return "🔄"
	default:
		return "❓"
	}
}

func (v *Visualizer) getStepSymbol(stepType StepType) string {
	switch stepType {
	case StepTypeHuman:
		return "👤"
	case StepTypeTask:
		return "⚙"
	case StepTypeFork:
		return "🔀"
	case StepTypeJoin:
		return "🔗"
	case StepTypeCondition:
		return "❓"
	case StepTypeSavePoint:
		return "💾"
	case StepTypeParallel:
		return "∥"
	default:
		return "→"
	}
}

func (v *Visualizer) indent(level int) string {
	result := ""
	for i := 0; i < level; i++ {
		result += "  "
	}
	return result
}
