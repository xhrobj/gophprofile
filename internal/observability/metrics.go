package observability

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	metricsNamespace         = "gophprofile"
	unknownRoute             = "unmatched"
	unknownMethod            = "OTHER"
	unknownStatusClass       = "other"
	metricResultSuccess      = "success"
	metricResultError        = "error"
	storageUsageQueryTimeout = time.Second
)

// AvatarStorageUsageReader возвращает логический объём оригиналов аватаров из источника истины.
type AvatarStorageUsageReader interface {
	// StorageUsageBytes возвращает актуальный объём успешно загруженных неудаленных оригиналов.
	StorageUsageBytes(ctx context.Context) (int64, error)
}

// ServerMetrics содержит Prometheus-метрики Сервера.
type ServerMetrics struct {
	registry             *prometheus.Registry
	httpRequests         *prometheus.CounterVec
	httpRequestDuration  *prometheus.HistogramVec
	avatarUploads        *prometheus.CounterVec
	avatarUploadDuration *prometheus.HistogramVec
	avatarUploadSize     prometheus.Histogram
	avatarDeletions      *prometheus.CounterVec
}

var uploadSizeBuckets = []float64{
	32 * 1024,
	128 * 1024,
	512 * 1024,
	1024 * 1024,
	2 * 1024 * 1024,
	5 * 1024 * 1024,
	10 * 1024 * 1024,
}

// NewServerMetrics создаёт изолированный registry с метриками Сервера и стандартными метриками процесса.
func NewServerMetrics() *ServerMetrics {
	registry := prometheus.NewRegistry()
	metrics := &ServerMetrics{
		registry: registry,
		httpRequests: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: metricsNamespace,
				Subsystem: "http",
				Name:      "requests_total",
				Help:      "Total number of HTTP requests processed by the server.",
			},
			[]string{"method", "route", "status_class"},
		),
		httpRequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: metricsNamespace,
				Subsystem: "http",
				Name:      "request_duration_seconds",
				Help:      "HTTP request duration in seconds.",
				Buckets:   prometheus.DefBuckets,
			},
			[]string{"method", "route", "status_class"},
		),
		avatarUploads: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: metricsNamespace,
				Subsystem: "avatar",
				Name:      "uploads_total",
				Help:      "Total number of avatar upload operations by result.",
			},
			[]string{"result"},
		),
		avatarUploadDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: metricsNamespace,
				Subsystem: "avatar",
				Name:      "upload_duration_seconds",
				Help:      "Avatar upload operation duration in seconds by result.",
				Buckets:   prometheus.DefBuckets,
			},
			[]string{"result"},
		),
		avatarUploadSize: prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Namespace: metricsNamespace,
				Subsystem: "avatar",
				Name:      "upload_size_bytes",
				Help:      "Size in bytes of successfully uploaded avatar originals.",
				Buckets:   uploadSizeBuckets,
			},
		),
		avatarDeletions: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: metricsNamespace,
				Subsystem: "avatar",
				Name:      "deletions_total",
				Help:      "Total number of avatar deletion operations by result.",
			},
			[]string{"result"},
		),
	}

	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		metrics.httpRequests,
		metrics.httpRequestDuration,
		metrics.avatarUploads,
		metrics.avatarUploadDuration,
		metrics.avatarUploadSize,
		metrics.avatarDeletions,
	)

	return metrics
}

// RegisterAvatarStorageUsage добавляет метрику логического объёма оригиналов из PostgreSQL.
func (m *ServerMetrics) RegisterAvatarStorageUsage(reader AvatarStorageUsageReader) {
	m.registry.MustRegister(prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Subsystem: "avatar",
			Name:      "storage_usage_bytes",
			Help:      "Total size in bytes of completed, non-deleted avatar originals recorded in PostgreSQL.",
		},
		func() float64 {
			ctx, cancel := context.WithTimeout(context.Background(), storageUsageQueryTimeout)
			defer cancel()

			usage, err := reader.StorageUsageBytes(ctx)
			if err != nil {
				return math.NaN()
			}

			return float64(usage)
		},
	))
}

// Handler возвращает HTTP-handler для выдачи метрик в формате Prometheus.
func (m *ServerMetrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// ObserveHTTPRequest учитывает завершившийся HTTP-запрос.
func (m *ServerMetrics) ObserveHTTPRequest(method, route string, status int, duration time.Duration) {
	method = metricMethod(method)
	if route == "" {
		route = unknownRoute
	}
	statusClass := metricStatusClass(status)

	m.httpRequests.WithLabelValues(method, route, statusClass).Inc()
	m.httpRequestDuration.WithLabelValues(method, route, statusClass).Observe(duration.Seconds())
}

// ObserveAvatarUpload учитывает завершившуюся операцию загрузки аватара.
func (m *ServerMetrics) ObserveAvatarUpload(success bool, sizeBytes int64, duration time.Duration) {
	result := metricResult(success)
	m.avatarUploads.WithLabelValues(result).Inc()
	m.avatarUploadDuration.WithLabelValues(result).Observe(duration.Seconds())
	if success {
		m.avatarUploadSize.Observe(float64(sizeBytes))
	}
}

// ObserveAvatarDeletion учитывает завершившуюся операцию удаления аватара.
func (m *ServerMetrics) ObserveAvatarDeletion(success bool) {
	m.avatarDeletions.WithLabelValues(metricResult(success)).Inc()
}

func metricMethod(method string) string {
	switch method {
	case http.MethodConnect,
		http.MethodDelete,
		http.MethodGet,
		http.MethodHead,
		http.MethodOptions,
		http.MethodPatch,
		http.MethodPost,
		http.MethodPut,
		http.MethodTrace:
		return method
	default:
		return unknownMethod
	}
}

func metricStatusClass(status int) string {
	if status < 100 || status > 599 {
		return unknownStatusClass
	}

	return strconv.Itoa(status/100) + "xx"
}

func metricResult(success bool) string {
	if success {
		return metricResultSuccess
	}

	return metricResultError
}
