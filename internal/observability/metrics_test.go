package observability

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestServerMetrics_HTTP(t *testing.T) {
	metrics := NewServerMetrics()
	metrics.ObserveHTTPRequest(http.MethodGet, "/api/v1/avatars/{avatarID}", http.StatusOK, 5*time.Millisecond)
	metrics.ObserveHTTPRequest("CUSTOM", "", http.StatusInternalServerError, 42*time.Millisecond)

	body := metricsBody(t, metrics)

	for _, want := range []string{
		`gophprofile_http_requests_total{method="GET",route="/api/v1/avatars/{avatarID}",status_class="2xx"} 1`,
		`gophprofile_http_requests_total{method="OTHER",route="unmatched",status_class="5xx"} 1`,
		`gophprofile_http_request_duration_seconds_bucket`,
		`go_goroutines`,
		`process_start_time_seconds`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics output does not contain %q", want)
		}
	}
}

func TestNewServerMetrics_UsesIndependentRegistry(t *testing.T) {
	first := NewServerMetrics()
	second := NewServerMetrics()

	first.ObserveHTTPRequest(http.MethodGet, "/api/v1/avatars", http.StatusOK, time.Millisecond)

	if !strings.Contains(metricsBody(t, first), "gophprofile_http_requests_total") {
		t.Error("first registry has no HTTP request metric")
	}
	if strings.Contains(metricsBody(t, second), "gophprofile_http_requests_total") {
		t.Error("second registry contains HTTP request metric from first registry")
	}
}

func metricsBody(t *testing.T, metrics *ServerMetrics) string {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	response := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("metrics status = %d, want %d", response.Code, http.StatusOK)
	}

	return response.Body.String()
}
