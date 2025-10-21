package floxy

import (
	"errors"
	"fmt"
	"slices"
)

const (
	defaultMaxRetries = 3
)

type Builder struct {
	name        string
	version     int
	steps       map[string]*StepDefinition
	startStep   string
	currentStep string

	subBuilders       []*Builder
	defaultMaxRetries int
}

func NewBuilder(name string, version int, opts ...BuilderOption) *Builder {
	builder := &Builder{
		name:              name,
		version:           version,
		steps:             make(map[string]*StepDefinition),
		defaultMaxRetries: defaultMaxRetries,
	}

	for _, opt := range opts {
		opt(builder)
	}

	return builder
}

func (builder *Builder) Step(name, handler string) *Builder {
	step := &StepDefinition{
		Name:       name,
		Type:       StepTypeTask,
		Handler:    handler,
		MaxRetries: builder.defaultMaxRetries,
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

func (builder *Builder) OnFailure(name, handler string, opts ...StepOption) *Builder {
	if builder.currentStep == "" {
		panic(fmt.Sprintf("OnFailure %q called with no step", name))
	}

	compensation := &StepDefinition{
		Name:       name,
		Type:       StepTypeTask,
		Handler:    handler,
		MaxRetries: builder.defaultMaxRetries,
	}
	for _, opt := range opts {
		opt(compensation)
	}

	builder.steps[name] = compensation
	builder.steps[builder.currentStep].OnFailure = name

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

	if subBuilder.startStep == "" {
		panic(fmt.Sprintf("OnFailureFlow %q: sub-flow must contain at least one step", name))
	}
	if _, err := subBuilder.Build(); err != nil {
		panic(fmt.Sprintf("OnFailureFlow %q build: %v", name, err))
	}

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

func (builder *Builder) Parallel(name string, tasks ...*StepDefinition) *Builder {
	parallelStep := &StepDefinition{
		Name:     name,
		Type:     StepTypeParallel,
		Metadata: make(map[string]string),
		Parallel: []string{},
	}

	if _, ok := builder.steps[name]; ok {
		panic(fmt.Sprintf("duplicate parallel step %q", name))
	}
	builder.steps[name] = parallelStep

	if builder.currentStep != "" && builder.currentStep != name {
		builder.steps[builder.currentStep].Next = append(builder.steps[builder.currentStep].Next, name)
	}

	for i, task := range tasks {
		if task.Name == "" {
			panic(fmt.Sprintf("tasks[%d] in parallel %q missing step name", i, name))
		}
		if task.Handler == "" {
			panic(fmt.Sprintf("tasks[%d] in parallel %q missing step handler", i, name))
		}
		if _, ok := builder.steps[task.Name]; ok {
			panic(fmt.Sprintf("duplicate step %q in parallel %q", task.Name, name))
		}

		builder.steps[task.Name] = task

		parallelStep.Parallel = append(parallelStep.Parallel, task.Name)
	}

	builder.currentStep = name

	return builder
}

func (builder *Builder) Fork(name string, branches ...func(branch *Builder)) *Builder {
	if len(branches) < 2 {
		panic(fmt.Sprintf("ForkFlow %q called with count of branches < 2", name))
	}

	forkStep := &StepDefinition{
		Name:     name,
		Type:     StepTypeFork,
		Metadata: make(map[string]string),
	}

	builder.steps[name] = forkStep

	if builder.currentStep != "" && builder.currentStep != name {
		builder.steps[builder.currentStep].Next = append(builder.steps[builder.currentStep].Next, name)
	}

	for i, branchFn := range branches {
		sub := &Builder{
			name:    fmt.Sprintf("%s_branch_%d", builder.name, i+1),
			version: builder.version,
			steps:   make(map[string]*StepDefinition),
		}

		branchFn(sub)

		if sub.startStep == "" {
			panic(fmt.Sprintf("Fork %q: branch %d has no steps", name, i+1))
		}
		if _, err := sub.Build(); err != nil {
			panic(fmt.Sprintf("invalid branch %d for Fork %q: %v", i+1, name, err))
		}

		builder.subBuilders = append(builder.subBuilders, sub)

		for stepName, stepDef := range sub.steps {
			if _, ok := builder.steps[stepName]; ok {
				panic(fmt.Sprintf("duplicate step %q in fork flow %q", stepName, name))
			}
			builder.steps[stepName] = stepDef
		}

		forkStep.Parallel = append(forkStep.Parallel, sub.startStep)
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
		WaitFor:      waitFor,
		JoinStrategy: strategy,
		Metadata:     make(map[string]string),
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
	branches []func(branch *Builder),
	joinName string,
	joinStrategy JoinStrategy,
) *Builder {
	_ = builder.Fork(forkName, branches...)
	forkStep := builder.steps[forkName]
	waitFor := slices.Clone(forkStep.Parallel)

	return builder.JoinStep(joinName, waitFor, joinStrategy)
}

func (builder *Builder) Then(name, handler string) *Builder {
	return builder.Step(name, handler)
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

func NewTask(name, handler string, opts ...StepOption) *StepDefinition {
	step := &StepDefinition{
		Name:       name,
		Handler:    handler,
		Type:       StepTypeTask,
		Metadata:   make(map[string]string),
		MaxRetries: defaultMaxRetries,
	}

	for _, opt := range opts {
		opt(step)
	}

	return step
}
