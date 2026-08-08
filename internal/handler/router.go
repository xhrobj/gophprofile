package handler

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/xhrobj/gophprofile/web"
)

// HTTPMetrics описывает HTTP-метрики, используемые маршрутизатором.
type HTTPMetrics interface {
	// Handler возвращает HTTP-обработчик для экспорта метрик Prometheus.
	Handler() http.Handler

	// ObserveHTTPRequest регистрирует завершенный HTTP-запрос.
	// NOTE: route должен содержать шаблон маршрута, а не фактический URL.
	ObserveHTTPRequest(method, route string, status int, duration time.Duration)
}

// AvatarService объединяет операции сервиса аватаров, используемые HTTP-маршрутизатором.
type AvatarService interface {
	avatarUploader
	avatarDownloader
	avatarMetadataReader
	avatarDeleter
}

// NewRouter создаёт HTTP-маршрутизатор Сервера с middleware и зарегистрированными маршрутами.
func NewRouter(
	baseLogger *slog.Logger,
	avatarService AvatarService,
	healthChecker healthChecker,
	maxUploadSize int64,
	metrics HTTPMetrics,
) http.Handler {
	router := chi.NewRouter()

	router.Use(otelhttp.NewMiddleware("HTTP", otelhttp.WithSpanNameFormatter(httpSpanName)))
	router.Use(traceRouteMiddleware)
	router.Use(requestIDMiddleware(baseLogger))
	if metrics != nil {
		router.Use(httpMetricsMiddleware(metrics))
	}
	router.Use(accessLogMiddleware(baseLogger))

	webHandler := web.Handler()

	router.Get("/", webHandler.ServeHTTP)
	router.Get("/web/upload", webHandler.ServeHTTP)
	router.Get("/web/gallery/{userID}", webHandler.ServeHTTP)

	router.Get("/health", newHealthHandler(healthChecker))
	if metrics != nil {
		router.Handle("/metrics", metrics.Handler())
	}

	upload := newUploadHandler(avatarService, maxUploadSize, baseLogger)
	download := newDownloadHandler(avatarService, baseLogger)
	metadata := newMetadataHandler(avatarService, baseLogger)
	deleteAvatar := newDeleteHandler(avatarService, baseLogger)

	router.Post("/api/v1/avatars", upload.ServeHTTP)
	router.Get("/api/v1/avatars/{avatarID}", download.byID)
	router.Delete("/api/v1/avatars/{avatarID}", deleteAvatar.byID)
	router.Get("/api/v1/avatars/{avatarID}/metadata", metadata.byID)

	router.Get("/api/v1/users/{userID}/avatar", download.currentByUserID)
	router.Delete("/api/v1/users/{userID}/avatar", deleteAvatar.currentByUserID)
	router.Get("/api/v1/users/{userID}/avatars", metadata.listByUserID)

	return router
}

func httpSpanName(_ string, r *http.Request) string {
	return r.Method
}
