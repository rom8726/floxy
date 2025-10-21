package floxy

import (
	"errors"
	"fmt"
)

type Builder struct {
	name        string
	version     int
	steps       map[string]*StepDefinition
	startStep   string
	currentStep string
}

func NewBuilder(name string, version int) *Builder {
	return &Builder{
		name:    name,
		version: version,
		steps:   make(map[string]*StepDefinition),
	}
}

func (builder *Builder) Step(name, handler string) *Builder {
	step := &StepDefinition{
		Name:       name,
		Type:       StepTypeTask,
		Handler:    handler,
		MaxRetries: 3,
		Next:       []string{},
		Metadata:   make(map[string]string),
	}

	builder.steps[name] = step

	if builder.startStep == "" {
		builder.startStep = name
	}

	if builder.currentStep != "" && builder.currentStep != name {
		builder.steps[builder.currentStep].Next = append(builder.steps[builder.currentStep].Next, name)
	}

	builder.currentStep = name

	return builder
}

func (builder *Builder) WithMaxRetries(retries int) *Builder {
	if builder.currentStep != "" {
		builder.steps[builder.currentStep].MaxRetries = retries
	}

	return builder
}

func (builder *Builder) OnFailure(name, handler string) *Builder {
	if builder.currentStep != "" {
		onFailureStep := &StepDefinition{
			Name:       name,
			Type:       StepTypeTask,
			Handler:    handler,
			MaxRetries: 3,
			Next:       []string{},
			Metadata:   make(map[string]string),
		}
		builder.steps[name] = onFailureStep

		builder.steps[builder.currentStep].OnFailure = name
	}

	return builder
}

func (builder *Builder) WithMetadata(key, value string) *Builder {
	if builder.currentStep != "" {
		builder.steps[builder.currentStep].Metadata[key] = value
	}

	return builder
}

func (builder *Builder) Parallel(name string, parallelSteps ...string) *Builder {
	step := &StepDefinition{
		Name:       name,
		Type:       StepTypeParallel,
		Handler:    "",
		MaxRetries: 0,
		Parallel:   parallelSteps,
		Next:       []string{},
		Metadata:   make(map[string]string),
	}

	builder.steps[name] = step

	if builder.currentStep != "" && builder.currentStep != name {
		builder.steps[builder.currentStep].Next = append(builder.steps[builder.currentStep].Next, name)
	}

	builder.currentStep = name

	return builder
}

func (builder *Builder) ForkJoin(
	forkName string,
	parallelSteps []string,
	joinName string,
	joinStrategy JoinStrategy,
) *Builder {
	forkStep := &StepDefinition{
		Name:       forkName,
		Type:       StepTypeFork,
		Handler:    "",
		MaxRetries: 0,
		Parallel:   parallelSteps,
		Next:       []string{joinName},
		Metadata:   make(map[string]string),
	}
	builder.steps[forkName] = forkStep

	if builder.currentStep != "" && builder.currentStep != forkName {
		builder.steps[builder.currentStep].Next = append(builder.steps[builder.currentStep].Next, forkName)
	}

	if joinStrategy == "" {
		joinStrategy = "all"
	}

	joinStep := &StepDefinition{
		Name:         joinName,
		Type:         StepTypeJoin,
		Handler:      "",
		MaxRetries:   0,
		WaitFor:      parallelSteps,
		JoinStrategy: joinStrategy,
		Next:         []string{},
		Metadata:     make(map[string]string),
	}
	builder.steps[joinName] = joinStep

	builder.currentStep = joinName

	return builder
}

func (builder *Builder) ForkStep(name string, parallelSteps ...string) *Builder {
	step := &StepDefinition{
		Name:       name,
		Type:       StepTypeFork,
		Handler:    "",
		MaxRetries: 0,
		Parallel:   parallelSteps,
		Next:       []string{},
		Metadata:   make(map[string]string),
	}

	builder.steps[name] = step

	if builder.currentStep != "" && builder.currentStep != name {
		builder.steps[builder.currentStep].Next = append(builder.steps[builder.currentStep].Next, name)
	}

	builder.currentStep = name

	return builder
}

func (builder *Builder) JoinStep(name string, waitFor []string, strategy JoinStrategy) *Builder {
	if strategy == "" {
		strategy = JoinStrategyAll
	}

	step := &StepDefinition{
		Name:         name,
		Type:         StepTypeJoin,
		Handler:      "",
		MaxRetries:   0,
		WaitFor:      waitFor,
		JoinStrategy: strategy,
		Next:         []string{},
		Metadata:     make(map[string]string),
	}

	builder.steps[name] = step

	if builder.currentStep != "" && builder.currentStep != name {
		builder.steps[builder.currentStep].Next = append(builder.steps[builder.currentStep].Next, name)
	}

	builder.currentStep = name

	return builder
}

func (builder *Builder) Then(name, handler string) *Builder {
	return builder.Step(name, handler)
}

func (builder *Builder) Fork(branches ...string) *Builder {
	if builder.currentStep != "" {
		builder.steps[builder.currentStep].Next = append(builder.steps[builder.currentStep].Next, branches...)
	}

	return builder
}

func (builder *Builder) Build() (*WorkflowDefinition, error) {
	if builder.name == "" {
		return nil, errors.New("workflow name is required")
	}

	if builder.startStep == "" {
		return nil, errors.New("at least one step is required")
	}

	id := fmt.Sprintf("%s-v%d", builder.name, builder.version)

	def := &WorkflowDefinition{
		ID:      id,
		Name:    builder.name,
		Version: builder.version,
		Definition: GraphDefinition{
			Start: builder.startStep,
			Steps: builder.steps,
		},
	}

	if err := builder.validate(def); err != nil {
		return nil, err
	}

	return def, nil
}

func (builder *Builder) validate(def *WorkflowDefinition) error {
	for stepName, stepDef := range def.Definition.Steps {
		for _, nextStep := range stepDef.Next {
			if _, ok := def.Definition.Steps[nextStep]; !ok {
				return fmt.Errorf("step %q references unknown step: %q", stepName, nextStep)
			}
		}

		if stepDef.OnFailure != "" {
			if _, ok := def.Definition.Steps[stepDef.OnFailure]; !ok {
				return fmt.Errorf("step %q references unknown compensation step: %q",
					stepName, stepDef.OnFailure)
			}
		}

		for _, parallelStep := range stepDef.Parallel {
			if _, ok := def.Definition.Steps[parallelStep]; !ok {
				return fmt.Errorf("step %q references unknown parallel step: %q", stepName, parallelStep)
			}
		}

		if stepDef.Type == StepTypeTask && stepDef.Handler == "" {
			return fmt.Errorf("task step %q must have a handler", stepName)
		}
	}

	visited := make(map[string]bool)
	err := builder.detectCycles(def.Definition.Start, def.Definition.Steps, visited, make(map[string]bool))

	return err
}

func (builder *Builder) detectCycles(
	current string,
	steps map[string]*StepDefinition,
	visited, recStack map[string]bool,
) error {
	visited[current] = true
	recStack[current] = true

	step, ok := steps[current]
	if !ok {
		return nil
	}

	for _, next := range step.Next {
		if !visited[next] {
			if err := builder.detectCycles(next, steps, visited, recStack); err != nil {
				return err
			}
		} else if recStack[next] {
			return fmt.Errorf("cycle detected: %s -> %s", current, next)
		}
	}

	recStack[current] = false

	return nil
}
