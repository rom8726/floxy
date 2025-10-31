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

	t.Run("with compensation task", func(t *testing.T) {
		wf, err := NewBuilder("compensation-task", 1).
			Step("step1", "handler1").
			Step("step2", "handler2").
			OnFailure("compensation2", "compensation2_handler").
			Step("step3", "handler3").
			Build()

		require.NoError(t, err)
		assert.Equal(t, "compensation2", wf.Definition.Steps["step2"].OnFailure)
	})

	t.Run("parallel steps", func(t *testing.T) {
		wf, err := NewBuilder("parallel-workflow", 1).
			Step("step1", "handler1").
			Parallel("parallel",
				NewTask("task1", "handler1"),
				NewTask("task2", "handler2"),
				NewTask("task3", "handler3"),
			).
			Step("final", "final_handler").
			Build()

		require.NoError(t, err)
		assert.Equal(t, StepTypeParallel, wf.Definition.Steps["parallel"].Type)
		assert.ElementsMatch(t,
			[]string{"task1", "task2", "task3"},
			wf.Definition.Steps["parallel"].Parallel)
		assert.Equal(t, "parallel_join", wf.Definition.Steps["parallel"].Next[0])
		assert.Equal(t, "final", wf.Definition.Steps["parallel_join"].Next[0])
	})

	t.Run("fork flow", func(t *testing.T) {
		wf, err := NewBuilder("fork-workflow", 1).
			Step("step1", "handler1").
			Fork("fork",
				func(branch1 *Builder) {
					branch1.Step("a1", "h1").Step("a2", "h2")
				},
				func(branch2 *Builder) {
					branch2.Step("b1", "h3")
				},
			).
			Join("join", JoinStrategyAll).
			Step("final", "final_handler").
			Build()

		require.NoError(t, err)
		assert.Equal(t, StepTypeFork, wf.Definition.Steps["fork"].Type)
		assert.Len(t, wf.Definition.Steps["fork"].Parallel, 2)
		assert.Equal(t, StepTypeJoin, wf.Definition.Steps["join"].Type)
		//assert.ElementsMatch(t, []string{"a2", "b1"}, wf.Definition.Steps["join"].WaitFor)
	})

	t.Run("fork join sugar", func(t *testing.T) {
		wf, err := NewBuilder("forkjoin-workflow", 1).
			Step("step1", "handler1").
			ForkJoin("fork",
				[]func(b *Builder){
					func(b *Builder) { b.Step("b1", "h1") },
					func(b *Builder) { b.Step("b2", "h2") },
				},
				"join", JoinStrategyAll,
			).
			Step("next", "handler_next").
			Build()

		require.NoError(t, err)
		assert.Equal(t, StepTypeFork, wf.Definition.Steps["fork"].Type)
		assert.Equal(t, StepTypeJoin, wf.Definition.Steps["join"].Type)
		//assert.ElementsMatch(t, []string{"b1", "b2"}, wf.Definition.Steps["join"].WaitFor)
	})

	t.Run("error: duplicate step", func(t *testing.T) {
		_, err := NewBuilder("dup", 1).
			Step("s1", "h1").
			Step("s1", "h2").Build()
		require.Error(t, err)
	})

	t.Run("error: unknown next step", func(t *testing.T) {
		builder := NewBuilder("invalid-next", 1)
		builder.steps["a"] = &StepDefinition{
			Name: "a", Type: StepTypeTask, Handler: "ha", Next: []string{"b"},
		}
		_, err := builder.Build()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "at least one step is required")
	})

	t.Run("error: cycle detection", func(t *testing.T) {
		builder := NewBuilder("cyclic", 1)
		builder.Step("a", "ha").
			Step("b", "hb")

		// create a cycle manually
		builder.steps["b"].Next = append(builder.steps["b"].Next, "a")

		_, err := builder.Build()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cycle detected")
	})

	t.Run("error: invalid fork with one branch", func(t *testing.T) {
		_, err := NewBuilder("invalid-fork", 1).
			Step("start", "h1").
			Fork("fork", func(b *Builder) {
				b.Step("branch", "h2")
			}).Build()
		require.Error(t, err)
	})

	t.Run("complex flow with fork, parallel, on-failure", func(t *testing.T) {
		wf, err := NewBuilder("complex", 1).
			Step("start", "init").
			Fork("fork1",
				func(b *Builder) {
					b.Step("a1", "ha").
						Parallel("p1",
							NewTask("pa", "hpa"),
							NewTask("pb", "hpb")).
						Step("a2", "ha2")
				},
				func(b *Builder) {
					b.Step("b1", "hb").
						OnFailure("b_fail", "h_fail").
						Step("b2", "hb2")
				},
			).
			Join("join", JoinStrategyAll).
			Step("final", "finish").
			Build()

		require.NoError(t, err)
		assert.Equal(t, "complex-v1", wf.ID)
		assert.Equal(t, StepTypeFork, wf.Definition.Steps["fork1"].Type)
		assert.Equal(t, StepTypeJoin, wf.Definition.Steps["join"].Type)
		assert.Contains(t, wf.Definition.Steps, "p1")
	})

	t.Run("error: empty step name", func(t *testing.T) {
		_, err := NewBuilder("empty-step", 1).
			Step("", "handler").Build()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "at least one step is required")
	})

	t.Run("error: _root_ step name", func(t *testing.T) {
		_, err := NewBuilder("_root_", 1).
			Step("_root_", "handler").Build()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "step name cannot be _root_")
	})
}
