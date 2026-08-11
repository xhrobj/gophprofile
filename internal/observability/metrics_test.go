package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type storageUsageReader struct {
	usage    int64
	err      error
	calls    atomic.Int32
	contexts chan context.Context
}

func TestServerMetrics_HTTP(t *testing.T) {
	metrics := NewServerMetrics()
	metrics.ObserveHTTPRequest(http.MethodGet, "/api/v1/avatars/{avatarID}", http.StatusOK, 5*time.Millisecond)
	metrics.ObserveHTTPRequest("CUSTOM", "", http.StatusInternalServerError, 42*time.Millisecond)

	body := metricsBody(t, metrics.Handler())

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

func TestServerMetrics_AvatarOperations(t *testing.T) {
	metrics := NewServerMetrics()
	metrics.ObserveAvatarUpload(true, 1024, 5*time.Millisecond)
	metrics.ObserveAvatarUpload(false, 2048, 10*time.Millisecond)
	metrics.ObserveAvatarDeletion(true)
	metrics.ObserveAvatarDeletion(false)

	body := metricsBody(t, metrics.Handler())

	for _, want := range []string{
		`gophprofile_avatar_uploads_total{result="success"} 1`,
		`gophprofile_avatar_uploads_total{result="error"} 1`,
		`gophprofile_avatar_upload_duration_seconds_count{result="success"} 1`,
		`gophprofile_avatar_upload_duration_seconds_count{result="error"} 1`,
		`gophprofile_avatar_upload_size_bytes_count 1`,
		`gophprofile_avatar_deletions_total{result="success"} 1`,
		`gophprofile_avatar_deletions_total{result="error"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics output does not contain %q", want)
		}
	}
}

func TestServerMetrics_AvatarStorageUsage(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	metrics := NewServerMetrics()
	reader := &storageUsageReader{
		usage:    4096,
		contexts: make(chan context.Context, 1),
	}
	metrics.RegisterAvatarStorageUsage(ctx, reader)

	queryCtx := waitStorageUsageQuery(t, reader)
	waitMetricContains(t, metrics.Handler(), `gophprofile_avatar_storage_usage_bytes 4096`)
	if !tracingSuppressed(queryCtx) {
		t.Error("storage usage query context does not suppress tracing")
	}

	callsBeforeScrape := reader.calls.Load()
	metricsBody(t, metrics.Handler())
	metricsBody(t, metrics.Handler())
	if callsAfterScrape := reader.calls.Load(); callsAfterScrape != callsBeforeScrape {
		t.Errorf("StorageUsageBytes() calls after scrape = %d, want %d", callsAfterScrape, callsBeforeScrape)
	}
}

func TestServerMetrics_AvatarStorageUsage_Error(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	metrics := NewServerMetrics()
	reader := &storageUsageReader{
		err:      context.DeadlineExceeded,
		contexts: make(chan context.Context, 1),
	}
	metrics.RegisterAvatarStorageUsage(ctx, reader)

	waitStorageUsageQuery(t, reader)
	waitMetricContains(t, metrics.Handler(), `gophprofile_avatar_storage_usage_bytes NaN`)
}

func TestServerMetrics_PostgreSQLPool(t *testing.T) {
	pool, err := pgxpool.New(
		context.Background(),
		"postgres://gophprofile:gophprofile@127.0.0.1:1/gophprofile?sslmode=disable",
	)
	if err != nil {
		t.Fatalf("create PostgreSQL pool: %v", err)
	}
	t.Cleanup(pool.Close)

	metrics := NewServerMetrics()
	metrics.RegisterPostgreSQLPool(pool)
	body := metricsBody(t, metrics.Handler())

	for _, want := range []string{
		`gophprofile_postgres_pool_acquired_connections 0`,
		`gophprofile_postgres_pool_idle_connections 0`,
		`gophprofile_postgres_pool_total_connections 0`,
		`gophprofile_postgres_pool_acquires_total 0`,
		`gophprofile_postgres_pool_acquire_duration_seconds_total 0`,
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

	if !strings.Contains(metricsBody(t, first.Handler()), "gophprofile_http_requests_total") {
		t.Error("first registry has no HTTP request metric")
	}
	if strings.Contains(metricsBody(t, second.Handler()), "gophprofile_http_requests_total") {
		t.Error("second registry contains HTTP request metric from first registry")
	}
}

func TestWorkerMetrics_ProcessedEvents(t *testing.T) {
	metrics := NewWorkerMetrics()
	metrics.ObserveProcessedEvent("avatar.uploaded", true, 5*time.Millisecond)
	metrics.ObserveProcessedEvent("avatar.deleted", false, 10*time.Millisecond)
	metrics.ObserveProcessedEvent("custom.event", false, 15*time.Millisecond)

	body := metricsBody(t, metrics.Handler())

	for _, want := range []string{
		`gophprofile_worker_processed_events_total{event="avatar.uploaded",result="success"} 1`,
		`gophprofile_worker_processed_events_total{event="avatar.deleted",result="error"} 1`,
		`gophprofile_worker_processed_events_total{event="unknown",result="error"} 1`,
		`gophprofile_worker_processing_duration_seconds_count{event="avatar.uploaded",result="success"} 1`,
		`go_goroutines`,
		`process_start_time_seconds`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics output does not contain %q", want)
		}
	}
}

func TestNewWorkerMetrics_UsesIndependentRegistry(t *testing.T) {
	first := NewWorkerMetrics()
	second := NewWorkerMetrics()

	first.ObserveProcessedEvent("avatar.uploaded", true, time.Millisecond)

	if !strings.Contains(metricsBody(t, first.Handler()), "gophprofile_worker_processed_events_total") {
		t.Error("first registry has no Worker event metric")
	}
	if strings.Contains(metricsBody(t, second.Handler()), "gophprofile_worker_processed_events_total") {
		t.Error("second registry contains Worker event metric from first registry")
	}
}

func (s *storageUsageReader) StorageUsageBytes(ctx context.Context) (int64, error) {
	s.calls.Add(1)
	if s.contexts != nil {
		select {
		case s.contexts <- ctx:
		default:
		}
	}

	return s.usage, s.err
}

func waitStorageUsageQuery(t *testing.T, reader *storageUsageReader) context.Context {
	t.Helper()

	select {
	case ctx := <-reader.contexts:
		return ctx
	case <-time.After(time.Second):
		t.Fatal("StorageUsageBytes() was not called")

		return nil
	}
}

func waitMetricContains(t *testing.T, handler http.Handler, want string) {
	t.Helper()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(metricsBody(t, handler), want) {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("metrics output does not contain %q", want)
}

func metricsBody(t *testing.T, handler http.Handler) string {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("metrics status = %d, want %d", response.Code, http.StatusOK)
	}

	return response.Body.String()
}
