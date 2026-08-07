package logger

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestNew(t *testing.T) {
	lg, err := New("server", "info")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if lg == nil {
		t.Fatal("New() = nil, want logger")
	}
}

func TestNew_Validation(t *testing.T) {
	tests := []struct {
		name    string
		service string
		level   string
		want    string
	}{
		{name: "empty service", service: "  ", level: "info", want: "service"},
		{name: "unknown level", service: "server", level: "trace", want: "trace"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.service, tt.level)
			if err == nil {
				t.Fatal("New() error = nil, want validation error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("New() error = %q, want %q", err, tt.want)
			}
		})
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  slog.Level
	}{
		{name: "debug", value: "DEBUG", want: slog.LevelDebug},
		{name: "info", value: "info", want: slog.LevelInfo},
		{name: "warn", value: " warn ", want: slog.LevelWarn},
		{name: "error", value: "error", want: slog.LevelError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseLevel(tt.value)
			if err != nil {
				t.Fatalf("parseLevel() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("parseLevel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseLevel_RejectsUnknownValue(t *testing.T) {
	_, err := parseLevel("trace")
	if err == nil {
		t.Fatal("parseLevel() error = nil, want validation error")
	}
}

func TestContextFields(t *testing.T) {
	var output bytes.Buffer
	lg, err := newLogger("server", "warn", &output)
	if err != nil {
		t.Fatalf("newLogger() error = %v", err)
	}

	traceID := trace.TraceID{
		0xc0, 0xde, 0xca, 0xfe, 0xba, 0xbe, 0x4b, 0xed,
		0xb0, 0x42, 0xfe, 0xed, 0xde, 0xad, 0xbe, 0xef,
	}
	spanID := trace.SpanID{0xde, 0xad, 0xbe, 0xef, 0xc0, 0xde, 0xca, 0xfe}
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: spanID})
	ctx := trace.ContextWithSpanContext(context.Background(), spanContext)

	lg.InfoContext(ctx, "hidden")
	requestLogger := WithRequestID(lg, "faceb00cf00dfeeddeadbeefc0decafe")
	messageLogger := WithMessageID(requestLogger, "deadbeef-f00d-4dad-b042-c0decafe0bad")
	messageLogger.WarnContext(ctx, "visible")

	scanner := bufio.NewScanner(&output)
	if !scanner.Scan() {
		t.Fatal("logger produced no output")
	}

	var entry map[string]any
	if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
		t.Fatalf("decode log entry: %v", err)
	}

	assertLogField(t, entry, "level", "warn")
	assertLogField(t, entry, "msg", "visible")
	assertLogField(t, entry, serviceKey, "server")
	assertLogField(t, entry, requestIDKey, "faceb00cf00dfeeddeadbeefc0decafe")
	assertLogField(t, entry, messageIDKey, "deadbeef-f00d-4dad-b042-c0decafe0bad")
	assertLogField(t, entry, traceIDKey, traceID.String())
	assertLogField(t, entry, spanIDKey, spanID.String())

	if scanner.Scan() {
		t.Errorf("unexpected extra log entry: %s", scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan log output: %v", err)
	}
}

func assertLogField(t *testing.T, entry map[string]any, key string, want string) {
	t.Helper()

	got, ok := entry[key]
	if !ok {
		t.Fatalf("log entry has no field %q", key)
	}
	if got != want {
		t.Errorf("log field %q = %#v, want %q", key, got, want)
	}
}
