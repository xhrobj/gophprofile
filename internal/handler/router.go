package handler

import (
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/xhrobj/gophprofile/internal/logger"
)

// NewRouter создаёт HTTP-маршрутизатор Сервера с middleware и зарегистрированными маршрутами.
func NewRouter(baseLogger *zap.Logger, avatarService interface {
	avatarUploader
	avatarDownloader
	avatarMetadataReader
}, maxUploadSize int64) http.Handler {
	router := chi.NewRouter()

	router.Use(requestIDMiddleware(baseLogger))
	router.Use(accessLogMiddleware(baseLogger))

	router.Get("/", rootHandler(baseLogger))
	upload := newUploadHandler(avatarService, maxUploadSize, baseLogger)
	download := newDownloadHandler(avatarService, baseLogger)
	metadata := newMetadataHandler(avatarService, baseLogger)

	router.Post("/api/v1/avatars", upload.ServeHTTP)
	router.Get("/api/v1/avatars/{avatarID}", download.byID)
	router.Get("/api/v1/avatars/{avatarID}/metadata", metadata.byID)
	router.Get("/api/v1/users/{userID}/avatar", download.currentByUserID)
	router.Get("/api/v1/users/{userID}/avatars", metadata.listByUserID)

	return router
}

func rootHandler(baseLogger *zap.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")

		if _, err := io.WriteString(w, "GophProfile is running!\n"); err != nil {
			logger.WithRequestID(baseLogger, RequestIDFromContext(r.Context())).Error(
				"failed to write HTTP response",
				zap.Error(err),
			)
		}
	}
}
