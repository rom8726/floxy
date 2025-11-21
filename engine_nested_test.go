package floxy

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type maybeErrorCtxKey struct{}

func TestEngineNested(t *testing.T) {
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
	defer func() { _ = engine.Shutdown(time.Second) }()

	target := &FloxyStressTarget{
		engine: engine,
	}

	target.registerHandlers()

	workflowDef := nestedWorkflow(t)

	err := engine.RegisterWorkflow(ctx, workflowDef)
	require.NoError(t, err)

	t.Run("without error", func(t *testing.T) {
		// Create random order data
		order := map[string]any{
			"order_id": fmt.Sprintf("ORD-%d", time.Now().UnixNano()),
			"user_id":  fmt.Sprintf("user-%d", rand.Intn(1000)),
			"amount":   float64(rand.Intn(500) + 50),
			"items":    rand.Intn(10) + 1,
		}

		input, _ := json.Marshal(order)

		instanceID, err := engine.Start(ctx, "nested-workflow-v1", input)
		require.NoError(t, err)

		// Process workflow until completion or failure
		for i := 0; i < 20; i++ {
			empty, err := engine.ExecuteNext(ctx, "worker1")
			require.NoError(t, err)
			if empty {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}

		// Check final status
		status, err := engine.GetStatus(ctx, instanceID)
		require.NoError(t, err)

		// Check steps
		steps, err := engine.GetSteps(ctx, instanceID)
		require.NoError(t, err)

		stepNames := make([]string, len(steps))
		stepStatuses := make(map[string]StepStatus)
		for i, step := range steps {
			stepNames[i] = step.StepName
			stepStatuses[step.StepName] = step.Status
		}

		t.Logf("Final status: %s", status)
		t.Logf("Executed steps: %v", stepNames)
		t.Logf("Step statuses: %v", stepStatuses)

		assert.Equal(t, StatusCompleted, status)
		assert.Equal(t,
			[]string{"validate", "process-payment", "payment-checkpoint", "nested-parallel",
				"reserve-inventory-1", "reserve-inventory-2", "ship-1", "ship-2", "join", "notify"},
			stepNames,
		)
		for _, step := range steps {
			assert.Equal(t, StepStatusCompleted, step.Status, "Step %s should be completed", step.StepName)
		}
	})

	t.Run("with error in shipping", func(t *testing.T) {
		// Create random order data
		order := map[string]any{
			"order_id": fmt.Sprintf("ORD-%d", time.Now().UnixNano()),
			"user_id":  fmt.Sprintf("user-%d", rand.Intn(1000)),
			"amount":   float64(rand.Intn(500) + 50),
			"items":    rand.Intn(10) + 1,
		}

		input, _ := json.Marshal(order)

		instanceID, err := engine.Start(ctx, "nested-workflow-v1", input)
		require.NoError(t, err)

		ctx = context.WithValue(ctx, maybeErrorCtxKey{}, "shipping")

		// Process workflow until completion or failure
		for i := 0; i < 100; i++ {
			empty, err := engine.ExecuteNext(ctx, "worker1")
			require.NoError(t, err)
			if empty {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}

		// Check final status
		status, err := engine.GetStatus(ctx, instanceID)
		require.NoError(t, err)

		// Check steps
		steps, err := engine.GetSteps(ctx, instanceID)
		require.NoError(t, err)

		stepNames := make([]string, len(steps))
		stepStatuses := make(map[string]StepStatus)
		for i, step := range steps {
			stepNames[i] = step.StepName
			stepStatuses[step.StepName] = step.Status
		}

		t.Logf("Final status: %s", status)
		t.Logf("Executed steps: %v", stepNames)
		t.Logf("Step statuses: %v", stepStatuses)

		assert.Equal(t, StatusFailed, status)

		for _, step := range steps {
			switch step.StepName {
			case "validate", "process-payment", "payment-checkpoint":
				assert.Equal(t, StepStatusCompleted, step.Status)
			case "fork", "join", "nested-parallel", "reserve-inventory-1", "reserve-inventory-2", "ship-1", "ship-2":
				assert.Equal(t, StepStatusRolledBack, step.Status)
			default:
				assert.Fail(t, "Unexpected step %s", step.StepName)
			}
		}
	})
}
