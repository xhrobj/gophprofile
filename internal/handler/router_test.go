package handler

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/xhrobj/gophprofile/internal/model"
	"github.com/xhrobj/gophprofile/internal/service"
)

type noopAvatarService struct{}

type recordedHTTPRequest struct {
	method   string
	route    string
	status   int
	duration time.Duration
}

type recordingHTTPMetrics struct {
	requests []recordedHTTPRequest
}

func (m *recordingHTTPMetrics) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func (m *recordingHTTPMetrics) ObserveHTTPRequest(method, route string, status int, duration time.Duration) {
	m.requests = append(m.requests, recordedHTTPRequest{
		method:   method,
		route:    route,
		status:   status,
		duration: duration,
	})
}

func TestRouter_RequestID(t *testing.T) {
	tests := []struct {
		name              string
		incomingRequestID string
	}{
		{name: "generates request ID"},
		{name: "replaces incoming request ID", incomingRequestID: "external-request-id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			router := NewRouter(testLogger(&output), noopAvatarService{}, noopHealthChecker{}, 10<<20, nil)
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.incomingRequestID != "" {
				request.Header.Set(requestIDHeader, tt.incomingRequestID)
			}
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Errorf("response status = %d, want %d", response.Code, http.StatusOK)
			}

			gotRequestID := response.Header().Get(requestIDHeader)
			if gotRequestID == "" {
				t.Fatal("response request ID is empty")
			}
			if gotRequestID == tt.incomingRequestID {
				t.Errorf("response request ID = %q, want generated request ID", gotRequestID)
			}

			decoded, err := hex.DecodeString(gotRequestID)
			if err != nil {
				t.Fatalf("generated request ID is not hexadecimal: %v", err)
			}
			if len(decoded) != requestIDSize {
				t.Errorf("generated request ID size = %d, want %d", len(decoded), requestIDSize)
			}

			entry := decodeLogEntry(t, &output)
			assertEntryField(t, entry, "service", "server")
			assertEntryField(t, entry, "request_id", gotRequestID)
			assertEntryField(t, entry, "method", http.MethodGet)
			assertEntryField(t, entry, "path", "/")
			assertEntryNumber(t, entry, "status", http.StatusOK)
			if _, ok := entry["duration"]; !ok {
				t.Error("log entry has no duration field")
			}
		})
	}
}

func TestRouter_Tracing(t *testing.T) {
	previousProvider := otel.GetTracerProvider()
	previousPropagator := otel.GetTextMapPropagator()
	spanRecorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		otel.SetTracerProvider(previousProvider)
		otel.SetTextMapPropagator(previousPropagator)
		_ = provider.Shutdown(context.Background())
	})

	router := NewRouter(discardLogger(), noopAvatarService{}, noopHealthChecker{}, 10<<20, nil)
	request := httptest.NewRequest(http.MethodGet, "/web/gallery/Alice", nil)
	request.Header.Set("traceparent", "00-c0decafebabe4bedb042feeddeadbeef-deadbeefc0decafe-01")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Errorf("response status = %d, want %d", response.Code, http.StatusOK)
	}

	spans := spanRecorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}
	span := spans[0]
	if got := span.Name(); got != "GET /web/gallery/{userID}" {
		t.Errorf("span name = %q, want %q", got, "GET /web/gallery/{userID}")
	}
	if got := span.SpanContext().TraceID().String(); got != "c0decafebabe4bedb042feeddeadbeef" {
		t.Errorf("trace ID = %q, want propagated trace ID", got)
	}
	if got := spanAttribute(span, "http.route"); got != "/web/gallery/{userID}" {
		t.Errorf("http.route = %q, want %q", got, "/web/gallery/{userID}")
	}
}

