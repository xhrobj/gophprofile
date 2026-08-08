package service

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
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

	failedUploadService := newTestAvatarService(
		&fakeAvatarRepository{createErr: errCreateMetadata},
		&fakeAvatarStorage{},
		&fakeAvatarEventPublisher{},
	)
	if _, err := failedUploadService.Upload(ctx, UploadInput{
		UserID:   "Alice",
		FileName: "avatar.png",
		Content:  encodePNG(t),
	}); err == nil {
		t.Fatal("Upload() error = nil, want error")
	}

	parentSpan.End()
	spans := spanRecorder.Ended()

	assertChildSpan(t, spans, "upload avatar", parentSpan.SpanContext().SpanID())
	assertChildSpan(t, spans, "delete avatar", parentSpan.SpanContext().SpanID())
	assertSpanStatus(t, spans, "upload avatar", codes.Error)
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

func assertSpanStatus(t *testing.T, spans []sdktrace.ReadOnlySpan, name string, want codes.Code) {
	t.Helper()

	for _, span := range spans {
		if span.Name() == name && span.Status().Code == want {
			return
		}
	}

	t.Errorf("span %q with status %v not found", name, want)
}
