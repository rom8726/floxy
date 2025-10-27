package validate

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/rom8726/floxy"
)

var _ floxy.Plugin = (*ValidationPlugin)(nil)

type ValidationRule func(data json.RawMessage) error

type ValidationPlugin struct {
	floxy.BasePlugin

	rules map[string][]ValidationRule
	mu    sync.RWMutex
}

func New() *ValidationPlugin {
	return &ValidationPlugin{
		BasePlugin: floxy.NewBasePlugin("validation", floxy.PriorityHigh),
		rules:      make(map[string][]ValidationRule),
	}
}

func (p *ValidationPlugin) AddRule(stepName string, rule ValidationRule) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.rules[stepName]; !exists {
		p.rules[stepName] = make([]ValidationRule, 0)
	}

	p.rules[stepName] = append(p.rules[stepName], rule)
}

func (p *ValidationPlugin) OnStepStart(
	_ context.Context,
	_ *floxy.WorkflowInstance,
	step *floxy.WorkflowStep,
) error {
	p.mu.RLock()
	rules, exists := p.rules[step.StepName]
	p.mu.RUnlock()

	if !exists {
		return nil
	}

	for i, rule := range rules {
		if err := rule(step.Input); err != nil {
			return fmt.Errorf("validation rule %d failed for step %q: %w", i, step.StepName, err)
		}
	}

	return nil
}
