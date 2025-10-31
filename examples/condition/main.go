package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rom8726/floxy"
)

type SimpleTestHandler struct{}

func (h *SimpleTestHandler) Name() string {
	return "simple-test"
}

func (h *SimpleTestHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
	var data map[string]any
	_ = json.Unmarshal(input, &data)

	fmt.Printf("Executing step: %s\n", stepCtx.StepName())

	// Add small delay for demonstration
	time.Sleep(100 * time.Millisecond)

	// Just pass data through
	return input, nil
}

// FailingHandler - handler that always fails
type FailingHandler struct{}

func (h *FailingHandler) Name() string {
	return "failing-handler"
}

func (h *FailingHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
	fmt.Printf("Executing step: %s (this step always fails)\n", stepCtx.StepName())
	return nil, fmt.Errorf("intentional error in step %s", stepCtx.StepName())
}

func main() {
	ctx := context.Background()

	// Connect to database
	pool, err := pgxpool.New(ctx, "postgres://floxy:password@localhost:5435/floxy?sslmode=disable")
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer pool.Close()

	// Run migrations
	if err := floxy.RunMigrations(ctx, pool); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	// Create engine
	engine := floxy.NewEngine(pool)
	defer engine.Shutdown()

	// Register handlers
	engine.RegisterHandler(&SimpleTestHandler{})
	engine.RegisterHandler(&FailingHandler{})

	// Create a workflow with conditions in parallel branches
	workflowDef, err := floxy.NewBuilder("condition-demo", 1).
		Step("start", "simple-test", floxy.WithStepMaxRetries(1)).
		Fork("parallel_branch", func(branch1 *floxy.Builder) {
			branch1.Step("branch1_step1", "simple-test", floxy.WithStepMaxRetries(1)).
				Condition("branch1_condition", "{{ gt .count 5 }}", func(elseBranch *floxy.Builder) {
					elseBranch.Step("branch1_else", "simple-test", floxy.WithStepMaxRetries(1))
				}).
				Then("branch1_next", "simple-test", floxy.WithStepMaxRetries(1))
		}, func(branch2 *floxy.Builder) {
			branch2.Step("branch2_step1", "simple-test", floxy.WithStepMaxRetries(1)).
				Condition("branch2_condition", "{{ lt .count 3 }}", func(elseBranch *floxy.Builder) {
					elseBranch.Step("branch2_else", "failing-handler", floxy.WithStepMaxRetries(1)) // This step will fail
				}).
				Then("branch2_next", "simple-test", floxy.WithStepMaxRetries(1))
		}).
		Join("join", floxy.JoinStrategyAll).
		Then("final", "simple-test", floxy.WithStepMaxRetries(1)).
		Build()

	if err != nil {
		log.Fatalf("Failed to create workflow: %v", err)
	}

	// Register workflow
	if err := engine.RegisterWorkflow(ctx, workflowDef); err != nil {
		log.Fatalf("Failed to register workflow: %v", err)
	}

	viz := floxy.NewVisualizer()
	fmt.Println(viz.RenderGraph(workflowDef))

	// Test case 1: count = 2
	// Expected behavior:
	// - branch1: count > 5 = false -> branch1_else will execute
	// - branch2: count < 3 = true -> branch2_next will execute
	fmt.Println("=== Test Case 1: count = 2 ===")
	input1 := map[string]any{
		"count": 2,
	}

	inputJSON1, _ := json.Marshal(input1)
	instanceID1, err := engine.Start(ctx, "condition-demo-v1", inputJSON1)
	if err != nil {
		log.Printf("Failed to start workflow: %v", err)
	} else {
		fmt.Printf("Started workflow instance: %d\n", instanceID1)
	}

	// Test case 2: count = 6
	// Expected behavior:
	// - branch1: count > 5 = true -> branch1_next will execute
	// - branch2: count < 3 = false -> branch2_else will execute (will fail)
	fmt.Println("\n=== Test Case 2: count = 6 ===")
	input2 := map[string]any{
		"count": 6,
	}

	inputJSON2, _ := json.Marshal(input2)
	instanceID2, err := engine.Start(ctx, "condition-demo-v1", inputJSON2)
	if err != nil {
		log.Printf("Failed to start workflow: %v", err)
	} else {
		fmt.Printf("Started workflow instance: %d\n", instanceID2)
	}

	// Process workflow until completion
	fmt.Println("\n=== Processing Workflow ===")
	for i := 0; i < 100; i++ {
		// Process workflow
		isEmpty, err := engine.ExecuteNext(ctx, "worker1")
		if err != nil {
			fmt.Printf("ExecuteNext: %v\n", err)
		}
		if isEmpty {
			break
		}

		time.Sleep(100 * time.Millisecond)
	}

	// Check final status
	fmt.Println("\n=== Final Status ===")

	// Check first instance (count = 2)
	if instanceID1 > 0 {
		status1, _ := engine.GetStatus(ctx, instanceID1)
		fmt.Printf("Instance %d (count=2) status: %s\n", instanceID1, status1)

		// Get step information
		steps1, err := engine.GetSteps(ctx, instanceID1)
		if err == nil {
			fmt.Println("\nExecuted steps for count=2:")
			for _, step := range steps1 {
				fmt.Printf("  - %s: %s\n", step.StepName, step.Status)
			}
		}
	}

	// Check the second instance (count = 6)
	if instanceID2 > 0 {
		status2, _ := engine.GetStatus(ctx, instanceID2)
		fmt.Printf("\nInstance %d (count=6) status: %s\n", instanceID2, status2)

		// Get step information
		steps2, err := engine.GetSteps(ctx, instanceID2)
		if err == nil {
			fmt.Println("\nExecuted steps for count=6:")
			for _, step := range steps2 {
				fmt.Printf("  - %s: %s\n", step.StepName, step.Status)
			}
		}
	}

	fmt.Println("\n=== Explanation ===")
	fmt.Println("With count = 2:")
	fmt.Println("- Branch 1: condition 'count > 5' = false -> branch1_else executes")
	fmt.Println("- Branch 2: condition 'count < 3' = true -> branch2_next executes")
	fmt.Println("- Both branches complete successfully")
	fmt.Println("- Join waits for both branches, then final executes")
	fmt.Println("\nWith count = 6:")
	fmt.Println("- Branch 1: condition 'count > 5' = true -> branch1_next executes")
	fmt.Println("- Branch 2: condition 'count < 3' = false -> branch2_else executes (fails)")
	fmt.Println("- Branch 2 fails, so the entire workflow fails")
	fmt.Println("- All steps are rolled back due to the failure")
	fmt.Println("- Join and final steps are not completed")
}
