//go:build integration

package postgres

import (
	"context"
	"os"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestIntegration_PostgreSQLTracing(t *testing.T) {
	dsn := os.Getenv("DATABASE_DSN")
	if dsn == "" {
		t.Fatal("DATABASE_DSN is not set")
	}

	previousProvider := otel.GetTracerProvider()
	spanRecorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previousProvider)
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Errorf("shutdown tracer provider: %v", err)
		}
	})

	pool, err := Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(pool.Close)

	spanCountBeforeQuery := len(spanRecorder.Ended())
	ctx, parentSpan := provider.Tracer("postgres-test").Start(context.Background(), "parent")
	defer parentSpan.End()

	if _, err := pool.Exec(ctx, "SELECT 42"); err != nil {
		t.Fatalf("Exec() error = %v", err)
	}

	var querySpan sdktrace.ReadOnlySpan
	for _, span := range spanRecorder.Ended()[spanCountBeforeQuery:] {
		if span.Name() == "SELECT" {
			querySpan = span
			break
		}
	}
	if querySpan == nil {
		t.Fatal("PostgreSQL SELECT span not found")
	}

	if got, want := querySpan.SpanContext().TraceID(), parentSpan.SpanContext().TraceID(); got != want {
		t.Errorf("trace ID = %s, want %s", got, want)
	}
	if got, want := querySpan.Parent().SpanID(), parentSpan.SpanContext().SpanID(); got != want {
		t.Errorf("parent span ID = %s, want %s", got, want)
	}
}
