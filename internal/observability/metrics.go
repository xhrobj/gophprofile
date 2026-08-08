package observability

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	metricsNamespace   = "gophprofile"
	unknownRoute       = "unmatched"
	unknownMethod      = "OTHER"
	unknownStatusClass = "other"
)

// ServerMetrics содержит Prometheus-метрики HTTP-сервера.
type ServerMetrics struct {
	registry            *prometheus.Registry
	httpRequests        *prometheus.CounterVec
	httpRequestDuration *prometheus.HistogramVec
}

// NewServerMetrics создаёт изолированный registry с HTTP-метриками и стандартными метриками процесса.
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
	}

	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		metrics.httpRequests,
		metrics.httpRequestDuration,
	)

	return metrics
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
