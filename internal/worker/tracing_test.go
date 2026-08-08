package worker

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/xhrobj/gophprofile/internal/event"
)

func TestWorker_Tracing(t *testing.T) {
	previousProvider := otel.GetTracerProvider()
	previousPropagator := otel.GetTextMapPropagator()
	spanRecorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		otel.SetTracerProvider(previousProvider)
		otel.SetTextMapPropagator(previousPropagator)
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Errorf("shutdown tracer provider: %v", err)
		}
	})

	parentCtx, parentSpan := provider.Tracer("worker-test").Start(context.Background(), "send")
	headers := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(parentCtx, headers)

	uploadWorker := newTestWorker(
		&fakeRepository{claimResults: []claimResult{{claimed: true}}},
		&fakeStorage{content: []byte("original")},
		&fakeImageProcessor{thumbnails: testThumbnails()},
	)
	uploadDelivery := newFakeUploadedDelivery(t, testEvent())
	uploadDelivery.headers = headers
	if err := uploadWorker.handleDelivery(context.Background(), uploadDelivery); err != nil {
		t.Fatalf("handle uploaded delivery error = %v", err)
	}

	deleteWorker := newTestWorker(&fakeRepository{}, &fakeStorage{}, &fakeImageProcessor{})
	deleteDelivery := newFakeDeletedDelivery(t, testDeletedEvent())
	deleteDelivery.headers = headers
	if err := deleteWorker.handleDelivery(context.Background(), deleteDelivery); err != nil {
		t.Fatalf("handle deleted delivery error = %v", err)
	}

	parentSpan.End()
	spans := spanRecorder.Ended()

	uploadConsumerSpan := assertTracingSpan(
		t,
		spans,
		"process "+event.AvatarUploadedRoutingKey,
		parentSpan.SpanContext().SpanID(),
	)
	if uploadConsumerSpan.SpanKind() != trace.SpanKindConsumer {
		t.Errorf("uploaded consumer span kind = %v, want %v", uploadConsumerSpan.SpanKind(), trace.SpanKindConsumer)
	}
	assertTracingSpan(t, spans, "process uploaded avatar", uploadConsumerSpan.SpanContext().SpanID())

	deleteConsumerSpan := assertTracingSpan(
		t,
		spans,
		"process "+event.AvatarDeletedRoutingKey,
		parentSpan.SpanContext().SpanID(),
	)
	if deleteConsumerSpan.SpanKind() != trace.SpanKindConsumer {
		t.Errorf("deleted consumer span kind = %v, want %v", deleteConsumerSpan.SpanKind(), trace.SpanKindConsumer)
	}
	assertTracingSpan(t, spans, "process deleted avatar", deleteConsumerSpan.SpanContext().SpanID())
}

func assertTracingSpan(
	t *testing.T,
	spans []sdktrace.ReadOnlySpan,
	name string,
	parentID trace.SpanID,
) sdktrace.ReadOnlySpan {
	t.Helper()

	for _, span := range spans {
		if span.Name() != name {
			continue
		}
		if got := span.Parent().SpanID(); got != parentID {
			t.Errorf("%s parent span ID = %s, want %s", name, got, parentID)
		}

		return span
	}

	t.Fatalf("span %q not found", name)

	return nil
}
