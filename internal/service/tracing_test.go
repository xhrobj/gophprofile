package service

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/xhrobj/gophprofile/internal/model"
)

func TestAvatarService_Tracing(t *testing.T) {
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

	ctx, parentSpan := provider.Tracer("service-test").Start(context.Background(), "request")

	uploadService := newTestAvatarService(
		&fakeAvatarRepository{},
		&fakeAvatarStorage{},
		&fakeAvatarEventPublisher{},
	)
	if _, err := uploadService.Upload(ctx, UploadInput{
		UserID:   "Alice",
		FileName: "avatar.png",
		Content:  encodePNG(t),
	}); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	deleteService := newDeleteTestAvatarService(
		&deleteRepository{avatar: model.Avatar{ID: avatarID42, UserID: "Alice"}},
		&deletePublisher{},
	)
	if err := deleteService.DeleteByID(ctx, avatarID42, "Alice"); err != nil {
		t.Fatalf("DeleteByID() error = %v", err)
	}

	parentSpan.End()

	assertChildSpan(t, spanRecorder.Ended(), "upload avatar", parentSpan.SpanContext().SpanID())
	assertChildSpan(t, spanRecorder.Ended(), "delete avatar", parentSpan.SpanContext().SpanID())
}

func assertChildSpan(t *testing.T, spans []sdktrace.ReadOnlySpan, name string, parentID trace.SpanID) {
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
