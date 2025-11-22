package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/rom8726/floxy"
	"github.com/rom8726/floxy/plugins/engine/telemetry"
)

type HelloHandler struct{}

func (h *HelloHandler) Name() string {
	return "hello"
}

func (h *HelloHandler) Execute(ctx context.Context, stepCtx floxy.StepContext, input json.RawMessage) (json.RawMessage, error) {
	var data map[string]interface{}
	if err := json.Unmarshal(input, &data); err != nil {
		return nil, err
	}

	name := "World"
	if n, ok := data["name"].(string); ok {
		name = n
	}

	message := fmt.Sprintf("Hello, %s!", name)
	log.Printf("Hello handler executed: %s. Idempotency key: %s", message, stepCtx.IdempotencyKey())

	result := map[string]interface{}{
		"message":   message,
		"timestamp": time.Now().Unix(),
	}

	return json.Marshal(result)
}

func initTracer() (func() error, error) {
	ctx := context.Background()

	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpoint("localhost:4318"),
		otlptracehttp.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("creating OTLP trace exporter: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			attribute.String("service.name", "floxy-example"),
			attribute.String("service.version", "1.0.0"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("creating resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return tp.Shutdown(ctx)
	}, nil
}

func main() {
	shutdown, err := initTracer()
	if err != nil {
		log.Fatalf("Failed to initialize tracer: %v", err)
	}
	defer func() {
		if err := shutdown(); err != nil {
			log.Printf("Failed to shutdown tracer: %v", err)
		}
	}()

	ctx := context.Background()
	pool, err := pgxpool.New(context.Background(), "postgres://floxy:password@localhost:5435/floxy?sslmode=disable")
	if err != nil {
		log.Fatalf("Failed to create connection pool: %v", err)
	}
	defer pool.Close()

	engine := floxy.NewEngine(pool)
	defer func() {
		if err := engine.Shutdown(); err != nil {
			log.Printf("Failed to shutdown engine: %v", err)
		}
	}()

	tracer := otel.Tracer("floxy")
	telemetryPlugin := telemetry.New(tracer)
	engine.RegisterPlugin(telemetryPlugin)

	if err := floxy.RunMigrations(ctx, pool); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	engine.RegisterHandler(&HelloHandler{})

	workflowDef, err := floxy.NewBuilder("hello-world", 1).
		Step("say-hello", "hello", floxy.WithStepMaxRetries(3)).
		Build()
	if err != nil {
		log.Fatalf("Failed to build workflow: %v", err)
	}

	if err := engine.RegisterWorkflow(context.Background(), workflowDef); err != nil {
		log.Fatalf("Failed to register workflow: %v", err)
	}

	workerPool := floxy.NewWorkerPool(engine, 2, 1*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	workerPool.Start(ctx)

	input := json.RawMessage(`{"name": "Floxy"}`)
	instanceID, err := engine.Start(context.Background(), "hello-world-v1", input)
	if err != nil {
		log.Fatalf("Failed to start workflow: %v", err)
	}

	log.Printf("Started workflow instance: %d", instanceID)
	log.Println("Traces are being sent to Jaeger. Open http://localhost:16686 to view them.")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	<-sigCh
	log.Println("Shutting down...")

	workerPool.Stop()
	cancel()

	status, err := engine.GetStatus(context.Background(), instanceID)
	if err != nil {
		log.Printf("Failed to get status: %v", err)
	} else {
		log.Printf("Final status: %s", status)
	}
}
