package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rom8726/floxy"
)

func TestInMemoryStoreWorkflow(t *testing.T) {
	ctx := context.Background()
	store := floxy.NewMemoryStore()
	txManager := floxy.NewMemoryTxManager()

	engine := floxy.NewEngine(nil,
		floxy.WithEngineStore(store),
		floxy.WithEngineTxManager(txManager),
	)
	defer engine.Shutdown()

	engine.RegisterHandler(&ProcessHandler{})

	workflowDef, err := floxy.NewBuilder("processing-workflow", 1).
		Step("process-item", "process").
		Build()
	require.NoError(t, err)

	err = engine.RegisterWorkflow(ctx, workflowDef)
	require.NoError(t, err)

	workerPool := floxy.NewWorkerPool(engine, 2, 50*time.Millisecond)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	workerPool.Start(ctx)

	input := json.RawMessage(`{"item": "test-item-456"}`)
	instanceID, err := engine.Start(ctx, workflowDef.ID, input)
	require.NoError(t, err)

	time.Sleep(500 * time.Millisecond)

	workerPool.Stop()
	cancel()

	instance, err := store.GetInstance(ctx, instanceID)
	require.NoError(t, err)
	assert.Equal(t, floxy.StatusCompleted, instance.Status)

	var outputData map[string]interface{}
	require.NoError(t, json.Unmarshal(instance.Output, &outputData))
	assert.Contains(t, outputData["item"], "processed")

	steps, err := store.GetStepsByInstance(ctx, instanceID)
	require.NoError(t, err)
	assert.Len(t, steps, 1)
	assert.Equal(t, "process-item", steps[0].StepName)
	assert.Equal(t, floxy.StepStatusCompleted, steps[0].Status)

	stats, err := store.GetSummaryStats(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint(1), stats.TotalWorkflows)
	assert.Equal(t, uint(1), stats.CompletedWorkflows)
	assert.Equal(t, uint(0), stats.FailedWorkflows)
	assert.Equal(t, uint(0), stats.RunningWorkflows)
	assert.Equal(t, uint(0), stats.PendingWorkflows)
	assert.Equal(t, uint(0), stats.ActiveWorkflows)

	events, err := store.GetWorkflowEvents(ctx, instanceID)
	require.NoError(t, err)
	assert.Greater(t, len(events), 0)
}

func TestInMemoryStoreMultipleInstances(t *testing.T) {
	ctx := context.Background()
	store := floxy.NewMemoryStore()
	txManager := floxy.NewMemoryTxManager()

	engine := floxy.NewEngine(nil,
		floxy.WithEngineStore(store),
		floxy.WithEngineTxManager(txManager),
	)
	defer engine.Shutdown()

	engine.RegisterHandler(&ProcessHandler{})

	workflowDef, err := floxy.NewBuilder("processing-workflow", 1).
		Step("process-item", "process").
		Build()
	require.NoError(t, err)

	err = engine.RegisterWorkflow(ctx, workflowDef)
	require.NoError(t, err)

	workerPool := floxy.NewWorkerPool(engine, 2, 50*time.Millisecond)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	workerPool.Start(ctx)

	input1 := json.RawMessage(`{"item": "item-1"}`)
	instanceID1, err := engine.Start(ctx, workflowDef.ID, input1)
	require.NoError(t, err)

	input2 := json.RawMessage(`{"item": "item-2"}`)
	instanceID2, err := engine.Start(ctx, workflowDef.ID, input2)
	require.NoError(t, err)

	time.Sleep(500 * time.Millisecond)

	workerPool.Stop()
	cancel()

	instance1, err := store.GetInstance(ctx, instanceID1)
	require.NoError(t, err)
	assert.Equal(t, floxy.StatusCompleted, instance1.Status)

	instance2, err := store.GetInstance(ctx, instanceID2)
	require.NoError(t, err)
	assert.Equal(t, floxy.StatusCompleted, instance2.Status)

	allInstances, err := store.GetAllWorkflowInstances(ctx)
	require.NoError(t, err)
	assert.Len(t, allInstances, 2)

	workflowInstances, err := store.GetWorkflowInstances(ctx, workflowDef.ID)
	require.NoError(t, err)
	assert.Len(t, workflowInstances, 2)

	stats, err := store.GetSummaryStats(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint(2), stats.TotalWorkflows)
	assert.Equal(t, uint(2), stats.CompletedWorkflows)
}

