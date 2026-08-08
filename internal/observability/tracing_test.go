package observability

import (
	"context"
	"errors"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.39.0"
	oteltrace "go.opentelemetry.io/otel/trace"
)

type recordingExporter struct {
	spans       []sdktrace.ReadOnlySpan
	shutdownErr error
	shutdown    bool
}

func (e *recordingExporter) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	e.spans = append(e.spans, spans...)
	return nil
}

func (e *recordingExporter) Shutdown(context.Context) error {
	e.shutdown = true
	return e.shutdownErr
}

func TestNewTracing_Disabled(t *testing.T) {
	tracing, err := NewTracing(context.Background(), "server", "", false)
	if err != nil {
		t.Fatalf("NewTracing() error = %v", err)
	}

	if err := tracing.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func TestNewTracing_Validation(t *testing.T) {
	tests := []struct {
		name        string
		serviceName string
		endpoint    string
		want        string
	}{
		{
			name:        "empty service name",
			serviceName: "  ",
			endpoint:    "http://localhost:4318",
			want:        "service name",
		},
		{
			name:        "missing endpoint scheme",
			serviceName: "server",
			endpoint:    "localhost:4318",
			want:        "OTLP endpoint",
		},
		{
			name:        "unsupported endpoint scheme",
			serviceName: "server",
			endpoint:    "ftp://localhost:4318",
			want:        "OTLP endpoint",
		},
		{
			name:        "endpoint with query",
			serviceName: "server",
			endpoint:    "http://localhost:4318?debug=true",
			want:        "OTLP endpoint",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewTracing(context.Background(), tt.serviceName, tt.endpoint, true)
			if err == nil {
				t.Fatal("NewTracing() error = nil, want validation error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("NewTracing() error = %q, want %q", err, tt.want)
			}
		})
	}
}

func TestTraceEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		want     string
	}{
		{name: "host endpoint", endpoint: "http://localhost:4318", want: "http://localhost:4318/v1/traces"},
		{name: "base path", endpoint: "https://collector.example/otel", want: "https://collector.example/otel/v1/traces"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := traceEndpoint(tt.endpoint)
			if err != nil {
				t.Fatalf("traceEndpoint() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("traceEndpoint() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTracing_DropsTelemetryNoise(t *testing.T) {
	preserveGlobals(t)

	res, err := traceResource("server")
	if err != nil {
		t.Fatalf("traceResource() error = %v", err)
	}

	exporter := &recordingExporter{}
	tracing := installTracing(res, exporter)
	tracer := otel.Tracer("test")

	for _, path := range []string{"/health", "/metrics"} {
		noiseCtx, noiseSpan := tracer.Start(
			context.Background(),
			"GET",
			oteltrace.WithAttributes(semconv.URLPath(path)),
		)
		_, noiseChild := tracer.Start(noiseCtx, "HTTP HEAD")
		noiseChild.End()
		noiseSpan.End()
	}

	requestCtx, requestSpan := tracer.Start(
		context.Background(),
		"POST",
		oteltrace.WithAttributes(semconv.URLPath("/api/v1/avatars")),
	)
	_, requestChild := tracer.Start(requestCtx, "INSERT")
	requestChild.End()
	requestSpan.End()

	if err := tracing.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	if len(exporter.spans) != 2 {
		t.Fatalf("exported spans = %d, want 2", len(exporter.spans))
	}

	for _, span := range exporter.spans {
		if span.Name() == "GET" || span.Name() == "HTTP HEAD" {
			t.Errorf("telemetry-noise span %q was exported", span.Name())
		}
	}
}

func TestTracing_ShutdownExportsSpan(t *testing.T) {
	preserveGlobals(t)

	res, err := traceResource("server")
	if err != nil {
		t.Fatalf("traceResource() error = %v", err)
	}

	exporter := &recordingExporter{}
	tracing := installTracing(res, exporter)

	ctx, span := otel.Tracer("test").Start(context.Background(), "request")
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	span.End()

	if carrier.Get("traceparent") == "" {
		t.Error("traceparent header is empty")
	}

	if err := tracing.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	if !exporter.shutdown {
		t.Error("exporter was not shut down")
	}
	if len(exporter.spans) != 1 {
		t.Fatalf("exported spans = %d, want 1", len(exporter.spans))
	}
	if got := serviceName(exporter.spans[0]); got != "server" {
		t.Errorf("service.name = %q, want %q", got, "server")
	}
}

func TestTracing_ShutdownReturnsExporterError(t *testing.T) {
	preserveGlobals(t)

	wantErr := errors.New("exporter shutdown failed")
	res, err := traceResource("server")
	if err != nil {
		t.Fatalf("traceResource() error = %v", err)
	}

	tracing := installTracing(res, &recordingExporter{shutdownErr: wantErr})

	err = tracing.Shutdown(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("Shutdown() error = %v, want %v", err, wantErr)
	}
}

func preserveGlobals(t *testing.T) {
	t.Helper()

	previousProvider := otel.GetTracerProvider()
	previousPropagator := otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(previousProvider)
		otel.SetTextMapPropagator(previousPropagator)
	})
}

func serviceName(span sdktrace.ReadOnlySpan) string {
	for _, attr := range span.Resource().Attributes() {
		if attr.Key == semconv.ServiceNameKey {
			return attr.Value.AsString()
		}
	}

	return ""
}
