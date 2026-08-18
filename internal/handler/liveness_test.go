package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLivenessHandler(t *testing.T) {
	router := NewRouter(discardLogger(), noopAvatarService{}, noopHealthChecker{}, testMaxUploadSize, nil)
	request := httptest.NewRequest(http.MethodGet, "/live", nil)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Errorf("response status = %d, want %d", response.Code, http.StatusOK)
	}
}