func TestInMemoryStoreWorkflowDefinition(t *testing.T) {
	ctx := context.Background()
	store := floxy.NewMemoryStore()

	def := &floxy.WorkflowDefinition{
		ID:      "test-workflow",
		Name:    "test-workflow",
		Version: 1,
		Definition: floxy.GraphDefinition{
			Steps: map[string]*floxy.StepDefinition{
				"step1": {
					Name: "step1",
					Type: floxy.StepTypeTask,
				},
			},
			Start: "step1",
		},
	}

	err := store.SaveWorkflowDefinition(ctx, def)
	require.NoError(t, err)

	retrieved, err := store.GetWorkflowDefinition(ctx, def.ID)
	require.NoError(t, err)
	assert.Equal(t, def.ID, retrieved.ID)
	assert.Equal(t, def.Name, retrieved.Name)
	assert.Equal(t, def.Version, retrieved.Version)

	definitions, err := store.GetWorkflowDefinitions(ctx)
	require.NoError(t, err)
	assert.Len(t, definitions, 1)
}

func TestInMemoryStoreQueueOperations(t *testing.T) {
	ctx := context.Background()
	store := floxy.NewMemoryStore()

	instanceID := int64(1)
	stepID := int64(10)

	err := store.EnqueueStep(ctx, instanceID, &stepID, floxy.PriorityNormal, 0)
	require.NoError(t, err)

	item, err := store.DequeueStep(ctx, "worker-1")
	require.NoError(t, err)
	require.NotNil(t, item)
	assert.Equal(t, instanceID, item.InstanceID)
	assert.Equal(t, stepID, *item.StepID)

	err = store.RemoveFromQueue(ctx, item.ID)
	require.NoError(t, err)

	item, err = store.DequeueStep(ctx, "worker-1")
	require.NoError(t, err)
	assert.Nil(t, item)
}

func TestInMemoryStoreJoinState(t *testing.T) {
	ctx := context.Background()
	store := floxy.NewMemoryStore()

	instanceID := int64(1)
	joinStepName := "join-step"

	err := store.CreateJoinState(ctx, instanceID, joinStepName, []string{"step1", "step2"}, floxy.JoinStrategyAll)
	require.NoError(t, err)

	state, err := store.GetJoinState(ctx, instanceID, joinStepName)
	require.NoError(t, err)
	assert.Equal(t, instanceID, state.InstanceID)
	assert.Equal(t, joinStepName, state.JoinStepName)
	assert.Len(t, state.WaitingFor, 2)
	assert.False(t, state.IsReady)

	isReady, err := store.UpdateJoinState(ctx, instanceID, joinStepName, "step1", true)
	require.NoError(t, err)
	assert.False(t, isReady)

	isReady, err = store.UpdateJoinState(ctx, instanceID, joinStepName, "step2", true)
	require.NoError(t, err)
	assert.True(t, isReady)

	state, err = store.GetJoinState(ctx, instanceID, joinStepName)
	require.NoError(t, err)
	assert.True(t, state.IsReady)
	assert.Len(t, state.Completed, 2)
}

func TestInMemoryStoreEvents(t *testing.T) {
	ctx := context.Background()
	store := floxy.NewMemoryStore()

	instanceID := int64(1)
	stepID := int64(10)

	err := store.LogEvent(ctx, instanceID, &stepID, "test-event", map[string]string{"key": "value"})
	require.NoError(t, err)

	events, err := store.GetWorkflowEvents(ctx, instanceID)
	require.NoError(t, err)
	assert.Len(t, events, 1)
	assert.Equal(t, "test-event", events[0].EventType)
}
