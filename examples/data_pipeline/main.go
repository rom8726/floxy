package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rom8726/floxy"
)

type DataExtractorHandler struct{}

func (h *DataExtractorHandler) Name() string {
	return "data-extractor"
}

func (h *DataExtractorHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
	var config map[string]any
	if err := json.Unmarshal(input, &config); err != nil {
		return nil, err
	}

	source := config["source"].(string)

	time.Sleep(100 * time.Millisecond)

	records := make([]map[string]any, 0)
	for i := 0; i < 10; i++ {
		records = append(records, map[string]any{
			"id":     fmt.Sprintf("%s_%d", source, i),
			"value":  rand.Float64() * 100,
			"source": source,
		})
	}

	log.Printf("Extracted %d records from %s", len(records), source)

	result := map[string]any{
		"records":   records,
		"source":    source,
		"count":     len(records),
		"timestamp": time.Now().Unix(),
	}

	return json.Marshal(result)
}

type DataValidatorHandler struct{}

func (h *DataValidatorHandler) Name() string {
	return "data-validator"
}

func (h *DataValidatorHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
	var data map[string]any
	if err := json.Unmarshal(input, &data); err != nil {
		return nil, err
	}

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

	if len(validRecords) == 0 {
		return nil, fmt.Errorf("no valid records found")
	}

	log.Printf("Validated %d valid records, %d invalid records", len(validRecords), len(invalidRecords))

	result := map[string]any{
		"valid_records":   validRecords,
		"invalid_records": invalidRecords,
		"valid_count":     len(validRecords),
		"invalid_count":   len(invalidRecords),
		"timestamp":       time.Now().Unix(),
	}

	return json.Marshal(result)
}

type DataTransformerHandler struct{}

func (h *DataTransformerHandler) Name() string {
	return "data-transformer"
}

func (h *DataTransformerHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
	var data map[string]any
	if err := json.Unmarshal(input, &data); err != nil {
		return nil, err
	}

	validRecords := data["valid_records"].([]any)
	transformedRecords := make([]map[string]any, 0)

	for _, record := range validRecords {
		rec := record.(map[string]any)
		value := rec["value"].(float64)

		transformedRecord := map[string]any{
			"id":               rec["id"],
			"original_value":   value,
			"normalized_value": value / 100.0,
			"category":         getCategory(value),
			"processed_at":     time.Now().Unix(),
		}

		transformedRecords = append(transformedRecords, transformedRecord)
	}

	log.Printf("Transformed %d records", len(transformedRecords))

	result := map[string]any{
		"transformed_records": transformedRecords,
		"count":               len(transformedRecords),
		"timestamp":           time.Now().Unix(),
	}

	return json.Marshal(result)
}

type DataAggregatorHandler struct{}

func (h *DataAggregatorHandler) Name() string {
	return "data-aggregator"
}

func (h *DataAggregatorHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
	var data map[string]any
	if err := json.Unmarshal(input, &data); err != nil {
		return nil, err
	}

	allRecords := make([]map[string]any, 0)

	for key, value := range data {
		if key == "transformed_records" {
			if records, ok := value.([]any); ok {
				for _, record := range records {
					if rec, ok := record.(map[string]any); ok {
						allRecords = append(allRecords, rec)
					}
				}
			}
		}
	}

	if len(allRecords) == 0 {
		return nil, fmt.Errorf("no records to aggregate")
	}

	stats := calculateStats(allRecords)

	log.Printf("Aggregated %d records, avg: %.2f, min: %.2f, max: %.2f",
		len(allRecords), stats["average"], stats["minValue"], stats["maxValue"])

	result := map[string]any{
		"total_records": len(allRecords),
		"statistics":    stats,
		"timestamp":     time.Now().Unix(),
	}

	return json.Marshal(result)
}

type ReportGeneratorHandler struct{}

func (h *ReportGeneratorHandler) Name() string {
	return "report-generator"
}

func (h *ReportGeneratorHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
	var data map[string]any
	if err := json.Unmarshal(input, &data); err != nil {
		return nil, err
	}

	reportID := fmt.Sprintf("RPT_%d", time.Now().Unix())

	log.Printf("Generated report: %s", reportID)

	result := map[string]any{
		"report_id": reportID,
		"status":    "completed",
		"timestamp": time.Now().Unix(),
	}

	return json.Marshal(result)
}

