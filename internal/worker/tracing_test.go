package worker

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestWorker_Tracing(t *testing.T) {
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

	ctx, parentSpan := provider.Tracer("worker-test").Start(context.Background(), "delivery")

	uploadWorker := newTestWorker(
		&fakeRepository{claimResults: []claimResult{{claimed: true}}},
		&fakeStorage{content: []byte("original")},
		&fakeImageProcessor{thumbnails: testThumbnails()},
	)
	if err := uploadWorker.handleDelivery(ctx, newFakeUploadedDelivery(t, testEvent())); err != nil {
		t.Fatalf("handle uploaded delivery error = %v", err)
	}

	deleteWorker := newTestWorker(&fakeRepository{}, &fakeStorage{}, &fakeImageProcessor{})
	if err := deleteWorker.handleDelivery(ctx, newFakeDeletedDelivery(t, testDeletedEvent())); err != nil {
		t.Fatalf("handle deleted delivery error = %v", err)
	}

	parentSpan.End()

	assertTracingChildSpan(t, spanRecorder.Ended(), "process uploaded avatar", parentSpan.SpanContext().SpanID())
	assertTracingChildSpan(t, spanRecorder.Ended(), "process deleted avatar", parentSpan.SpanContext().SpanID())
}

func assertTracingChildSpan(t *testing.T, spans []sdktrace.ReadOnlySpan, name string, parentID trace.SpanID) {
	t.Helper()

	for _, span := range spans {
		if span.Name() != name {
			continue
		}
		if got := span.Parent().SpanID(); got != parentID {
			t.Errorf("%s parent span ID = %s, want %s", name, got, parentID)
		}

		return
	}

	t.Errorf("span %q not found", name)
}
