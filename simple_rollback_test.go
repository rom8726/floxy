package floxy

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSimpleRollbackWithCondition(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	store, txManager, cleanup := setupTestStore(t)
	t.Cleanup(cleanup)

	ctx := context.Background()
	engine := NewEngine(nil,
		WithEngineStore(store),
		WithEngineTxManager(txManager),
		WithEngineCancelInterval(time.Minute),
	)
	defer engine.Shutdown()

	// Register handlers
	engine.RegisterHandler(&SimpleTestHandler{})
	engine.RegisterHandler(&FailingHandler{})

	// Create a simple workflow: start -> condition -> next (fails) -> final
	workflowDef, err := NewBuilder("simple_rollback", 1).
		Step("start", "simple-test", WithStepMaxRetries(1)).
		Condition("condition", "{{ eq .count 2 }}", func(elseBranch *Builder) {
			elseBranch.Step("else_step", "simple-test", WithStepMaxRetries(1))
		}).
		Then("next_step", "failing-handler", WithStepMaxRetries(1)). // This will fail
		Then("final", "simple-test", WithStepMaxRetries(1)).
		Build()

	require.NoError(t, err)
	err = engine.RegisterWorkflow(ctx, workflowDef)
	require.NoError(t, err)

	// Start with count = 2 (condition will be false -> else)
	input := json.RawMessage(`{"count": 2}`)
	instanceID, err := engine.Start(ctx, "simple_rollback-v1", input)
	require.NoError(t, err)

	// Process workflow until failure
	for i := 0; i < 10; i++ {
		empty, err := engine.ExecuteNext(ctx, "worker1")
		require.NoError(t, err)
		if empty {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Check final status - should be failed
	status, err := engine.GetStatus(ctx, instanceID)
	require.NoError(t, err)
	assert.Equal(t, StatusFailed, status)

	// Check steps
	steps, err := engine.GetSteps(ctx, instanceID)
	require.NoError(t, err)

	stepNames := make([]string, len(steps))
	stepStatuses := make(map[string]StepStatus)
	for i, step := range steps {
		stepNames[i] = step.StepName
		stepStatuses[step.StepName] = step.Status
	}

	t.Logf("Executed steps: %v", stepNames)
	t.Logf("Step statuses: %v", stepStatuses)

	// Verify that the final was not executed
	assert.NotContains(t, stepNames, "final")

	// Verify that the executed steps are present
	assert.Contains(t, stepNames, "start")
	assert.Contains(t, stepNames, "condition")
	assert.Contains(t, stepNames, "next_step")
}

type SimpleTestHandler struct{}

func (h *SimpleTestHandler) Name() string { return "simple-test" }

func (h *SimpleTestHandler) Execute(ctx context.Context, stepCtx StepContext, input json.RawMessage) (json.RawMessage, error) {
	// Just pass through the input
	return input, nil
}
