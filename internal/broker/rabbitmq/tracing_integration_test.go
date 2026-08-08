//go:build integration

package rabbitmq_test

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/xhrobj/gophprofile/internal/broker/rabbitmq"
	"github.com/xhrobj/gophprofile/internal/event"
	"github.com/xhrobj/gophprofile/internal/model"
)

func TestIntegration_RabbitMQTracePropagation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), rabbitMQIntegrationTestTimeout)
	t.Cleanup(cancel)

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

	url := requireEnv(t, "RABBITMQ_URL")
	exchange, queue := uniqueTopologyNames()
	t.Cleanup(func() {
		cleanupTopology(t, url, exchange, queue)
	})

	consumer, err := rabbitmq.OpenConsumer(url, exchange, queue)
	if err != nil {
		t.Fatalf("OpenConsumer() error = %v", err)
	}
	t.Cleanup(func() {
		if err := consumer.Close(); err != nil {
			t.Errorf("Consumer.Close() error = %v", err)
		}
	})

	publisher, err := rabbitmq.OpenPublisher(url, exchange, queue)
	if err != nil {
		t.Fatalf("OpenPublisher() error = %v", err)
	}
	t.Cleanup(func() {
		if err := publisher.Close(); err != nil {
			t.Errorf("Publisher.Close() error = %v", err)
		}
	})

	deliveries, err := consumer.Consume(ctx)
	if err != nil {
		t.Fatalf("Consume() error = %v", err)
	}

	ctx, parentSpan := provider.Tracer("rabbitmq-test").Start(ctx, "request")
	avatar := model.Avatar{
		ID:     "c0decafe-babe-4bed-b042-feeddeadbeef",
		UserID: "Alice",
		S3Key:  "originals/Alice/c0decafe-babe-4bed-b042-feeddeadbeef/avatar.webp",
	}
	if err := publisher.PublishAvatarUploaded(ctx, avatar); err != nil {
		parentSpan.End()
		t.Fatalf("PublishAvatarUploaded() error = %v", err)
	}

	item := receiveDelivery(t, ctx, deliveries)
	if err := item.Ack(); err != nil {
		parentSpan.End()
		t.Fatalf("Ack() error = %v", err)
	}

	producerSpan := findSpan(t, spanRecorder.Ended(), "send "+exchange+":"+event.AvatarUploadedRoutingKey)
	if producerSpan.SpanKind() != trace.SpanKindProducer {
		t.Errorf("producer span kind = %v, want %v", producerSpan.SpanKind(), trace.SpanKindProducer)
	}
	if got := producerSpan.Parent().SpanID(); got != parentSpan.SpanContext().SpanID() {
		t.Errorf("producer parent span ID = %s, want %s", got, parentSpan.SpanContext().SpanID())
	}

	headers := propagation.MapCarrier(item.Headers())
	if headers.Get("traceparent") == "" {
		t.Fatal("RabbitMQ delivery traceparent header is empty")
	}

	extracted := propagation.TraceContext{}.Extract(context.Background(), headers)
	messageSpanContext := trace.SpanContextFromContext(extracted)
	if !messageSpanContext.IsRemote() {
		t.Error("extracted message span context is not remote")
	}
	if got := messageSpanContext.TraceID(); got != parentSpan.SpanContext().TraceID() {
		t.Errorf("message trace ID = %s, want %s", got, parentSpan.SpanContext().TraceID())
	}
	if got := messageSpanContext.SpanID(); got != producerSpan.SpanContext().SpanID() {
		t.Errorf("message parent span ID = %s, want producer span ID %s", got, producerSpan.SpanContext().SpanID())
	}

	parentSpan.End()
}

func findSpan(t *testing.T, spans []sdktrace.ReadOnlySpan, name string) sdktrace.ReadOnlySpan {
	t.Helper()

	for _, span := range spans {
		if span.Name() == name {
			return span
		}
	}

	t.Fatalf("span %q not found", name)

	return nil
}
