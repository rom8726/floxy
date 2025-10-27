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

type DocumentProcessorHandler struct{}

func (h *DocumentProcessorHandler) Name() string {
	return "document-processor"
}

func (h *DocumentProcessorHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
	var data map[string]any
	if err := json.Unmarshal(input, &data); err != nil {
		return nil, err
	}

	documentID := data["document_id"].(string)
	documentType := data["document_type"].(string)

	log.Printf("Processing document %s of type %s", documentID, documentType)

	// Simulate document processing
	time.Sleep(100 * time.Millisecond)

	result := map[string]any{
		"document_id":   documentID,
		"document_type": documentType,
		"status":        "processed",
		"processed_at":  time.Now().Unix(),
		"metadata": map[string]any{
			"pages":     10,
			"file_size": "2.5MB",
			"format":    "PDF",
		},
	}

	return json.Marshal(result)
}

type ApprovalHandler struct{}

func (h *ApprovalHandler) Name() string {
	return "approval"
}

func (h *ApprovalHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
	var data map[string]any
	if err := json.Unmarshal(input, &data); err != nil {
		return nil, err
	}

	documentID := data["document_id"].(string)
	documentType := data["document_type"].(string)

	log.Printf("Document %s (%s) approved for publication", documentID, documentType)

	result := map[string]any{
		"document_id":   documentID,
		"document_type": documentType,
		"status":        "approved",
		"approved_at":   time.Now().Unix(),
		"approved_by":   "system",
	}

	return json.Marshal(result)
}

type RejectionHandler struct{}

func (h *RejectionHandler) Name() string {
	return "rejection"
}

func (h *RejectionHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
	var data map[string]any
	if err := json.Unmarshal(input, &data); err != nil {
		return nil, err
	}

	documentID := data["document_id"].(string)
	documentType := data["document_type"].(string)

	log.Printf("Document %s (%s) rejected and archived", documentID, documentType)

	result := map[string]any{
		"document_id":   documentID,
		"document_type": documentType,
		"status":        "rejected",
		"rejected_at":   time.Now().Unix(),
		"reason":        "human_rejection",
	}

	return json.Marshal(result)
}

type NotificationHandler struct{}

func (h *NotificationHandler) Name() string {
	return "notification"
}

func (h *NotificationHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
	var data map[string]any
	if err := json.Unmarshal(input, &data); err != nil {
		return nil, err
	}

	documentID := data["document_id"].(string)
	status := data["status"].(string)

	message, ok := stepCtx.GetVariableAsString("message")
	if !ok {
		message = "Document status updated"
	}

	log.Printf("Notification sent for document %s: %s (status: %s)", documentID, message, status)

	result := map[string]any{
		"document_id": documentID,
		"message":     message,
		"status":      status,
		"sent_at":     time.Now().Unix(),
	}

	return json.Marshal(result)
}

