package telemetry

import (
	"context"
	"log"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	"go.opentelemetry.io/otel/trace"
)

var (
	// Global tracer for the application
	Tracer trace.Tracer

	// Global meter for custom metrics
	Meter metric.Meter

	// Custom metrics
	BeadsTotal          metric.Int64UpDownCounter
	BeadsCompleted      metric.Int64Counter
	BeadsActive         metric.Int64UpDownCounter
	AgentIterations     metric.Int64Counter
	WorkflowsStarted    metric.Int64Counter
	WorkflowsCompleted  metric.Int64Counter
	DispatchLatency     metric.Float64Histogram
	AgentExecutionTime  metric.Float64Histogram
)

// InitTelemetry initializes OpenTelemetry tracing and metrics
func InitTelemetry(ctx context.Context, serviceName, otelEndpoint string) (func(context.Context) error, error) {
	// Create resource with service information
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion("1.0.0"),
			attribute.String("environment", "development"),
		),
	)
	if err != nil {
		return nil, err
	}

	// Create OTLP trace exporter
	traceExporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(otelEndpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, err
	}

	// Create trace provider
	traceProvider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	// Set global trace provider and propagator
	otel.SetTracerProvider(traceProvider)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	// Create global tracer
	Tracer = otel.Tracer(serviceName)

	// Create global meter
	Meter = otel.Meter(serviceName)

	// Initialize custom metrics
	if err := initMetrics(); err != nil {
		return nil, err
	}

	log.Printf("[Telemetry] Initialized with endpoint %s", otelEndpoint)

	// Return shutdown function
	return func(ctx context.Context) error {
		shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		return traceProvider.Shutdown(shutdownCtx)
	}, nil
}

// initMetrics creates all custom metrics
func initMetrics() error {
	var err error

	BeadsTotal, err = Meter.Int64UpDownCounter(
		"loom.beads.total",
		metric.WithDescription("Total number of beads in the system"),
	)
	if err != nil {
		return err
	}

	BeadsCompleted, err = Meter.Int64Counter(
		"loom.beads.completed",
		metric.WithDescription("Number of beads completed"),
	)
	if err != nil {
		return err
	}

	BeadsActive, err = Meter.Int64UpDownCounter(
		"loom.beads.active",
		metric.WithDescription("Number of beads currently being worked on"),
	)
	if err != nil {
		return err
	}

	AgentIterations, err = Meter.Int64Counter(
		"loom.agent.iterations",
		metric.WithDescription("Number of agent iterations"),
	)
	if err != nil {
		return err
	}

	WorkflowsStarted, err = Meter.Int64Counter(
		"loom.workflows.started",
		metric.WithDescription("Number of workflows started"),
	)
	if err != nil {
		return err
	}

	WorkflowsCompleted, err = Meter.Int64Counter(
		"loom.workflows.completed",
		metric.WithDescription("Number of workflows completed"),
	)
	if err != nil {
		return err
	}

	DispatchLatency, err = Meter.Float64Histogram(
		"loom.dispatch.latency",
		metric.WithDescription("Dispatch operation latency in milliseconds"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return err
	}

	AgentExecutionTime, err = Meter.Float64Histogram(
		"loom.agent.execution_time",
		metric.WithDescription("Agent execution time in milliseconds"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return err
	}

	return nil
}
