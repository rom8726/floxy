package floxy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkflowBuilder(t *testing.T) {
	t.Run("simple workflow", func(t *testing.T) {
		wf, err := NewBuilder("test-workflow", 1).
			Step("step1", "handler1").
			Step("step2", "handler2").
			Step("step3", "handler3").
			Build()

		require.NoError(t, err)
		assert.Equal(t, "test-workflow-v1", wf.ID)
		assert.Equal(t, "test-workflow", wf.Name)
		assert.Equal(t, 1, wf.Version)
		assert.Equal(t, "step1", wf.Definition.Start)
		assert.Len(t, wf.Definition.Steps, 3)
	})

	t.Run("with compensation", func(t *testing.T) {
		wf, err := NewBuilder("saga-workflow", 1).
			Step("step1", "handler1").
			Step("step2", "handler2").
			OnFailure("compensation2").
			Step("step3", "handler3").
			Build()

		require.NoError(t, err)
		assert.Equal(t, "compensation2", wf.Definition.Steps["step2"].OnFailure)
	})

	t.Run("with max retries", func(t *testing.T) {
		wf, err := NewBuilder("retry-workflow", 1).
			Step("step1", "handler1").
			WithMaxRetries(5).
			Build()

		require.NoError(t, err)
		assert.Equal(t, 5, wf.Definition.Steps["step1"].MaxRetries)
	})

	t.Run("parallel steps", func(t *testing.T) {
		wf, err := NewBuilder("parallel-workflow", 1).
			Step("step1", "handler1").
			Parallel("parallel", "task1", "task2", "task3").
			Step("final", "final_handler").
			Build()

		require.NoError(t, err)
		assert.Equal(t, StepTypeParallel, wf.Definition.Steps["parallel"].Type)
		assert.Len(t, wf.Definition.Steps["parallel"].Parallel, 3)
	})

	t.Run("invalid reference", func(t *testing.T) {
		_, err := NewBuilder("invalid-workflow", 1).
			Step("step1", "handler1").
			OnFailure("nonexistent").
			Build()

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unknown compensation step")
	})
}
