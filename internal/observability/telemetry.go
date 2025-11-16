package observability

import (
	"context"
	"fmt"
	"log"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

// Config holds observability configuration
type Config struct {
	ServiceName  string
	OTelEndpoint string
	Enabled      bool
}

// Setup initializes OpenTelemetry
func Setup(ctx context.Context, cfg *Config) (func(), error) {
	if !cfg.Enabled || cfg.OTelEndpoint == "" {
		log.Printf("OpenTelemetry disabled or no endpoint configured")
		return func() {}, nil
	}

	log.Printf("Initializing OpenTelemetry with endpoint: %s", cfg.OTelEndpoint)

	// Create resource
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(cfg.ServiceName),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create trace exporter
	traceExporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(cfg.OTelEndpoint),
		otlptracegrpc.WithInsecure(), // Use TLS in production
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create trace exporter: %w", err)
	}

	// Create trace provider
	traceProvider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(traceProvider)

	log.Printf("OpenTelemetry initialized successfully")

	// Return cleanup function
	cleanup := func() {
		log.Printf("Shutting down OpenTelemetry...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := traceProvider.Shutdown(ctx); err != nil {
			log.Printf("Error shutting down trace provider: %v", err)
		}
	}

	return cleanup, nil
}

// GetTracer returns a tracer for the given name
func GetTracer(name string) interface{} {
	return otel.Tracer(name)
}
