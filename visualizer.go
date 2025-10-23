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

	output := fmt.Sprintf("%s→ %s [%s]\n", v.indent(indent), stepName, step.Type)

	if step.Handler != "" {
		output += fmt.Sprintf("%s  handler: %s\n", v.indent(indent), step.Handler)
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

func (v *Visualizer) indent(level int) string {
	result := ""
	for i := 0; i < level; i++ {
		result += "  "
	}
	return result
}
