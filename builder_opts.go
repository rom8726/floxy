package floxy

import (
	"log"
	"time"
)

type StepOption func(step *StepDefinition)

func WithStepMaxRetries(maxRetries int) StepOption {
	return func(step *StepDefinition) {
		if step.NoIdempotent {
			log.Println("WithStepMaxRetries: unable to set MaxRetries to not idempotent step")

			return
		}

		step.MaxRetries = maxRetries
	}
}

func WithStepMetadata(metadata map[string]any) StepOption {
	return func(step *StepDefinition) {
		step.Metadata = metadata
	}
}

func WithStepNoIdempotent() StepOption {
	return func(step *StepDefinition) {
		step.NoIdempotent = true
		step.MaxRetries = 1
	}
}

func WithStepDelay(delay time.Duration) StepOption {
	return func(step *StepDefinition) {
		step.Delay = delay
	}
}

func WithStepRetryDelay(retryDelay time.Duration) StepOption {
	return func(step *StepDefinition) {
		step.RetryDelay = retryDelay
	}
}

func WithStepTimeout(timeout time.Duration) StepOption {
	return func(step *StepDefinition) {
		step.Timeout = timeout
	}
}

type BuilderOption func(builder *Builder)

func WithBuilderMaxRetries(maxRetries int) BuilderOption {
	return func(builder *Builder) {
		builder.defaultMaxRetries = maxRetries
	}
}
