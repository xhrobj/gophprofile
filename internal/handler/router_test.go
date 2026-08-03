package handler

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/xhrobj/gophprofile/internal/model"
	"github.com/xhrobj/gophprofile/internal/service"
)

func TestRouter_RequestID(t *testing.T) {
	tests := []struct {
		name              string
		incomingRequestID string
	}{
		{
			name: "generates request ID",
		},
		{
			name:              "replaces incoming request ID",
			incomingRequestID: "external-request-id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			router := NewRouter(testLogger(&output), noopAvatarService{}, 10<<20)
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

func TestRouter_LogsNotFoundStatus(t *testing.T) {
	var output bytes.Buffer
	router := NewRouter(testLogger(&output), noopAvatarService{}, 10<<20)
	request := httptest.NewRequest(http.MethodGet, "/missing", nil)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Errorf("response status = %d, want %d", response.Code, http.StatusNotFound)
	}

	entry := decodeLogEntry(t, &output)
	assertEntryNumber(t, entry, "status", http.StatusNotFound)
}

func testLogger(output *bytes.Buffer) *zap.Logger {
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		zapcore.AddSync(output),
		zapcore.DebugLevel,
	)

	return zap.New(core).With(zap.String("service", "server"))
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

type noopAvatarService struct{}

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
