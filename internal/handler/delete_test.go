package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/xhrobj/gophprofile/internal/model"
	"github.com/xhrobj/gophprofile/internal/service"
)

type fakeAvatarDeleter struct {
	noopAvatarService

	deleteByIDErr      error
	deleteCurrentErr   error
	deleteByIDCalls    [][2]string
	deleteCurrentCalls [][2]string
}

var errDeleteAvatar = errors.New("delete avatar")

func TestDeleteHandler(t *testing.T) {
	tests := []struct {
		name            string
		path            string
		wantByIDCall    [2]string
		wantCurrentCall [2]string
	}{
		{
			name:         "deletes avatar by ID",
			path:         "/api/v1/avatars/" + testAvatarID,
			wantByIDCall: [2]string{testAvatarID, "Alice"},
		},
		{
			name:            "deletes current user avatar",
			path:            "/api/v1/users/Alice/avatar",
			wantCurrentCall: [2]string{"Alice", "Alice"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := &fakeAvatarDeleter{}
			router := NewRouter(zap.NewNop(), api, noopHealthChecker{}, testMaxUploadSize)
			request := httptest.NewRequest(http.MethodDelete, tt.path, nil)
			request.Header.Set(userIDHeader, "Alice")
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			if response.Code != http.StatusNoContent {
				t.Fatalf("response status = %d, want %d", response.Code, http.StatusNoContent)
			}
			if response.Body.Len() != 0 {
				t.Errorf("response body = %q, want empty", response.Body.String())
			}
			if tt.wantByIDCall != ([2]string{}) {
				if len(api.deleteByIDCalls) != 1 || api.deleteByIDCalls[0] != tt.wantByIDCall {
					t.Errorf("DeleteByID() calls = %#v, want %#v", api.deleteByIDCalls, tt.wantByIDCall)
				}
			}
			if tt.wantCurrentCall != ([2]string{}) {
				if len(api.deleteCurrentCalls) != 1 || api.deleteCurrentCalls[0] != tt.wantCurrentCall {
					t.Errorf("DeleteCurrentByUserID() calls = %#v, want %#v", api.deleteCurrentCalls, tt.wantCurrentCall)
				}
			}
		})
	}
}

func TestDeleteHandler_Validation(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		userID string
	}{
		{name: "invalid avatar ID", path: "/api/v1/avatars/not-a-uuid", userID: "Alice"},
		{name: "missing requester", path: "/api/v1/avatars/" + testAvatarID},
		{name: "invalid path user ID", path: "/api/v1/users/" + strings.Repeat("a", maxUserIDLen+1) + "/avatar", userID: "Alice"},
		{name: "blank requester", path: "/api/v1/users/Alice/avatar", userID: "   "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := &fakeAvatarDeleter{}
			router := NewRouter(zap.NewNop(), api, noopHealthChecker{}, testMaxUploadSize)
			request := httptest.NewRequest(http.MethodDelete, tt.path, nil)
			if tt.userID != "" {
				request.Header.Set(userIDHeader, tt.userID)
			}
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			assertErrorResponse(t, response, http.StatusBadRequest, "invalid_request", 0)
			if len(api.deleteByIDCalls) != 0 || len(api.deleteCurrentCalls) != 0 {
				t.Errorf("delete calls = %d/%d, want 0/0", len(api.deleteByIDCalls), len(api.deleteCurrentCalls))
			}
		})
	}
}

func TestDeleteHandler_Error(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "forbidden", err: service.ErrForbidden, wantStatus: http.StatusForbidden, wantCode: "forbidden"},
		{name: "avatar not found", err: model.ErrAvatarNotFound, wantStatus: http.StatusNotFound, wantCode: "avatar_not_found"},
		{name: "service unavailable", err: service.ErrServiceUnavailable, wantStatus: http.StatusServiceUnavailable, wantCode: "service_unavailable"},
		{name: "internal error", err: errDeleteAvatar, wantStatus: http.StatusInternalServerError, wantCode: "internal_error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := &fakeAvatarDeleter{deleteByIDErr: tt.err}
			router := NewRouter(zap.NewNop(), api, noopHealthChecker{}, testMaxUploadSize)
			request := httptest.NewRequest(http.MethodDelete, "/api/v1/avatars/"+testAvatarID, nil)
			request.Header.Set(userIDHeader, "Alice")
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			assertErrorResponse(t, response, tt.wantStatus, tt.wantCode, 0)
		})
	}
}

func (f *fakeAvatarDeleter) DeleteByID(_ context.Context, avatarID, requesterUserID string) error {
	f.deleteByIDCalls = append(f.deleteByIDCalls, [2]string{avatarID, requesterUserID})
	return f.deleteByIDErr
}

func (f *fakeAvatarDeleter) DeleteCurrentByUserID(_ context.Context, userID, requesterUserID string) error {
	f.deleteCurrentCalls = append(f.deleteCurrentCalls, [2]string{userID, requesterUserID})
	return f.deleteCurrentErr
}