func TestRouter_HTTPMetrics(t *testing.T) {
	metrics := &recordingHTTPMetrics{}
	router := NewRouter(discardLogger(), noopAvatarService{}, noopHealthChecker{}, 10<<20, metrics)

	paths := []string{
		"/web/gallery/Alice",
		"/missing/c0decafe-babe-4bed-b042-feeddeadbeef",
		"/health",
		"/metrics",
	}
	for _, path := range paths {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()

		router.ServeHTTP(response, request)
	}

	if len(metrics.requests) != 2 {
		t.Fatalf("observed requests = %d, want 2", len(metrics.requests))
	}

	matched := metrics.requests[0]
	if matched.method != http.MethodGet {
		t.Errorf("matched method = %q, want %q", matched.method, http.MethodGet)
	}
	if matched.route != "/web/gallery/{userID}" {
		t.Errorf("matched route = %q, want %q", matched.route, "/web/gallery/{userID}")
	}
	if matched.status != http.StatusOK {
		t.Errorf("matched status = %d, want %d", matched.status, http.StatusOK)
	}
	unmatched := metrics.requests[1]
	if unmatched.route != "" {
		t.Errorf("unmatched route = %q, want empty route pattern", unmatched.route)
	}
	if unmatched.status != http.StatusNotFound {
		t.Errorf("unmatched status = %d, want %d", unmatched.status, http.StatusNotFound)
	}
}

func TestRouter_Web(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{name: "root", path: "/"},
		{name: "upload page", path: "/web/upload"},
		{name: "gallery page", path: "/web/gallery/Alice"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := NewRouter(discardLogger(), noopAvatarService{}, noopHealthChecker{}, 10<<20, nil)
			request := httptest.NewRequest(http.MethodGet, tt.path, nil)
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Errorf("response status = %d, want %d", response.Code, http.StatusOK)
			}
			if got := response.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
				t.Errorf("Content-Type = %q, want %q", got, "text/html; charset=utf-8")
			}
			body := response.Body.String()
			if !strings.Contains(body, "GophProfile") || !strings.Contains(body, `id="uploadForm"`) {
				t.Error("response does not contain GophProfile page")
			}
		})
	}
}

func TestRouter_LogsNotFoundStatus(t *testing.T) {
	var output bytes.Buffer
	router := NewRouter(testLogger(&output), noopAvatarService{}, noopHealthChecker{}, 10<<20, nil)
	request := httptest.NewRequest(http.MethodGet, "/missing", nil)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Errorf("response status = %d, want %d", response.Code, http.StatusNotFound)
	}

	entry := decodeLogEntry(t, &output)
	assertEntryNumber(t, entry, "status", http.StatusNotFound)
}

func (noopAvatarService) Upload(context.Context, service.UploadInput) (model.Avatar, error) {
	return model.Avatar{}, nil
}

func (noopAvatarService) Download(context.Context, service.DownloadInput) (service.DownloadOutput, error) {
	return service.DownloadOutput{}, model.ErrAvatarNotFound
}

func (noopAvatarService) GetMetadata(context.Context, string) (model.Avatar, error) {
	return model.Avatar{}, model.ErrAvatarNotFound
}

func (noopAvatarService) ListByUserID(context.Context, string) ([]model.Avatar, error) {
	return nil, nil
}

func (noopAvatarService) DeleteByID(context.Context, string, string) error {
	return nil
}

func (noopAvatarService) DeleteCurrentByUserID(context.Context, string, string) error {
	return nil
}

func testLogger(output *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(output, &slog.HandlerOptions{Level: slog.LevelDebug})).With(
		slog.String("service", "server"),
	)
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func decodeLogEntry(t *testing.T, output *bytes.Buffer) map[string]any {
	t.Helper()

	scanner := bufio.NewScanner(output)
	if !scanner.Scan() {
		t.Fatal("logger produced no output")
	}

	var entry map[string]any
	if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
		t.Fatalf("decode log entry: %v", err)
	}

	if scanner.Scan() {
		t.Errorf("unexpected extra log entry: %s", scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan log output: %v", err)
	}

	return entry
}

func assertEntryField(t *testing.T, entry map[string]any, key string, want string) {
	t.Helper()

	got, ok := entry[key]
	if !ok {
		t.Fatalf("log entry has no field %q", key)
	}
	if got != want {
		t.Errorf("log field %q = %#v, want %q", key, got, want)
	}
}

func assertEntryNumber(t *testing.T, entry map[string]any, key string, want int) {
	t.Helper()

	got, ok := entry[key]
	if !ok {
		t.Fatalf("log entry has no field %q", key)
	}
	if got != float64(want) {
		t.Errorf("log field %q = %#v, want %d", key, got, want)
	}
}

func spanAttribute(span sdktrace.ReadOnlySpan, key string) string {
	for _, attr := range span.Attributes() {
		if string(attr.Key) == key {
			return attr.Value.AsString()
		}
	}

	return ""
}
