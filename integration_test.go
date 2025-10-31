package floxy

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func setupTestDatabase(t *testing.T) (testcontainers.Container, *pgxpool.Pool) {
	ctx := context.Background()

	// Start PostgreSQL container
	postgresContainer, err := postgres.Run(ctx,
		"postgres:17-alpine",
		postgres.WithDatabase("floxy"),
		postgres.WithUsername("user"),
		postgres.WithPassword("password"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second)),
	)
	require.NoError(t, err)

	// Get connection string
	connStr, err := postgresContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	// Wait a bit for PostgreSQL to be fully ready
	time.Sleep(2 * time.Second)

	// Create connection pool with retry
	var pool *pgxpool.Pool
	for i := 0; i < 5; i++ {
		pool, err = pgxpool.New(ctx, connStr)
		if err == nil {
			break
		}
		if i < 4 {
			time.Sleep(time.Duration(i+1) * time.Second)
		}
	}
	require.NoError(t, err)

	// Run migrations
	err = RunMigrations(ctx, pool)
	require.NoError(t, err)

	return postgresContainer, pool
}

func TestIntegration_DataPipeline(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	container, pool := setupTestDatabase(t)
	t.Cleanup(func() {
		pool.Close()
		_ = container.Terminate(context.Background())
	})

	ctx := context.Background()
	engine := NewEngine(pool, WithEngineCancelInterval(time.Minute))
	defer engine.Shutdown()

	// Register handlers
	engine.RegisterHandler(&DataExtractorHandler{})
	engine.RegisterHandler(&DataValidatorHandler{})
	engine.RegisterHandler(&DataTransformerHandler{})
	engine.RegisterHandler(&DataAggregatorHandler{})
	engine.RegisterHandler(&ReportGeneratorHandler{})

	// Create workflow
	workflowDef, err := NewBuilder("data-pipeline", 1).
		Fork("extract-data",
			func(branch *Builder) {
				branch.Step("extract-source1", "data-extractor", WithStepMaxRetries(2))
			},
			func(branch *Builder) {
				branch.Step("extract-source2", "data-extractor", WithStepMaxRetries(2))
			},
			func(branch *Builder) {
				branch.Step("extract-source3", "data-extractor", WithStepMaxRetries(2))
			},
		).
		JoinStep("join-data", []string{"extract-source1", "extract-source2", "extract-source3"}, JoinStrategyAll).
		Then("validate-data", "data-validator", WithStepMaxRetries(1)).
		Then("transform-data", "data-transformer", WithStepMaxRetries(2)).
		Then("aggregate-data", "data-aggregator", WithStepMaxRetries(2)).
		Then("generate-report", "report-generator", WithStepMaxRetries(1)).
		Build()

	require.NoError(t, err)

	// Register workflow
	err = engine.RegisterWorkflow(ctx, workflowDef)
	require.NoError(t, err)

	viz := NewVisualizer()
	fmt.Println(viz.RenderGraph(workflowDef))

	// Start workflow
	input := json.RawMessage(`{"sources": ["source1", "source2", "source3"]}`)
	instanceID, err := engine.Start(ctx, "data-pipeline-v1", input)
	require.NoError(t, err)
	assert.Greater(t, instanceID, int64(0))

	// Process workflow
	for i := 0; i < 50; i++ {
		empty, err := engine.ExecuteNext(ctx, "worker1")
		if err != nil {
			t.Logf("ExecuteNext error: %v", err)
		}
		if empty {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Check final status
	status, err := engine.GetStatus(ctx, instanceID)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, status)

	// Check steps
	steps, err := engine.GetSteps(ctx, instanceID)
	require.NoError(t, err)
	assert.Greater(t, len(steps), 0)

	// Verify all steps are completed
	for _, step := range steps {
		assert.Equal(t, StepStatusCompleted, step.Status)
	}
}

func TestIntegration_Ecommerce(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	container, pool := setupTestDatabase(t)
	t.Cleanup(func() {
		pool.Close()
		_ = container.Terminate(context.Background())
	})

	ctx := context.Background()
	engine := NewEngine(pool, WithEngineCancelInterval(time.Minute))
	defer engine.Shutdown()

	// Register handlers
	engine.RegisterHandler(&PaymentHandler{})
	engine.RegisterHandler(&InventoryHandler{})
	engine.RegisterHandler(&ShippingHandler{})
	engine.RegisterHandler(&NotificationHandler{})
	engine.RegisterHandler(&RefundHandler{})

	// Create workflow
	workflowDef, err := NewBuilder("ecommerce-order", 1).
		Step("process-payment", "payment", WithStepMaxRetries(3)).
		OnFailure("send-payment-failure-notification", "notification",
			WithStepMaxRetries(1),
			WithStepMetadata(map[string]any{
				"message": "Payment failed!",
			})).
		Then("reserve-inventory", "inventory", WithStepMaxRetries(2)).
		Then("ship-order", "shipping", WithStepMaxRetries(2)).
		Then("send-success-notification", "notification",
			WithStepMaxRetries(1),
			WithStepMetadata(map[string]any{
				"message": "Order shipped successfully!",
			}),
		).Build()

	require.NoError(t, err)

	// Register workflow
	err = engine.RegisterWorkflow(ctx, workflowDef)
	require.NoError(t, err)

	viz := NewVisualizer()
	fmt.Println(viz.RenderGraph(workflowDef))

	// Start workflow
	order := map[string]any{
		"user_id": "user123",
		"amount":  500.0,
		"items":   []string{"item1", "item2"},
	}
	input, _ := json.Marshal(order)
	instanceID, err := engine.Start(ctx, "ecommerce-order-v1", input)
	require.NoError(t, err)
	assert.Greater(t, instanceID, int64(0))

	// Process workflow
	for i := 0; i < 50; i++ {
		empty, err := engine.ExecuteNext(ctx, "worker1")
		if err != nil {
			t.Logf("ExecuteNext error: %v", err)
		}
		if empty {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Check final status
	status, err := engine.GetStatus(ctx, instanceID)
	require.NoError(t, err)

	// Log the actual status for debugging
	t.Logf("Final workflow status: %s", status)

	// For ecommerce test, we expect it to complete successfully
	assert.Equal(t, StatusCompleted, status)
}

func TestIntegration_Microservices(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	container, pool := setupTestDatabase(t)
	t.Cleanup(func() {
		pool.Close()
		_ = container.Terminate(context.Background())
	})

	ctx := context.Background()
	engine := NewEngine(pool, WithEngineCancelInterval(time.Minute))
	defer engine.Shutdown()

	// Register handlers
	engine.RegisterHandler(&UserServiceHandler{})
	engine.RegisterHandler(&PaymentServiceHandler{})
	engine.RegisterHandler(&InventoryServiceHandler{})
	engine.RegisterHandler(&NotificationServiceHandler{})
	engine.RegisterHandler(&AnalyticsServiceHandler{})
	engine.RegisterHandler(&AuditServiceHandler{})
	engine.RegisterHandler(&CompensationHandler{})

	// Create workflow
	workflowDef, err := NewBuilder("microservices-orchestration", 1).
		Step("validate-user", "user-service", WithStepMaxRetries(3)).
		OnFailure("compensate-user-validation", "compensation",
			WithStepMaxRetries(1),
			WithStepMetadata(map[string]any{
				"action": "user_validation_failed",
				"reason": "user_validation_error",
			})).
		Fork("process-payment-and-inventory",
			func(branch *Builder) {
				branch.Step("process-payment", "payment-service", WithStepMaxRetries(3))
			},
			func(branch *Builder) {
				branch.Step("check-inventory", "inventory-service", WithStepMaxRetries(2))
			},
		).
		JoinStep("send-notifications", []string{"process-payment", "check-inventory"}, JoinStrategyAll).
		Fork("track-analytics",
			func(branch *Builder) {
				branch.Step("track-event", "analytics-service", WithStepMaxRetries(1))
			},
			func(branch *Builder) {
				branch.Step("audit-action", "audit-service", WithStepMaxRetries(1))
			},
		).
		JoinStep("finalize-order", []string{"track-event", "audit-action"}, JoinStrategyAll).
		Build()

	require.NoError(t, err)

	// Register workflow
	err = engine.RegisterWorkflow(ctx, workflowDef)
	require.NoError(t, err)

	viz := NewVisualizer()
	fmt.Println(viz.RenderGraph(workflowDef))

	// Start workflow
	order := map[string]any{
		"user_id": "user456",
		"amount":  750.0,
		"items":   []string{"item3", "item4"},
	}
	input, _ := json.Marshal(order)
	instanceID, err := engine.Start(ctx, "microservices-orchestration-v1", input)
	require.NoError(t, err)
	assert.Greater(t, instanceID, int64(0))

	// Process workflow with timeout
	timeout := time.After(30 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			t.Fatal("Test timeout: workflow did not complete in 30 seconds")
		case <-ticker.C:
			empty, err := engine.ExecuteNext(ctx, "worker1")
			if err != nil {
				t.Logf("ExecuteNext error: %v", err)
			}
			if empty {
				goto done
			}
		}
	}
done:

	// Check final status
	status, err := engine.GetStatus(ctx, instanceID)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, status)
}

func TestIntegration_SavePointDemo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	container, pool := setupTestDatabase(t)
	t.Cleanup(func() {
		pool.Close()
		_ = container.Terminate(context.Background())
	})

	ctx := context.Background()
	engine := NewEngine(pool, WithEngineCancelInterval(time.Minute))
	defer engine.Shutdown()

	// Register handlers
	engine.RegisterHandler(&PaymentHandler{})
	engine.RegisterHandler(&InventoryHandler{})
	engine.RegisterHandler(&ConditionalShippingHandler{})
	engine.RegisterHandler(&NotificationHandler{})
	engine.RegisterHandler(&CompensationHandler{})

	// Create workflow with SavePoint
	workflowDef, err := NewBuilder("savepoint-demo", 1).
		Step("process-payment", "payment", WithStepMaxRetries(2)).
		SavePoint("payment-checkpoint").
		Then("reserve-inventory", "inventory", WithStepMaxRetries(1)).
		Then("ship-order", "shipping", WithStepMaxRetries(1)).
		OnFailure("ship-order-failure", "compensation",
			WithStepMaxRetries(0), WithStepMetadata(map[string]any{
				"action": "return",
			}),
		).
		Then("send-success-notification", "notification",
			WithStepMaxRetries(1),
			WithStepMetadata(map[string]any{
				"message": "Order processed successfully!",
			}),
		).Build()

	require.NoError(t, err)

	// Register workflow
	err = engine.RegisterWorkflow(ctx, workflowDef)
	require.NoError(t, err)

	viz := NewVisualizer()
	fmt.Println(viz.RenderGraph(workflowDef))

	// Test successful order
	order1 := map[string]any{
		"user_id": "user123",
		"amount":  500.0,
		"items":   []string{"item1", "item2"},
	}
	input1, _ := json.Marshal(order1)
	instanceID1, err := engine.Start(ctx, "savepoint-demo-v1", input1)
	require.NoError(t, err)

	// Test failed order
	order2 := map[string]any{
		"user_id": "user456",
		"amount":  1500.0,
		"items":   []string{"item3", "item4"},
	}
	input2, _ := json.Marshal(order2)
	instanceID2, err := engine.Start(ctx, "savepoint-demo-v1", input2)
	require.NoError(t, err)

	// Process workflows
	for range 100 {
		empty, err := engine.ExecuteNext(ctx, "worker1")
		if err != nil {
			t.Logf("ExecuteNext error: %v", err)
		}
		if empty {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Check final status
	status1, err := engine.GetStatus(ctx, instanceID1)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, status1)

	status2, err := engine.GetStatus(ctx, instanceID2)
	require.NoError(t, err)
	assert.Equal(t, StatusFailed, status2)
}

func TestIntegration_RollbackDemo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	container, pool := setupTestDatabase(t)
	t.Cleanup(func() {
		pool.Close()
		_ = container.Terminate(context.Background())
	})

	ctx := context.Background()
	engine := NewEngine(pool, WithEngineCancelInterval(time.Minute))
	defer engine.Shutdown()

	// Register handlers
	engine.RegisterHandler(&PaymentHandler{})
	engine.RegisterHandler(&InventoryHandler{})
	engine.RegisterHandler(&FailingShippingHandler{})
	engine.RegisterHandler(&NotificationHandler{})
	engine.RegisterHandler(&CompensationHandler{})

	// Create workflow with SavePoint and OnFailure handlers for all steps
	workflowDef, err := NewBuilder("rollback-demo", 1).
		Step("process-payment", "payment", WithStepMaxRetries(2)).
		OnFailure("refund-payment", "compensation",
			WithStepMaxRetries(1),
			WithStepMetadata(map[string]any{
				"action": "refund",
				"reason": "payment_failed",
			})).
		Then("reserve-inventory", "inventory", WithStepMaxRetries(1)).
		OnFailure("release-inventory", "compensation",
			WithStepMaxRetries(1),
			WithStepMetadata(map[string]any{
				"action": "release",
				"reason": "inventory_failed",
			})).
		Then("ship-order", "shipping", WithStepMaxRetries(1)).
		OnFailure("cancel-shipment", "compensation",
			WithStepMaxRetries(1),
			WithStepMetadata(map[string]any{
				"action": "cancel",
				"reason": "shipping_failed",
			})).
		Then("send-success-notification", "notification",
			WithStepMaxRetries(1),
			WithStepMetadata(map[string]any{
				"message": "Order processed successfully!",
			})).
		OnFailure("send-failure-notification", "notification",
			WithStepMaxRetries(1),
			WithStepMetadata(map[string]any{
				"message": "Order processing failed!",
			})).
		Build()

	require.NoError(t, err)

	// Register workflow
	err = engine.RegisterWorkflow(ctx, workflowDef)
	require.NoError(t, err)

	viz := NewVisualizer()
	fmt.Println(viz.RenderGraph(workflowDef))

	// Start workflow
	order := map[string]any{
		"user_id": "user789",
		"amount":  750.0,
		"items":   []string{"item5", "item6", "item7"},
	}
	input, _ := json.Marshal(order)
	instanceID, err := engine.Start(ctx, "rollback-demo-v1", input)
	require.NoError(t, err)
	assert.Greater(t, instanceID, int64(0))

	// Process workflow
	for i := 0; i < 100; i++ {
		empty, err := engine.ExecuteNext(ctx, "worker1")
		if err != nil {
			t.Logf("ExecuteNext error: %v", err)
		}
		if empty {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Check final status
	status, err := engine.GetStatus(ctx, instanceID)
	require.NoError(t, err)
	assert.Equal(t, StatusFailed, status)

	// Check steps
	steps, err := engine.GetSteps(ctx, instanceID)
	require.NoError(t, err)
	assert.Greater(t, len(steps), 0)

	// Verify rollback status
	hasRolledBack := false
	for _, step := range steps {
		if step.Status == StepStatusRolledBack {
			hasRolledBack = true
			break
		}
	}
	assert.True(t, hasRolledBack, "Expected at least one step to be rolled back")
}

func TestIntegration_Condition__true(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	container, pool := setupTestDatabase(t)
	t.Cleanup(func() {
		pool.Close()
		_ = container.Terminate(context.Background())
	})

	ctx := context.Background()
	engine := NewEngine(pool, WithEngineCancelInterval(time.Minute))
	defer engine.Shutdown()

	// Register handlers
	engine.RegisterHandler(&DataExtractorHandler{})
	engine.RegisterHandler(&NotificationHandler{})

	// Create workflow
	workflowDef, err := NewBuilder("condition", 1).
		Step("extract-data", "data-extractor", WithStepMaxRetries(1)).
		Condition("has_records", "{{ gt .count 0 }}", func(elseBranch *Builder) {
			elseBranch.Step("send-notification-no-records", "notification", WithStepMetadata(map[string]any{
				"message": "No records found!",
			}))
		}).
		Then("send-notification-has-records", "notification", WithStepMetadata(map[string]any{
			"message": "Records found!",
		})).
		Build()

	require.NoError(t, err)

	// Register workflow
	err = engine.RegisterWorkflow(ctx, workflowDef)
	require.NoError(t, err)

	viz := NewVisualizer()
	fmt.Println(viz.RenderGraph(workflowDef))

	// Start workflow
	input := json.RawMessage(`{"sources": ["source1", "source2", "source3"]}`)
	instanceID, err := engine.Start(ctx, "condition-v1", input)
	require.NoError(t, err)
	assert.Greater(t, instanceID, int64(0))

	// Process workflow
	for i := 0; i < 50; i++ {
		empty, err := engine.ExecuteNext(ctx, "worker1")
		if err != nil {
			t.Logf("ExecuteNext error: %v", err)
		}
		if empty {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Check final status
	status, err := engine.GetStatus(ctx, instanceID)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, status)

	// Check steps
	steps, err := engine.GetSteps(ctx, instanceID)
	require.NoError(t, err)
	assert.Greater(t, len(steps), 0)

	// Verify all steps are completed
	for _, step := range steps {
		assert.Equal(t, StepStatusCompleted, step.Status)
	}
}

func TestIntegration_Condition__false(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	container, pool := setupTestDatabase(t)
	t.Cleanup(func() {
		pool.Close()
		_ = container.Terminate(context.Background())
	})

	ctx := context.Background()
	engine := NewEngine(pool, WithEngineCancelInterval(time.Minute))
	defer engine.Shutdown()

	// Register handlers
	engine.RegisterHandler(&DataExtractorHandler{})
	engine.RegisterHandler(&NotificationHandler{})

	// Create workflow
	workflowDef, err := NewBuilder("condition", 1).
		Step("extract-data", "data-extractor", WithStepMaxRetries(1)).
		Condition("has_records", "{{ eq .count 0 }}", func(elseBranch *Builder) {
			elseBranch.Step("send-notification-no-records", "notification", WithStepMetadata(map[string]any{
				"message": "No records found!",
			}))
		}).
		Then("send-notification-has-records", "notification", WithStepMetadata(map[string]any{
			"message": "Records found!",
		})).
		Build()

	require.NoError(t, err)

	// Register workflow
	err = engine.RegisterWorkflow(ctx, workflowDef)
	require.NoError(t, err)

	viz := NewVisualizer()
	fmt.Println(viz.RenderGraph(workflowDef))

	// Start workflow
	input := json.RawMessage(`{"sources": ["source1", "source2", "source3"]}`)
	instanceID, err := engine.Start(ctx, "condition-v1", input)
	require.NoError(t, err)
	assert.Greater(t, instanceID, int64(0))

	// Process workflow
	for i := 0; i < 50; i++ {
		empty, err := engine.ExecuteNext(ctx, "worker1")
		if err != nil {
			t.Logf("ExecuteNext error: %v", err)
		}
		if empty {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Check final status
	status, err := engine.GetStatus(ctx, instanceID)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, status)

	// Check steps
	steps, err := engine.GetSteps(ctx, instanceID)
	require.NoError(t, err)
	assert.Greater(t, len(steps), 0)

	// Verify all steps are completed
	for _, step := range steps {
		assert.Equal(t, StepStatusCompleted, step.Status)
	}
}

func TestIntegration_Condition_Logic(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	container, pool := setupTestDatabase(t)
	t.Cleanup(func() {
		pool.Close()
		_ = container.Terminate(context.Background())
	})

	ctx := context.Background()
	engine := NewEngine(pool, WithEngineCancelInterval(time.Minute))
	defer engine.Shutdown()

	// Register handlers
	engine.RegisterHandler(&TestConditionHandler{})

	// Test case 1: condition should be true (count > 0)
	t.Run("condition_true", func(t *testing.T) {
		workflowDef, err := NewBuilder("condition_test", 1).
			Step("check", "test-condition", WithStepMaxRetries(1)).
			Condition("is_positive", "{{ gt .count 0 }}", func(elseBranch *Builder) {
				elseBranch.Step("negative", "test-condition", WithStepMetadata(map[string]any{
					"message": "negative",
				}))
			}).
			Then("positive", "test-condition", WithStepMetadata(map[string]any{
				"message": "positive",
			})).
			Build()

		require.NoError(t, err)
		err = engine.RegisterWorkflow(ctx, workflowDef)
		require.NoError(t, err)

		// Start with count = 5 (should be true)
		input := json.RawMessage(`{"count": 5}`)
		instanceID, err := engine.Start(ctx, "condition_test-v1", input)
		require.NoError(t, err)

		// Process workflow
		for i := 0; i < 10; i++ {
			empty, err := engine.ExecuteNext(ctx, "worker1")
			require.NoError(t, err)
			if empty {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}

		// Check steps
		steps, err := engine.GetSteps(ctx, instanceID)
		require.NoError(t, err)

		// Should have: check, is_positive, positive
		// Should NOT have: negative
		stepNames := make([]string, len(steps))
		for i, step := range steps {
			stepNames[i] = step.StepName
		}

		assert.Contains(t, stepNames, "check")
		assert.Contains(t, stepNames, "is_positive")
		assert.Contains(t, stepNames, "positive")
		assert.NotContains(t, stepNames, "negative")
	})

	// Test case 2: condition should be false (count = 0)
	t.Run("condition_false", func(t *testing.T) {
		workflowDef, err := NewBuilder("condition_test", 2).
			Step("check", "test-condition", WithStepMaxRetries(1)).
			Condition("is_positive", "{{ gt .count 0 }}", func(elseBranch *Builder) {
				elseBranch.Step("negative", "test-condition", WithStepMetadata(map[string]any{
					"message": "negative",
				}))
			}).
			Then("positive", "test-condition", WithStepMetadata(map[string]any{
				"message": "positive",
			})).
			Build()

		require.NoError(t, err)
		err = engine.RegisterWorkflow(ctx, workflowDef)
		require.NoError(t, err)

		// Start with count = 0 (should be false)
		input := json.RawMessage(`{"count": 0}`)
		instanceID, err := engine.Start(ctx, "condition_test-v2", input)
		require.NoError(t, err)

		// Process workflow
		for i := 0; i < 10; i++ {
			empty, err := engine.ExecuteNext(ctx, "worker1")
			require.NoError(t, err)
			if empty {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}

		// Check steps
		steps, err := engine.GetSteps(ctx, instanceID)
		require.NoError(t, err)

		// Should have: check, is_positive, negative
		// Should NOT have: positive
		stepNames := make([]string, len(steps))
		for i, step := range steps {
			stepNames[i] = step.StepName
		}

		assert.Contains(t, stepNames, "check")
		assert.Contains(t, stepNames, "is_positive")
		assert.Contains(t, stepNames, "negative")
		assert.NotContains(t, stepNames, "positive")
	})
}

// Handler implementations for integration tests

type TestConditionHandler struct{}

func (h *TestConditionHandler) Name() string { return "test-condition" }

func (h *TestConditionHandler) Execute(ctx context.Context, stepCtx StepContext, input json.RawMessage) (json.RawMessage, error) {
	// Just pass through the input
	return input, nil
}

type DataExtractorHandler struct{}

func (h *DataExtractorHandler) Name() string { return "data-extractor" }

func (h *DataExtractorHandler) Execute(ctx context.Context, stepCtx StepContext, input json.RawMessage) (json.RawMessage, error) {
	var data map[string]any
	_ = json.Unmarshal(input, &data)

	// Simulate data extraction
	records := []map[string]any{
		{"id": 1, "value": 100.0, "timestamp": time.Now().Unix()},
		{"id": 2, "value": 200.0, "timestamp": time.Now().Unix()},
	}

	result := map[string]any{
		"records": records,
		"source":  "extracted",
		"count":   len(records),
	}

	return json.Marshal(result)
}

type DataValidatorHandler struct{}

func (h *DataValidatorHandler) Name() string { return "data-validator" }

func (h *DataValidatorHandler) Execute(ctx context.Context, stepCtx StepContext, input json.RawMessage) (json.RawMessage, error) {
	var data map[string]any
	_ = json.Unmarshal(input, &data)

	validRecords := make([]map[string]any, 0)
	invalidRecords := make([]map[string]any, 0)

	outputs := data["outputs"].(map[string]any)
	for _, output := range outputs {
		outputObj := output.(map[string]any)
		records := outputObj["records"].([]any)
		for _, record := range records {
			rec := record.(map[string]any)
			value := rec["value"].(float64)
			if value > 0 && value < 1000 {
				validRecords = append(validRecords, rec)
			} else {
				invalidRecords = append(invalidRecords, rec)
			}
		}
	}

	result := map[string]any{
		"valid_records":   validRecords,
		"invalid_records": invalidRecords,
		"validation":      "completed",
		"outputs":         outputs,
	}

	return json.Marshal(result)
}

type DataTransformerHandler struct{}

func (h *DataTransformerHandler) Name() string { return "data-transformer" }

func (h *DataTransformerHandler) Execute(ctx context.Context, stepCtx StepContext, input json.RawMessage) (json.RawMessage, error) {
	var data map[string]any
	_ = json.Unmarshal(input, &data)

	validRecords := data["valid_records"].([]any)
	transformedRecords := make([]map[string]any, 0)

	for _, record := range validRecords {
		rec := record.(map[string]any)
		transformed := map[string]any{
			"id":        rec["id"],
			"value":     rec["value"].(float64) * 1.1, // Apply transformation
			"timestamp": rec["timestamp"],
			"processed": true,
		}
		transformedRecords = append(transformedRecords, transformed)
	}

	result := map[string]any{
		"transformed_records": transformedRecords,
		"transformation":      "completed",
		"valid_records":       validRecords,
	}

	return json.Marshal(result)
}

type DataAggregatorHandler struct{}

func (h *DataAggregatorHandler) Name() string { return "data-aggregator" }

func (h *DataAggregatorHandler) Execute(ctx context.Context, stepCtx StepContext, input json.RawMessage) (json.RawMessage, error) {
	var data map[string]any
	_ = json.Unmarshal(input, &data)

	transformedRecords := data["transformed_records"].([]any)
	totalValue := 0.0
	recordCount := len(transformedRecords)

	for _, record := range transformedRecords {
		rec := record.(map[string]any)
		totalValue += rec["value"].(float64)
	}

	avgValue := totalValue / float64(recordCount)

	result := map[string]any{
		"total_value":         totalValue,
		"average_value":       avgValue,
		"record_count":        recordCount,
		"aggregation":         "completed",
		"transformed_records": transformedRecords,
	}

	return json.Marshal(result)
}

type ReportGeneratorHandler struct{}

func (h *ReportGeneratorHandler) Name() string { return "report-generator" }

func (h *ReportGeneratorHandler) Execute(ctx context.Context, stepCtx StepContext, input json.RawMessage) (json.RawMessage, error) {
	var data map[string]any
	_ = json.Unmarshal(input, &data)

	report := map[string]any{
		"report_id":     fmt.Sprintf("report_%d", time.Now().Unix()),
		"total_value":   data["total_value"],
		"average_value": data["average_value"],
		"record_count":  data["record_count"],
		"generated_at":  time.Now().Unix(),
		"status":        "completed",
	}

	return json.Marshal(report)
}

type PaymentHandler struct{}

func (h *PaymentHandler) Name() string { return "payment" }

func (h *PaymentHandler) Execute(ctx context.Context, stepCtx StepContext, input json.RawMessage) (json.RawMessage, error) {
	var order map[string]any
	_ = json.Unmarshal(input, &order)

	amount := order["amount"].(float64)
	userID := order["user_id"].(string)

	result := map[string]any{
		"transaction_id": fmt.Sprintf("txn_%d", time.Now().Unix()),
		"amount":         amount,
		"user_id":        userID,
		"status":         "completed",
		"order":          order,
	}

	return json.Marshal(result)
}

type InventoryHandler struct{}

func (h *InventoryHandler) Name() string { return "inventory" }

func (h *InventoryHandler) Execute(ctx context.Context, stepCtx StepContext, input json.RawMessage) (json.RawMessage, error) {
	var payment map[string]any
	_ = json.Unmarshal(input, &payment)

	order := payment["order"].(map[string]any)
	items := order["items"].([]any)

	result := map[string]any{
		"reserved_items": items,
		"status":         "reserved",
		"timestamp":      time.Now().Unix(),
		"order":          order,
		"payment":        payment,
	}

	return json.Marshal(result)
}

type ShippingHandler struct{}

func (h *ShippingHandler) Name() string { return "shipping" }

func (h *ShippingHandler) Execute(ctx context.Context, stepCtx StepContext, input json.RawMessage) (json.RawMessage, error) {
	var inventory map[string]any
	_ = json.Unmarshal(input, &inventory)

	result := map[string]any{
		"tracking_number": fmt.Sprintf("TRK_%d", time.Now().Unix()),
		"status":          "shipped",
		"timestamp":       time.Now().Unix(),
	}

	return json.Marshal(result)
}

type FailingShippingHandler struct{}

func (h *FailingShippingHandler) Name() string { return "shipping" }

func (h *FailingShippingHandler) Execute(ctx context.Context, stepCtx StepContext, input json.RawMessage) (json.RawMessage, error) {
	// Force failure for rollback demo
	return nil, fmt.Errorf("shipping service unavailable")
}

type ConditionalShippingHandler struct{}

func (h *ConditionalShippingHandler) Name() string { return "shipping" }

func (h *ConditionalShippingHandler) Execute(ctx context.Context, stepCtx StepContext, input json.RawMessage) (json.RawMessage, error) {
	var inventory map[string]any
	_ = json.Unmarshal(input, &inventory)

	order := inventory["order"].(map[string]any)
	amount := order["amount"].(float64)

	// Fail if amount is too high (for savepoint demo)
	if amount > 1000.0 {
		return nil, fmt.Errorf("shipping service unavailable for high-value orders")
	}

	result := map[string]any{
		"tracking_number": fmt.Sprintf("TRK_%d", time.Now().Unix()),
		"status":          "shipped",
		"timestamp":       time.Now().Unix(),
	}

	return json.Marshal(result)
}

type NotificationHandler struct{}

func (h *NotificationHandler) Name() string { return "notification" }

func (h *NotificationHandler) Execute(ctx context.Context, stepCtx StepContext, input json.RawMessage) (json.RawMessage, error) {
	message, _ := stepCtx.GetVariableAsString("message")

	result := map[string]any{
		"status":  "sent",
		"message": message,
	}

	return json.Marshal(result)
}

type RefundHandler struct{}

func (h *RefundHandler) Name() string { return "refund" }

func (h *RefundHandler) Execute(ctx context.Context, stepCtx StepContext, input json.RawMessage) (json.RawMessage, error) {
	result := map[string]any{
		"status": "refunded",
		"action": "refund",
	}

	return json.Marshal(result)
}

type UserServiceHandler struct{}

func (h *UserServiceHandler) Name() string { return "user-service" }

func (h *UserServiceHandler) Execute(ctx context.Context, stepCtx StepContext, input json.RawMessage) (json.RawMessage, error) {
	var order map[string]any
	_ = json.Unmarshal(input, &order)

	userID := order["user_id"].(string)

	result := map[string]any{
		"user_id":   userID,
		"status":    "validated",
		"service":   "user-service",
		"timestamp": time.Now().Unix(),
		"order":     order,
	}

	return json.Marshal(result)
}

type PaymentServiceHandler struct{}

func (h *PaymentServiceHandler) Name() string { return "payment-service" }

func (h *PaymentServiceHandler) Execute(ctx context.Context, stepCtx StepContext, input json.RawMessage) (json.RawMessage, error) {
	var data map[string]any
	_ = json.Unmarshal(input, &data)

	// Handle different input structures
	var order map[string]any
	var user map[string]any
	var amount float64
	var userID string

	if orderData, ok := data["order"].(map[string]any); ok {
		// Input has order and user structure
		order = orderData
		if userData, ok := data["user"].(map[string]any); ok {
			user = userData
		}
	} else {
		// Input is directly the order
		order = data
	}

	amount, ok := order["amount"].(float64)
	if !ok {
		return nil, fmt.Errorf("amount not found in order")
	}

	userID, ok = order["user_id"].(string)
	if !ok {
		return nil, fmt.Errorf("user_id not found in order")
	}

	result := map[string]any{
		"transaction_id": fmt.Sprintf("txn_%d", time.Now().Unix()),
		"amount":         amount,
		"user_id":        userID,
		"status":         "completed",
		"service":        "payment-service",
		"timestamp":      time.Now().Unix(),
		"order":          order,
	}

	if user != nil {
		result["user"] = user
	}

	return json.Marshal(result)
}

type InventoryServiceHandler struct{}

func (h *InventoryServiceHandler) Name() string { return "inventory-service" }

func (h *InventoryServiceHandler) Execute(ctx context.Context, stepCtx StepContext, input json.RawMessage) (json.RawMessage, error) {
	var data map[string]any
	_ = json.Unmarshal(input, &data)

	// Handle different input structures
	var order map[string]any
	var user map[string]any

	if orderData, ok := data["order"].(map[string]any); ok {
		// Input has order and user structure
		order = orderData
		if userData, ok := data["user"].(map[string]any); ok {
			user = userData
		}
	} else {
		// Input is directly the order
		order = data
	}

	items, ok := order["items"].([]any)
	if !ok {
		return nil, fmt.Errorf("items not found in order")
	}

	result := map[string]any{
		"reserved_items": items,
		"status":         "reserved",
		"service":        "inventory-service",
		"timestamp":      time.Now().Unix(),
		"order":          order,
	}

	if user != nil {
		result["user"] = user
	}

	return json.Marshal(result)
}

type NotificationServiceHandler struct{}

func (h *NotificationServiceHandler) Name() string { return "notification-service" }

func (h *NotificationServiceHandler) Execute(ctx context.Context, stepCtx StepContext, input json.RawMessage) (json.RawMessage, error) {
	result := map[string]any{
		"status":  "sent",
		"service": "notification-service",
	}

	return json.Marshal(result)
}

type AnalyticsServiceHandler struct{}

func (h *AnalyticsServiceHandler) Name() string { return "analytics-service" }

func (h *AnalyticsServiceHandler) Execute(ctx context.Context, stepCtx StepContext, input json.RawMessage) (json.RawMessage, error) {
	result := map[string]any{
		"status":  "tracked",
		"service": "analytics-service",
	}

	return json.Marshal(result)
}

type AuditServiceHandler struct{}

func (h *AuditServiceHandler) Name() string { return "audit-service" }

func (h *AuditServiceHandler) Execute(ctx context.Context, stepCtx StepContext, input json.RawMessage) (json.RawMessage, error) {
	result := map[string]any{
		"status":  "audited",
		"service": "audit-service",
	}

	return json.Marshal(result)
}

type CompensationHandler struct{}

func (h *CompensationHandler) Name() string { return "compensation" }

func (h *CompensationHandler) Execute(ctx context.Context, stepCtx StepContext, input json.RawMessage) (json.RawMessage, error) {
	action, _ := stepCtx.GetVariableAsString("action")
	reason, _ := stepCtx.GetVariableAsString("reason")

	result := map[string]any{
		"status": "compensated",
		"action": action,
		"reason": reason,
	}

	return json.Marshal(result)
}
