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

	subBuilders []*Builder
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

func (builder *Builder) OnFailureFlow(name string, fn func(failureBuilder *Builder)) *Builder {
	if builder.currentStep == "" {
		panic(fmt.Sprintf("OnFailureFlow %q called with no step", name))
	}

	subBuilder := &Builder{
		name:    builder.name + "_onfailure_" + name,
		version: builder.version,
		steps:   make(map[string]*StepDefinition),
	}

	fn(subBuilder)
	builder.subBuilders = append(builder.subBuilders, subBuilder)

	for subStep, subDef := range subBuilder.steps {
		if _, ok := builder.steps[subStep]; ok {
			panic(fmt.Sprintf("duplicate step %q in on-failure flow %q", subStep, name))
		}

		builder.steps[subStep] = subDef
	}

	builder.steps[builder.currentStep].OnFailure = subBuilder.startStep

	return builder
}

func (builder *Builder) WithMetadata(key, value string) *Builder {
	if builder.currentStep != "" {
		builder.steps[builder.currentStep].Metadata[key] = value
	}

	return builder
}

func (builder *Builder) ParallelFlow(name string, branches ...func(branch *Builder)) *Builder {
	if builder.currentStep == "" {
		panic(fmt.Sprintf("ParallelFlow %q called with no current step", name))
	}

	parallelStep := &StepDefinition{
		Name:     name,
		Type:     StepTypeParallel,
		Parallel: []string{},
		Next:     []string{},
		Metadata: make(map[string]string),
	}
	builder.steps[name] = parallelStep

	builder.steps[builder.currentStep].Next = append(builder.steps[builder.currentStep].Next, name)

	for i, branchFn := range branches {
		sub := &Builder{
			name:    fmt.Sprintf("%s_branch_%d", builder.name, i+1),
			version: builder.version,
			steps:   make(map[string]*StepDefinition),
		}
		branchFn(sub)
		builder.subBuilders = append(builder.subBuilders, sub)

		for stepName, stepDef := range sub.steps {
			if _, ok := builder.steps[stepName]; ok {
				panic(fmt.Sprintf("duplicate step %q in parallel flow", stepName))
			}
			builder.steps[stepName] = stepDef
		}

		parallelStep.Parallel = append(parallelStep.Parallel, sub.startStep)
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
		joinStrategy = JoinStrategyAll
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
		return nil, fmt.Errorf("builder %q: at least one step is required", builder.name)
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

	for _, sub := range builder.subBuilders {
		if _, err := sub.Build(); err != nil {
			return nil, err
		}
	}

	return def, nil
}

func (builder *Builder) validate(def *WorkflowDefinition) error {
	for stepName, stepDef := range def.Definition.Steps {
		for _, nextStep := range stepDef.Next {
			if _, ok := def.Definition.Steps[nextStep]; !ok {
				return fmt.Errorf("builder %q: step %q references unknown step: %q",
					builder.name, stepName, nextStep)
			}
		}

		if stepDef.OnFailure != "" {
			if _, ok := def.Definition.Steps[stepDef.OnFailure]; !ok {
				return fmt.Errorf("builder %q: step %q references unknown compensation step: %q",
					builder.name, stepName, stepDef.OnFailure)
			}
		}

		for _, parallelStep := range stepDef.Parallel {
			if _, ok := def.Definition.Steps[parallelStep]; !ok {
				return fmt.Errorf("builder %q: step %q references unknown parallel step: %q",
					builder.name, stepName, parallelStep)
			}
		}

		if stepDef.Type == StepTypeTask && stepDef.Handler == "" {
			return fmt.Errorf("builder %q: task step %q must have a handler", builder.name, stepName)
		}
	}

	visited := make(map[string]bool)
	err := builder.detectCycles(def.Definition.Start, def.Definition.Steps, visited, make(map[string]bool))
	if err != nil {
		return fmt.Errorf("builder %q: %w", builder.name, err)
	}

	return nil
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
