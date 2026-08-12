package observability

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/xhrobj/gophprofile/internal/event"
)

const (
	metricsNamespace            = "gophprofile"
	unknownRoute                = "unmatched"
	unknownMethod               = "OTHER"
	unknownStatusClass          = "other"
	unknownEvent                = "unknown"
	metricResultSuccess         = "success"
	metricResultError           = "error"
	storageUsageQueryTimeout    = time.Second
	storageUsageRefreshInterval = 30 * time.Second
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

// WorkerMetrics содержит Prometheus-метрики Воркера.
type WorkerMetrics struct {
	registry           *prometheus.Registry
	processedEvents    *prometheus.CounterVec
	processingDuration *prometheus.HistogramVec
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

// NewWorkerMetrics создаёт изолированный registry с метриками Воркера и стандартными метриками процесса.
func NewWorkerMetrics() *WorkerMetrics {
	registry := prometheus.NewRegistry()
	metrics := &WorkerMetrics{
		registry: registry,
		processedEvents: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: metricsNamespace,
				Subsystem: "worker",
				Name:      "processed_events_total",
				Help:      "Total number of broker events handled by the worker by event and result.",
			},
			[]string{"event", "result"},
		),
		processingDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: metricsNamespace,
				Subsystem: "worker",
				Name:      "processing_duration_seconds",
				Help:      "Broker event handling duration in seconds by event and result.",
				Buckets:   prometheus.DefBuckets,
			},
			[]string{"event", "result"},
		),
	}

	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		metrics.processedEvents,
		metrics.processingDuration,
	)

	return metrics
}

// RegisterAvatarStorageUsage добавляет кэшированную метрику логического объёма оригиналов из PostgreSQL.
func (m *ServerMetrics) RegisterAvatarStorageUsage(ctx context.Context, reader AvatarStorageUsageReader) {
	gauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Subsystem: "avatar",
		Name:      "storage_usage_bytes",
		Help:      "Total size in bytes of completed, non-deleted avatar originals recorded in PostgreSQL.",
	})
	gauge.Set(math.NaN())
	m.registry.MustRegister(gauge)

	go updateAvatarStorageUsage(ctx, gauge, reader)
}

func updateAvatarStorageUsage(ctx context.Context, gauge prometheus.Gauge, reader AvatarStorageUsageReader) {
	if ctx.Err() != nil {
		return
	}

	refreshAvatarStorageUsage(ctx, gauge, reader)
	ticker := time.NewTicker(storageUsageRefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refreshAvatarStorageUsage(ctx, gauge, reader)
		}
	}
}

func refreshAvatarStorageUsage(ctx context.Context, gauge prometheus.Gauge, reader AvatarStorageUsageReader) {
	queryCtx, cancel := context.WithTimeout(suppressTracing(ctx), storageUsageQueryTimeout)
	defer cancel()

	usage, err := reader.StorageUsageBytes(queryCtx)
	if err != nil {
		gauge.Set(math.NaN())

		return
	}

	gauge.Set(float64(usage))
}

// RegisterPostgreSQLPool добавляет метрики пула подключений PostgreSQL.
func (m *ServerMetrics) RegisterPostgreSQLPool(pool *pgxpool.Pool) {
	registerPostgreSQLPoolMetrics(m.registry, pool)
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

// RegisterPostgreSQLPool добавляет метрики пула подключений PostgreSQL.
func (m *WorkerMetrics) RegisterPostgreSQLPool(pool *pgxpool.Pool) {
	registerPostgreSQLPoolMetrics(m.registry, pool)
}

// Handler возвращает HTTP-handler для выдачи метрик Воркера в формате Prometheus.
func (m *WorkerMetrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// ObserveProcessedEvent учитывает завершенную обработку broker event.
func (m *WorkerMetrics) ObserveProcessedEvent(eventName string, success bool, duration time.Duration) {
	eventName = metricWorkerEvent(eventName)
	result := metricResult(success)

	m.processedEvents.WithLabelValues(eventName, result).Inc()
	m.processingDuration.WithLabelValues(eventName, result).Observe(duration.Seconds())
}

func registerPostgreSQLPoolMetrics(registry *prometheus.Registry, pool *pgxpool.Pool) {
	registry.MustRegister(
		prometheus.NewGaugeFunc(
			prometheus.GaugeOpts{
				Namespace: metricsNamespace,
				Subsystem: "postgres_pool",
				Name:      "acquired_connections",
				Help:      "Current number of acquired PostgreSQL pool connections.",
			},
			func() float64 { return float64(pool.Stat().AcquiredConns()) },
		),
		prometheus.NewGaugeFunc(
			prometheus.GaugeOpts{
				Namespace: metricsNamespace,
				Subsystem: "postgres_pool",
				Name:      "idle_connections",
				Help:      "Current number of idle PostgreSQL pool connections.",
			},
			func() float64 { return float64(pool.Stat().IdleConns()) },
		),
		prometheus.NewGaugeFunc(
			prometheus.GaugeOpts{
				Namespace: metricsNamespace,
				Subsystem: "postgres_pool",
				Name:      "total_connections",
				Help:      "Current total number of PostgreSQL pool connections.",
			},
			func() float64 { return float64(pool.Stat().TotalConns()) },
		),
		prometheus.NewCounterFunc(
			prometheus.CounterOpts{
				Namespace: metricsNamespace,
				Subsystem: "postgres_pool",
				Name:      "acquires_total",
				Help:      "Total number of successful PostgreSQL pool acquires.",
			},
			func() float64 { return float64(pool.Stat().AcquireCount()) },
		),
		prometheus.NewCounterFunc(
			prometheus.CounterOpts{
				Namespace: metricsNamespace,
				Subsystem: "postgres_pool",
				Name:      "acquire_duration_seconds_total",
				Help:      "Total time spent acquiring PostgreSQL pool connections in seconds.",
			},
			func() float64 { return pool.Stat().AcquireDuration().Seconds() },
		),
	)
}

func metricWorkerEvent(eventName string) string {
	switch eventName {
	case event.AvatarUploadedRoutingKey, event.AvatarDeletedRoutingKey:
		return eventName
	default:
		return unknownEvent
	}
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
