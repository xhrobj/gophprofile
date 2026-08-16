package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xhrobj/gophprofile/internal/health"
)

type fakeHealthChecker struct {
	report health.Report
	calls  int
}

type noopHealthChecker struct{}

func TestHealthHandler(t *testing.T) {
	tests := []struct {
		name       string
		report     health.Report
		wantStatus int
	}{
		{
			name: "healthy",
			report: health.Report{
				Status: "ok",
				Components: health.Components{
					Database: health.Component{Status: "ok"},
					S3:       health.Component{Status: "ok"},
					Broker:   health.Component{Status: "ok"},
				},
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "dependency unavailable",
			report: health.Report{
				Status: "unavailable",
				Components: health.Components{
					Database: health.Component{Status: "ok"},
					S3:       health.Component{Status: "error", Error: "connection refused"},
					Broker:   health.Component{Status: "ok"},
				},
			},
			wantStatus: http.StatusServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := &fakeHealthChecker{report: tt.report}
			router := NewRouter(discardLogger(), noopAvatarService{}, checker, testMaxUploadSize, nil)
			request := httptest.NewRequest(http.MethodGet, "/health", nil)
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			assertHealthResponse(t, response, tt.wantStatus, tt.report)
			if checker.calls != 1 {
				t.Errorf("Check() calls = %d, want 1", checker.calls)
			}
		})
	}
}

func (f *fakeHealthChecker) Check(context.Context) health.Report {
	f.calls++
	return f.report
}

func (noopHealthChecker) Check(context.Context) health.Report {
	return health.Report{Status: "ok"}
}

func assertHealthResponse(
	t *testing.T,
	response *httptest.ResponseRecorder,
	wantStatus int,
	wantBody health.Report,
) {
	t.Helper()

	if response.Code != wantStatus {
		t.Fatalf("response status = %d, want %d", response.Code, wantStatus)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	if strings.Contains(response.Body.String(), "connection refused") {
		t.Error("response leaks raw dependency error")
	}

	var body health.Report
	decodeJSONResponse(t, response, &body)
	wantBody.Components.Database.Error = ""
	wantBody.Components.S3.Error = ""
	wantBody.Components.Broker.Error = ""
	if body != wantBody {
		t.Errorf("response body = %+v, want %+v", body, wantBody)
	}
}
