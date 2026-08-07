//go:build integration

package s3_test

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/xhrobj/gophprofile/internal/s3"
)

func TestIntegration_MinIOTracing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), s3IntegrationTestTimeout)
	t.Cleanup(cancel)

	endpoint := requireEnv(t, "S3_ENDPOINT")
	accessKey := requireEnv(t, "S3_ACCESS_KEY")
	secretKey := requireEnv(t, "S3_SECRET_KEY")
	useSSL, err := strconv.ParseBool(requireEnv(t, "S3_USE_SSL"))
	if err != nil {
		t.Fatalf("parse S3_USE_SSL: %v", err)
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

	bucket := fmt.Sprintf("gophprofile-tracing-test-%d", time.Now().UnixNano())
	cleanupClient := newMinIOClient(t, endpoint, accessKey, secretKey, useSSL)
	t.Cleanup(func() {
		cleanupBucket(t, cleanupClient, bucket)
	})

	storage, err := s3.Open(ctx, endpoint, accessKey, secretKey, bucket, useSSL)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	spanCountBeforePut := len(spanRecorder.Ended())
	ctx, parentSpan := provider.Tracer("s3-test").Start(ctx, "parent")
	defer parentSpan.End()

	content := []byte("avatar-image-data")
	if err := storage.Put(ctx, "avatar.jpg", bytes.NewReader(content), int64(len(content)), "image/jpeg"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	var requestSpan sdktrace.ReadOnlySpan
	for _, span := range spanRecorder.Ended()[spanCountBeforePut:] {
		if span.InstrumentationScope().Name == otelhttp.ScopeName &&
			span.Parent().SpanID() == parentSpan.SpanContext().SpanID() {
			requestSpan = span
			break
		}
	}
	if requestSpan == nil {
		t.Fatal("S3 HTTP span not found")
	}

	if got, want := requestSpan.SpanContext().TraceID(), parentSpan.SpanContext().TraceID(); got != want {
		t.Errorf("trace ID = %s, want %s", got, want)
	}
}
