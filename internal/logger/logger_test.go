package logger

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
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
		{
			name:    "empty service",
			service: "  ",
			level:   "info",
			want:    "service",
		},
		{
			name:    "unknown level",
			service: "server",
			level:   "trace",
			want:    "trace",
		},
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
		want  zapcore.Level
	}{
		{name: "debug", value: "DEBUG", want: zapcore.DebugLevel},
		{name: "info", value: "info", want: zapcore.InfoLevel},
		{name: "warn", value: " warn ", want: zapcore.WarnLevel},
		{name: "error", value: "error", want: zapcore.ErrorLevel},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseLevel(tt.value)
			if err != nil {
				t.Fatalf("parseLevel() error = %v", err)
			}
			if got.Level() != tt.want {
				t.Errorf("parseLevel() = %v, want %v", got.Level(), tt.want)
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
	lg := testLogger(&output, zapcore.WarnLevel)

	lg.Info("hidden")
	WithMessageID(WithRequestID(lg, "request-1"), "message-1").Warn("visible")

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
	assertLogField(t, entry, requestIDKey, "request-1")
	assertLogField(t, entry, messageIDKey, "message-1")

	if scanner.Scan() {
		t.Errorf("unexpected extra log entry: %s", scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan log output: %v", err)
	}
}

func testLogger(output *bytes.Buffer, level zapcore.Level) *zap.Logger {
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		zapcore.AddSync(output),
		level,
	)

	return zap.New(core).With(zap.String(serviceKey, "server"))
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
