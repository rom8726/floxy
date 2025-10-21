package floxy

type StepOption func(step *StepDefinition)

func WithStepMaxRetries(maxRetries int) StepOption {
	return func(step *StepDefinition) {
		step.MaxRetries = maxRetries
	}
}

func WithStepMetadata(metadata map[string]string) StepOption {
	return func(step *StepDefinition) {
		step.Metadata = metadata
	}
}

type BuilderOption func(builder *Builder)

func WithBuilderMaxRetries(maxRetries int) BuilderOption {
	return func(builder *Builder) {
		builder.defaultMaxRetries = maxRetries
	}
}
