package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/xhrobj/gophprofile/web"
)

// NewRouter создаёт HTTP-маршрутизатор Сервера с middleware и зарегистрированными маршрутами.
func NewRouter(baseLogger *zap.Logger, avatarService interface {
	avatarUploader
	avatarDownloader
	avatarMetadataReader
	avatarDeleter
}, healthChecker healthChecker, maxUploadSize int64) http.Handler {
	router := chi.NewRouter()

	router.Use(requestIDMiddleware(baseLogger))
	router.Use(accessLogMiddleware(baseLogger))

	webHandler := web.Handler()

	router.Get("/", webHandler.ServeHTTP)
	router.Get("/web/upload", webHandler.ServeHTTP)
	router.Get("/web/gallery/{userID}", webHandler.ServeHTTP)

	router.Get("/health", newHealthHandler(healthChecker))

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