type CompensationHandler struct{}

func (h *CompensationHandler) Name() string {
	return "compensation"
}

func (h *CompensationHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
	log.Printf("Compensation step for %q", stepCtx.StepName())

	return input, nil
}

func getCategory(value float64) string {
	switch {
	case value < 20:
		return "low"
	case value < 50:
		return "medium"
	case value < 80:
		return "high"
	default:
		return "very_high"
	}
}

func calculateStats(records []map[string]any) map[string]any {
	if len(records) == 0 {
		return map[string]any{
			"average":  0.0,
			"minValue": 0.0,
			"maxValue": 0.0,
		}
	}

	var sum float64
	minValue := records[0]["normalized_value"].(float64)
	maxValue := records[0]["normalized_value"].(float64)

	for _, record := range records {
		value := record["normalized_value"].(float64)
		sum += value
		if value < minValue {
			minValue = value
		}
		if value > maxValue {
			maxValue = value
		}
	}

	return map[string]any{
		"average":  sum / float64(len(records)),
		"minValue": minValue,
		"maxValue": maxValue,
	}
}

func main() {
	ctx := context.Background()
	pool, err := pgxpool.New(context.Background(), "postgres://floxy:password@localhost:5435/floxy?sslmode=disable")
	if err != nil {
		log.Fatalf("Failed to create connection pool: %v", err)
	}
	defer pool.Close()

	// Run migrations
	if err := floxy.RunMigrations(ctx, pool); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	engine := floxy.NewEngine(pool)
	defer engine.Shutdown()

	engine.RegisterHandler(&DataExtractorHandler{})
	engine.RegisterHandler(&DataValidatorHandler{})
	engine.RegisterHandler(&DataTransformerHandler{})
	engine.RegisterHandler(&DataAggregatorHandler{})
	engine.RegisterHandler(&ReportGeneratorHandler{})
	engine.RegisterHandler(&CompensationHandler{})

	workflowDef, err := floxy.NewBuilder("data-pipeline", 1).
		Fork("extract-data",
			func(branch *floxy.Builder) {
				branch.Step("extract-source1", "data-extractor", floxy.WithStepMaxRetries(2))
			},
			func(branch *floxy.Builder) {
				branch.Step("extract-source2", "data-extractor", floxy.WithStepMaxRetries(2))
			},
			func(branch *floxy.Builder) {
				branch.Step("extract-source3", "data-extractor", floxy.WithStepMaxRetries(2)).
					Then("extract-source4", "data-extractor", floxy.WithStepMaxRetries(1)).
					OnFailure("extract-source4-failure", "compensation", floxy.WithStepMaxRetries(1)).
					Then("extract-source5", "data-extractor", floxy.WithStepMaxRetries(1)).
					OnFailure("extract-source5-failure", "compensation", floxy.WithStepMaxRetries(1))
			},
		).
		JoinStep("join-data", []string{"extract-source1", "extract-source2", "extract-source3"}, floxy.JoinStrategyAll).
		Then("validate-data", "data-validator", floxy.WithStepMaxRetries(1)).
		Then("transform-data", "data-transformer", floxy.WithStepMaxRetries(2)).
		Then("aggregate-data", "data-aggregator", floxy.WithStepMaxRetries(2)).
		Then("generate-report", "report-generator", floxy.WithStepMaxRetries(1)).
		Build()

	if err != nil {
		log.Fatalf("Failed to build workflow: %v", err)
	}

	if err := engine.RegisterWorkflow(context.Background(), workflowDef); err != nil {
		log.Fatalf("Failed to register workflow: %v", err)
	}

	workerPool := floxy.NewWorkerPool(engine, 5, 200*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	workerPool.Start(ctx)

	configs := []map[string]any{
		{"source": "database", "table": "users"},
		//{"source": "api", "endpoint": "/api/data"},
		//{"source": "file", "path": "/data/input.csv"},
	}

	for i, config := range configs {
		configJSON, _ := json.Marshal(config)
		instanceID, err := engine.Start(context.Background(), "data-pipeline-v1", configJSON)
		if err != nil {
			log.Printf("Failed to start workflow %d: %v", i+1, err)
		} else {
			log.Printf("Started data pipeline workflow instance %d: %d", i+1, instanceID)
		}
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	<-sigCh
	log.Println("Shutting down...")

	workerPool.Stop()
	cancel()
}
