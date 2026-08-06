package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/xhrobj/gophprofile/internal/model"
)

type fakeAvatarMetadataReader struct {
	noopAvatarService

	metadataAvatar model.Avatar
	metadataErr    error
	metadataCalls  []string

	listAvatars []model.Avatar
	listErr     error
	listCalls   []string
}

func TestMetadataHandler_GetByID(t *testing.T) {
	createdAt := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Minute)
	api := &fakeAvatarMetadataReader{metadataAvatar: model.Avatar{
		ID:        testAvatarID,
		UserID:    "Alice",
		FileName:  "avatar.png",
		MIMEType:  "image/png",
		SizeBytes: 1024,
		Width:     1920,
		Height:    1080,
		ThumbnailS3Keys: map[model.ThumbnailSize]string{
			model.ThumbnailSize300x300: "thumbnails/300.jpg",
			model.ThumbnailSize100x100: "thumbnails/100.jpg",
		},
		UploadStatus:     model.UploadStatusCompleted,
		ProcessingStatus: model.ProcessingStatusCompleted,
		CreatedAt:        createdAt,
		UpdatedAt:        updatedAt,
	}}
	router := NewRouter(zap.NewNop(), api, noopHealthChecker{}, testMaxUploadSize)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/avatars/"+testAvatarID+"/metadata", nil)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("response status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}

	var body avatarMetadataResponse
	decodeJSONResponse(t, response, &body)
	if body.ID != testAvatarID || body.UserID != "Alice" {
		t.Errorf("metadata identity = %q/%q, want %q/Alice", body.ID, body.UserID, testAvatarID)
	}
	if body.FileName != "avatar.png" || body.MIMEType != "image/png" || body.Size != 1024 {
		t.Errorf("metadata file = %+v, want avatar.png image/png 1024", body)
	}
	if body.Dimensions != (dimensionsResponse{Width: 1920, Height: 1080}) {
		t.Errorf("dimensions = %+v, want 1920x1080", body.Dimensions)
	}
	if len(body.Thumbnails) != 2 {
		t.Fatalf("thumbnails count = %d, want 2", len(body.Thumbnails))
	}
	if body.Thumbnails[0] != (thumbnailResponse{Size: "100x100", URL: "/api/v1/avatars/" + testAvatarID + "?size=100x100"}) {
		t.Errorf("first thumbnail = %+v, want 100x100", body.Thumbnails[0])
	}
	if body.Thumbnails[1] != (thumbnailResponse{Size: "300x300", URL: "/api/v1/avatars/" + testAvatarID + "?size=300x300"}) {
		t.Errorf("second thumbnail = %+v, want 300x300", body.Thumbnails[1])
	}
	if body.UploadStatus != "completed" || body.ProcessingStatus != "completed" {
		t.Errorf("statuses = %q/%q, want completed/completed", body.UploadStatus, body.ProcessingStatus)
	}
	if !body.CreatedAt.Equal(createdAt) || !body.UpdatedAt.Equal(updatedAt) {
		t.Errorf("timestamps = %v/%v, want %v/%v", body.CreatedAt, body.UpdatedAt, createdAt, updatedAt)
	}
	if len(api.metadataCalls) != 1 || api.metadataCalls[0] != testAvatarID {
		t.Errorf("GetMetadata() calls = %#v, want %q", api.metadataCalls, testAvatarID)
	}
}

func TestMetadataHandler_GetByID_WithoutThumbnails(t *testing.T) {
	api := &fakeAvatarMetadataReader{metadataAvatar: model.Avatar{ID: testAvatarID}}
	router := NewRouter(zap.NewNop(), api, noopHealthChecker{}, testMaxUploadSize)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/avatars/"+testAvatarID+"/metadata", nil)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	var body avatarMetadataResponse
	decodeJSONResponse(t, response, &body)
	if body.Thumbnails == nil {
		t.Fatal("thumbnails = nil, want empty array")
	}
	if len(body.Thumbnails) != 0 {
		t.Errorf("thumbnails count = %d, want 0", len(body.Thumbnails))
	}
}

