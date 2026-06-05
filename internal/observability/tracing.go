package observability

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// InitTracer configures the global OpenTelemetry TracerProvider and text-map propagator.
//
// It exports spans to an OTLP HTTP endpoint (e.g. Jaeger's collector at port 4318).
// If otlpEndpoint is empty, tracing is a no-op — spans are created but discarded.
//
// Returns a shutdown function that must be deferred in main() to flush pending spans
// before the process exits. Dropping this call loses the last batch of spans.
//
//	shutdown, err := observability.InitTracer(ctx, "payflow-api", "localhost:4318")
//	if err != nil { ... }
//	defer shutdown(ctx)
func InitTracer(ctx context.Context, serviceName, otlpEndpoint string) (func(context.Context) error, error) {
	// The resource describes this service to the tracing backend.
	// Jaeger and Grafana Tempo use service.name to group traces.
	//
	// resource.NewSchemaless avoids the "conflicting Schema URL" error that occurs
	// when our semconv import version differs from the one baked into resource.Default().
	// A schemaless resource has an empty URL which resource.Merge treats as compatible.
	res, err := resource.Merge(
		resource.Default(),
		resource.NewSchemaless(
			semconv.ServiceName(serviceName),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("tracing.InitTracer resource: %w", err)
	}

	var exporter sdktrace.SpanExporter

	if otlpEndpoint == "" {
		// No endpoint configured — use a no-op exporter so tracing calls compile
		// and work without panicking, but nothing is sent anywhere.
		exporter = &noopExporter{}
	} else {
		// OTLP/HTTP exporter — sends spans to Jaeger (or any OTLP-compatible backend).
		// Port 4318 is the OTLP HTTP standard; port 4317 is OTLP gRPC.
		exporter, err = otlptracehttp.New(ctx,
			otlptracehttp.WithEndpoint(otlpEndpoint),
			otlptracehttp.WithInsecure(), // no TLS in local dev
		)
		if err != nil {
			return nil, fmt.Errorf("tracing.InitTracer exporter: %w", err)
		}
	}

	// The BatchSpanProcessor buffers spans and sends them in batches — much more
	// efficient than sending each span synchronously as it would block every operation.
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		// AlwaysSample records 100% of traces. In production use
		// sdktrace.WithSampler(sdktrace.TraceIDRatioBased(0.1)) for 10%.
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	// Register as the global provider so otel.Tracer("...") works anywhere.
	otel.SetTracerProvider(tp)

	// The W3C TraceContext propagator encodes the trace context as HTTP headers
	// (traceparent, tracestate). This is the industry standard — supported by
	// every major vendor (Datadog, Jaeger, Zipkin, Tempo, etc.).
	// Baggage propagates user-defined key-value pairs alongside the trace.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp.Shutdown, nil
}

// noopExporter discards all spans. Used when no OTLP endpoint is configured.
type noopExporter struct{}

func (e *noopExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	return nil
}
func (e *noopExporter) Shutdown(ctx context.Context) error { return nil }