func main() {
	ctx := context.Background()

	pool, err := pgxpool.New(context.Background(), "postgres://floxy:password@localhost:5435/floxy?sslmode=disable")
	if err != nil {
		log.Fatalf("Failed to create connection pool: %v", err)
	}
	defer pool.Close()

	engine := floxy.NewEngine(pool)
	defer engine.Shutdown()

	// Run migrations
	if err := floxy.RunMigrations(ctx, pool); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	engine.RegisterHandler(&DocumentProcessorHandler{})
	engine.RegisterHandler(&ApprovalHandler{})
	engine.RegisterHandler(&RejectionHandler{})
	engine.RegisterHandler(&NotificationHandler{})

	workflowDef, err := floxy.NewBuilder("document-approval", 1).
		Step("process-document", "document-processor", floxy.WithStepMaxRetries(2)).
		WaitHumanConfirm("human-approval", floxy.WithStepDelay(time.Second)).
		Then("approve-document", "approval").
		Then("send-approval-notification", "notification",
			floxy.WithStepMaxRetries(1),
			floxy.WithStepMetadata(map[string]any{
				"message": "Document has been approved and published!",
			})).
		OnFailure("send-rejection-notification", "notification",
			floxy.WithStepMaxRetries(1),
			floxy.WithStepMetadata(map[string]any{
				"message": "Document has been rejected and archived.",
			})).
		Build()

	if err != nil {
		log.Fatalf("Failed to build workflow: %v", err)
	}

	if err := engine.RegisterWorkflow(context.Background(), workflowDef); err != nil {
		log.Fatalf("Failed to register workflow: %v", err)
	}

	workerPool := floxy.NewWorkerPool(engine, 3, 500*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	workerPool.Start(ctx)

	go func() {
		decisionWaitCh := engine.HumanDecisionWaitingEvents()
		for event := range decisionWaitCh {
			fmt.Printf("Wait decision event: %s\n", string(event.OutputData))
		}
	}()

	// Create document for processing
	document := map[string]any{
		"document_id":   "DOC-2024-001",
		"document_type": "contract",
		"title":         "Service Agreement",
		"author":        "legal@company.com",
		"submitted_at":  time.Now().Unix(),
		"priority":      "high",
	}

	documentJSON, _ := json.Marshal(document)
	instanceID, err := engine.Start(context.Background(), "document-approval-v1", documentJSON)
	if err != nil {
		log.Fatalf("Failed to start workflow: %v", err)
	}

	log.Printf("Started document approval workflow instance: %d", instanceID)

	// Wait for the workflow to process the document and reach human approval step
	log.Println("Waiting for workflow to reach human approval step...")

	humanStepIDCh := make(chan int64)

	go func() {
		// Simulate human decision
		select {
		case humanStepID := <-humanStepIDCh:
			time.Sleep(time.Second)
			decision := floxy.HumanDecisionConfirmed
			decidedBy := "manager@company.com"
			comment := "Document reviewed and approved for publication"

			log.Printf("Making human decision: %s by %s", decision, decidedBy)
			log.Printf("Comment: %s", comment)

			err = engine.MakeHumanDecision(context.Background(), humanStepID, decidedBy, decision, &comment)
			if err != nil {
				log.Fatalf("Failed to make human decision: %v", err)
			}

			log.Printf("Human decision made successfully!")
		case <-ctx.Done():
			return
		}
	}()

	var humanStepID int64
	var humanStepIDSent bool
	maxAttempts := 10

attempts:
	for i := 0; i < maxAttempts; i++ {
		time.Sleep(time.Second)

		steps, err := engine.GetSteps(context.Background(), instanceID)
		if err != nil {
			log.Printf("Failed to get steps (attempt %d): %v", i+1, err)

			continue
		}

		for _, step := range steps {
			//log.Printf("Step: %s (%s) - Status: %s", step.StepName, step.StepType, step.Status)
			if step.StepName == "human-approval" && step.Status == floxy.StepStatusWaitingDecision && !humanStepIDSent {
				humanStepID = step.ID
				humanStepIDCh <- humanStepID
				humanStepIDSent = true
			}
			if step.StepName == "send-approval-notification" && step.Status == floxy.StepStatusCompleted {
				log.Printf("Notification sent successfully!")

				break attempts
			}
		}
	}

	//if humanStepID == 0 {
	//	log.Fatal("Human approval step not found or not waiting for decision after maximum attempts")
	//}

	log.Printf("Found human approval step ID: %d", humanStepID)

	workerPool.Stop()
	cancel()

	status, err := engine.GetStatus(context.Background(), instanceID)
	if err != nil {
		log.Printf("Failed to get status: %v", err)
	} else {
		log.Printf("Final status: %s", status)
	}

	// Show final steps
	finalSteps, err := engine.GetSteps(context.Background(), instanceID)
	if err != nil {
		log.Printf("Failed to get final steps: %v", err)
	} else {
		log.Println("Final step statuses:")
		for _, step := range finalSteps {
			log.Printf("  - %s (%s): %s", step.StepName, step.StepType, step.Status)
		}
	}
}