func TestMetadataHandler_ListByUserID(t *testing.T) {
	createdAt := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	api := &fakeAvatarMetadataReader{listAvatars: []model.Avatar{
		{
			ID:               testAvatarID,
			UserID:           "Alice",
			UploadStatus:     model.UploadStatusCompleted,
			ProcessingStatus: model.ProcessingStatusCompleted,
			CreatedAt:        createdAt,
		},
		{
			ID:               "c0decafe-babe-4bed-b043-feeddeadbeef",
			UserID:           "Alice",
			UploadStatus:     model.UploadStatusCompleted,
			ProcessingStatus: model.ProcessingStatusPending,
			CreatedAt:        createdAt.Add(-time.Minute),
		},
		{
			ID:               "c0decafe-babe-4bed-b044-feeddeadbeef",
			UserID:           "Alice",
			UploadStatus:     model.UploadStatusFailed,
			ProcessingStatus: model.ProcessingStatusPending,
			CreatedAt:        createdAt.Add(-2 * time.Minute),
		},
	}}
	router := NewRouter(zap.NewNop(), api, noopHealthChecker{}, testMaxUploadSize)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/users/Alice/avatars", nil)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("response status = %d, want %d", response.Code, http.StatusOK)
	}
	var body avatarListResponse
	decodeJSONResponse(t, response, &body)
	if body.Total != 3 || len(body.Avatars) != 3 {
		t.Fatalf("list size = %d/%d, want 3/3", body.Total, len(body.Avatars))
	}
	wantStatuses := []string{"completed", "processing", "failed"}
	for i, wantStatus := range wantStatuses {
		if body.Avatars[i].Status != wantStatus {
			t.Errorf("avatar %d status = %q, want %q", i, body.Avatars[i].Status, wantStatus)
		}
		if body.Avatars[i].URL != "/api/v1/avatars/"+api.listAvatars[i].ID {
			t.Errorf("avatar %d URL = %q, want canonical URL", i, body.Avatars[i].URL)
		}
	}
	if len(api.listCalls) != 1 || api.listCalls[0] != "Alice" {
		t.Errorf("ListByUserID() calls = %#v, want Alice", api.listCalls)
	}
}

func TestMetadataHandler_ListByUserID_Empty(t *testing.T) {
	api := &fakeAvatarMetadataReader{}
	router := NewRouter(zap.NewNop(), api, noopHealthChecker{}, testMaxUploadSize)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/users/Eve/avatars", nil)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	var body avatarListResponse
	decodeJSONResponse(t, response, &body)
	if body.Total != 0 {
		t.Errorf("total = %d, want 0", body.Total)
	}
	if body.Avatars == nil {
		t.Fatal("avatars = nil, want empty array")
	}
	if len(body.Avatars) != 0 {
		t.Errorf("avatars count = %d, want 0", len(body.Avatars))
	}
}

func TestMetadataHandler_InvalidPathParameters(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{name: "invalid avatar ID", path: "/api/v1/avatars/not-a-uuid/metadata"},
		{name: "too long user ID", path: "/api/v1/users/" + strings.Repeat("a", maxUserIDBytes+1) + "/avatars"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := &fakeAvatarMetadataReader{}
			router := NewRouter(zap.NewNop(), api, noopHealthChecker{}, testMaxUploadSize)
			request := httptest.NewRequest(http.MethodGet, tt.path, nil)
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			assertErrorResponse(t, response, http.StatusBadRequest, "invalid_request", 0)
			if len(api.metadataCalls) != 0 {
				t.Errorf("GetMetadata() calls = %d, want 0", len(api.metadataCalls))
			}
			if len(api.listCalls) != 0 {
				t.Errorf("ListByUserID() calls = %d, want 0", len(api.listCalls))
			}
		})
	}
}

func TestMetadataHandler_Error(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		metadataErr error
		listErr     error
		wantStatus  int
		wantCode    string
	}{
		{name: "metadata not found", path: "/api/v1/avatars/" + testAvatarID + "/metadata", metadataErr: model.ErrAvatarNotFound, wantStatus: http.StatusNotFound, wantCode: "avatar_not_found"},
		{name: "metadata internal error", path: "/api/v1/avatars/" + testAvatarID + "/metadata", metadataErr: errors.New("metadata"), wantStatus: http.StatusInternalServerError, wantCode: "internal_error"},
		{name: "list internal error", path: "/api/v1/users/Alice/avatars", listErr: errors.New("list"), wantStatus: http.StatusInternalServerError, wantCode: "internal_error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := &fakeAvatarMetadataReader{metadataErr: tt.metadataErr, listErr: tt.listErr}
			router := NewRouter(zap.NewNop(), api, noopHealthChecker{}, testMaxUploadSize)
			request := httptest.NewRequest(http.MethodGet, tt.path, nil)
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			assertErrorResponse(t, response, tt.wantStatus, tt.wantCode, 0)
		})
	}
}

func (f *fakeAvatarMetadataReader) GetMetadata(_ context.Context, avatarID string) (model.Avatar, error) {
	f.metadataCalls = append(f.metadataCalls, avatarID)
	return f.metadataAvatar, f.metadataErr
}

func (f *fakeAvatarMetadataReader) ListByUserID(_ context.Context, userID string) ([]model.Avatar, error) {
	f.listCalls = append(f.listCalls, userID)
	return f.listAvatars, f.listErr
}
