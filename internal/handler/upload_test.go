package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/xhrobj/gophprofile/internal/model"
	"github.com/xhrobj/gophprofile/internal/service"
)

const (
	testMaxUploadSize = 10 << 20
	testAvatarID      = "c0decafe-babe-4bed-b042-feeddeadbeef"
)

type fakeAvatarUploader struct {
	avatar model.Avatar
	err    error
	calls  []service.UploadInput
}

var errUploadAvatar = errors.New("upload avatar")

func TestUploadHandler(t *testing.T) {
	createdAt := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	uploader := &fakeAvatarUploader{
		avatar: model.Avatar{
			ID:        testAvatarID,
			UserID:    "Alice",
			CreatedAt: createdAt,
		},
	}
	router := NewRouter(zap.NewNop(), uploader, testMaxUploadSize)
	request := newMultipartUploadRequest(t, "Alice", "avatar.png", []byte("image content"))
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("response status = %d, want %d", response.Code, http.StatusCreated)
	}
	wantLocation := "/api/v1/avatars/" + testAvatarID
	if got := response.Header().Get("Location"); got != wantLocation {
		t.Errorf("Location = %q, want %q", got, wantLocation)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}

	var body avatarUploadResponse
	decodeJSONResponse(t, response, &body)
	if body.ID != testAvatarID || body.UserID != "Alice" || body.URL != wantLocation {
		t.Errorf("response body = %+v, want ID %q, user Alice and URL %q", body, testAvatarID, wantLocation)
	}
	if body.Status != "processing" {
		t.Errorf("response status field = %q, want processing", body.Status)
	}
	if !body.CreatedAt.Equal(createdAt) {
		t.Errorf("response created_at = %s, want %s", body.CreatedAt, createdAt)
	}

	if len(uploader.calls) != 1 {
		t.Fatalf("Upload() calls = %d, want 1", len(uploader.calls))
	}
	call := uploader.calls[0]
	if call.UserID != "Alice" || call.FileName != "avatar.png" {
		t.Errorf("Upload() input = %+v, want Alice/avatar.png", call)
	}
	if !bytes.Equal(call.Content, []byte("image content")) {
		t.Errorf("Upload() content = %q, want image content", call.Content)
	}
}

func TestUploadHandler_UserID(t *testing.T) {
	tests := []struct {
		name   string
		userID string
	}{
		{name: "missing header"},
		{name: "blank header", userID: "   "},
		{name: "too long header", userID: strings.Repeat("a", maxUserIDLen+1)},
		{name: "control character", userID: "Alice\x00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uploader := &fakeAvatarUploader{}
			router := NewRouter(zap.NewNop(), uploader, testMaxUploadSize)
			request := newMultipartUploadRequest(t, tt.userID, "avatar.png", []byte("image content"))
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			assertErrorResponse(t, response, http.StatusBadRequest, "invalid_request", 0)
			if len(uploader.calls) != 0 {
				t.Errorf("Upload() calls = %d, want 0", len(uploader.calls))
			}
		})
	}
}

func TestUploadHandler_MultipartRequest(t *testing.T) {
	tests := []struct {
		name    string
		request func(t *testing.T) *http.Request
	}{
		{
			name: "missing multipart content type",
			request: func(t *testing.T) *http.Request {
				t.Helper()

				request := httptest.NewRequest(http.MethodPost, "/api/v1/avatars", strings.NewReader("data"))
				request.Header.Set(userIDHeader, "Alice")

				return request
			},
		},
		{
			name: "missing file field",
			request: func(t *testing.T) *http.Request {
				t.Helper()

				return newMultipartRequest(t, "Alice", func(writer *multipart.Writer) {
					if err := writer.WriteField("description", "avatar"); err != nil {
						t.Fatalf("write multipart field: %v", err)
					}
				})
			},
		},
		{
			name: "file field without file",
			request: func(t *testing.T) *http.Request {
				t.Helper()

				return newMultipartRequest(t, "Alice", func(writer *multipart.Writer) {
					if err := writer.WriteField("file", "not a file part"); err != nil {
						t.Fatalf("write multipart field: %v", err)
					}
				})
			},
		},
		{
			name: "duplicate file field",
			request: func(t *testing.T) *http.Request {
				t.Helper()

				return newMultipartRequest(t, "Alice", func(writer *multipart.Writer) {
					writeMultipartFile(t, writer, "avatar.png", []byte("first"))
					writeMultipartFile(t, writer, "avatar.jpg", []byte("second"))
				})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uploader := &fakeAvatarUploader{}
			router := NewRouter(zap.NewNop(), uploader, testMaxUploadSize)
			response := httptest.NewRecorder()

			router.ServeHTTP(response, tt.request(t))

			assertErrorResponse(t, response, http.StatusBadRequest, "invalid_request", 0)
			if len(uploader.calls) != 0 {
				t.Errorf("Upload() calls = %d, want 0", len(uploader.calls))
			}
		})
	}
}

