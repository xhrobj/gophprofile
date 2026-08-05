package handler

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/xhrobj/gophprofile/internal/model"
	"github.com/xhrobj/gophprofile/internal/service"
)

type fakeAvatarDownloader struct {
	noopAvatarService

	downloadOutput service.DownloadOutput
	downloadErr    error
	downloadCalls  []service.DownloadInput
}

func TestDownloadHandler(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		wantInput service.DownloadInput
	}{
		{
			name:      "gets avatar by ID",
			path:      "/api/v1/avatars/" + testAvatarID + "?size=100x100",
			wantInput: service.DownloadInput{AvatarID: testAvatarID, Size: "100x100"},
		},
		{
			name:      "gets current user avatar",
			path:      "/api/v1/users/Alice/avatar?size=original",
			wantInput: service.DownloadInput{UserID: "Alice", Size: "original"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := &fakeAvatarDownloader{downloadOutput: service.DownloadOutput{
				Content:     io.NopCloser(bytes.NewReader([]byte("image"))),
				ContentType: "image/jpeg",
			}}
			router := NewRouter(zap.NewNop(), api, noopHealthChecker{}, testMaxUploadSize)
			request := httptest.NewRequest(http.MethodGet, tt.path, nil)
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("response status = %d, want %d", response.Code, http.StatusOK)
			}
			if got := response.Header().Get("Content-Type"); got != "image/jpeg" {
				t.Errorf("Content-Type = %q, want image/jpeg", got)
			}
			if got := response.Body.String(); got != "image" {
				t.Errorf("response body = %q, want image", got)
			}
			if len(api.downloadCalls) != 1 || api.downloadCalls[0] != tt.wantInput {
				t.Errorf("Download() calls = %#v, want %#v", api.downloadCalls, tt.wantInput)
			}
		})
	}
}

func TestDownloadHandler_InvalidPathParameters(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{name: "invalid avatar ID", path: "/api/v1/avatars/not-a-uuid"},
		{name: "too long user ID", path: "/api/v1/users/" + strings.Repeat("a", maxUserIDLen+1) + "/avatar"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := &fakeAvatarDownloader{}
			router := NewRouter(zap.NewNop(), api, noopHealthChecker{}, testMaxUploadSize)
			request := httptest.NewRequest(http.MethodGet, tt.path, nil)
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			assertErrorResponse(t, response, http.StatusBadRequest, "invalid_request", 0)
			if len(api.downloadCalls) != 0 {
				t.Errorf("Download() calls = %d, want 0", len(api.downloadCalls))
			}
		})
	}
}

func TestDownloadHandler_Error(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "invalid size", err: service.ErrInvalidAvatarSize, wantStatus: http.StatusBadRequest, wantCode: "invalid_size"},
		{name: "avatar not found", err: model.ErrAvatarNotFound, wantStatus: http.StatusNotFound, wantCode: "avatar_not_found"},
		{name: "internal error", err: errors.New("download"), wantStatus: http.StatusInternalServerError, wantCode: "internal_error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := &fakeAvatarDownloader{downloadErr: tt.err}
			router := NewRouter(zap.NewNop(), api, noopHealthChecker{}, testMaxUploadSize)
			request := httptest.NewRequest(http.MethodGet, "/api/v1/avatars/"+testAvatarID, nil)
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			assertErrorResponse(t, response, tt.wantStatus, tt.wantCode, 0)
		})
	}
}

func (f *fakeAvatarDownloader) Download(_ context.Context, input service.DownloadInput) (service.DownloadOutput, error) {
	f.downloadCalls = append(f.downloadCalls, input)
	return f.downloadOutput, f.downloadErr
}
