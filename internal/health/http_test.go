package health

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeReporter struct {
	report Report
}

func TestNewLivenessHandler(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/live", nil)
	response := httptest.NewRecorder()

	NewLivenessHandler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", response.Code, http.StatusOK)
	}
}

func TestNewReadinessHandler(t *testing.T) {
	tests := []struct {
		name       string
		report     Report
		wantStatus int
	}{
		{
			name:       "available",
			report:     Report{Status: statusOK},
			wantStatus: http.StatusOK,
		},
		{
			name: "unavailable",
			report: Report{
				Status: statusUnavailable,
				Components: Components{
					Broker: Component{Status: componentStatusError, Error: "connection refused"},
				},
			},
			wantStatus: http.StatusServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/health", nil)
			response := httptest.NewRecorder()

			NewReadinessHandler(fakeReporter{report: tt.report}).ServeHTTP(response, request)

			if response.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", response.Code, tt.wantStatus)
			}
			if got := response.Header().Get("Content-Type"); got != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", got)
			}
			if strings.Contains(response.Body.String(), "connection refused") {
				t.Error("response exposes internal dependency error")
			}
		})
	}
}

func (f fakeReporter) Check(context.Context) Report {
	return f.report
}