func TestUploadHandler_FileName(t *testing.T) {
	uploader := &fakeAvatarUploader{}
	router := NewRouter(zap.NewNop(), uploader, testMaxUploadSize)
	request := newMultipartUploadRequest(t, "Alice", strings.Repeat("a", maxFileNameLen+1), []byte("image content"))
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	assertErrorResponse(t, response, http.StatusBadRequest, "invalid_request", 0)
	if len(uploader.calls) != 0 {
		t.Errorf("Upload() calls = %d, want 0", len(uploader.calls))
	}
}

func TestUploadHandler_FileSize(t *testing.T) {
	tests := []struct {
		name            string
		size            int
		wantStatus      int
		wantError       string
		wantUploadCalls int
	}{
		{
			name:            "accepts file at limit",
			size:            testMaxUploadSize,
			wantStatus:      http.StatusCreated,
			wantUploadCalls: 1,
		},
		{
			name:       "rejects file over limit",
			size:       testMaxUploadSize + 1,
			wantStatus: http.StatusRequestEntityTooLarge,
			wantError:  "file_too_large",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uploader := &fakeAvatarUploader{
				avatar: model.Avatar{
					ID:        testAvatarID,
					UserID:    "Alice",
					CreatedAt: time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC),
				},
			}
			router := NewRouter(zap.NewNop(), uploader, testMaxUploadSize)
			request := newMultipartUploadRequest(t, "Alice", "avatar.png", make([]byte, tt.size))
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			if tt.wantError != "" {
				assertErrorResponse(t, response, tt.wantStatus, tt.wantError, testMaxUploadSize)
			} else if response.Code != tt.wantStatus {
				t.Errorf("response status = %d, want %d", response.Code, tt.wantStatus)
			}
			if len(uploader.calls) != tt.wantUploadCalls {
				t.Errorf("Upload() calls = %d, want %d", len(uploader.calls), tt.wantUploadCalls)
			}
		})
	}
}

func TestUploadHandler_ServiceError(t *testing.T) {
	tests := []struct {
		name       string
		uploadErr  error
		wantStatus int
		wantError  string
	}{
		{
			name:       "rejects unsupported image",
			uploadErr:  service.ErrInvalidImageFormat,
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid_file_format",
		},
		{
			name:       "reports temporary service unavailability",
			uploadErr:  service.ErrServiceUnavailable,
			wantStatus: http.StatusServiceUnavailable,
			wantError:  "service_unavailable",
		},
		{
			name:       "hides internal error",
			uploadErr:  errUploadAvatar,
			wantStatus: http.StatusInternalServerError,
			wantError:  "internal_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uploader := &fakeAvatarUploader{err: tt.uploadErr}
			router := NewRouter(zap.NewNop(), uploader, testMaxUploadSize)
			request := newMultipartUploadRequest(t, "Alice", "avatar.png", []byte("image content"))
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			assertErrorResponse(t, response, tt.wantStatus, tt.wantError, 0)
		})
	}
}

func (f *fakeAvatarUploader) Upload(_ context.Context, input service.UploadInput) (model.Avatar, error) {
	f.calls = append(f.calls, input)
	if f.err != nil {
		return model.Avatar{}, f.err
	}

	return f.avatar, nil
}

func newMultipartUploadRequest(t *testing.T, userID, fileName string, content []byte) *http.Request {
	t.Helper()

	return newMultipartRequest(t, userID, func(writer *multipart.Writer) {
		writeMultipartFile(t, writer, fileName, content)
	})
}

func newMultipartRequest(
	t *testing.T,
	userID string,
	writeParts func(writer *multipart.Writer),
) *http.Request {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	writeParts(writer)
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/avatars", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	if userID != "" {
		request.Header.Set(userIDHeader, userID)
	}

	return request
}

func writeMultipartFile(t *testing.T, writer *multipart.Writer, fileName string, content []byte) {
	t.Helper()

	part, err := writer.CreateFormFile("file", fileName)
	if err != nil {
		t.Fatalf("create multipart file: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write multipart file: %v", err)
	}
}

func assertErrorResponse(
	t *testing.T,
	response *httptest.ResponseRecorder,
	wantStatus int,
	wantError string,
	wantMaxSize int64,
) {
	t.Helper()

	if response.Code != wantStatus {
		t.Fatalf("response status = %d, want %d", response.Code, wantStatus)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}

	var body errorResponse
	decodeJSONResponse(t, response, &body)
	if body.Error != wantError {
		t.Errorf("error code = %q, want %q", body.Error, wantError)
	}
	if body.Details == "" {
		t.Error("error details are empty")
	}
	if body.MaxSize != wantMaxSize {
		t.Errorf("max_size = %d, want %d", body.MaxSize, wantMaxSize)
	}
	if body.RequestID == "" {
		t.Error("request_id is empty")
	}
}

func decodeJSONResponse(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()

	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		t.Fatalf("decode JSON response: %v", err)
	}
}

func (f *fakeAvatarUploader) Download(context.Context, service.DownloadInput) (service.DownloadOutput, error) {
	return service.DownloadOutput{}, model.ErrAvatarNotFound
}

func (f *fakeAvatarUploader) GetMetadata(context.Context, string) (model.Avatar, error) {
	return model.Avatar{}, model.ErrAvatarNotFound
}

func (f *fakeAvatarUploader) ListByUserID(context.Context, string) ([]model.Avatar, error) {
	return nil, nil
}
