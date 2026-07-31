package handler

import (
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/xhrobj/gophprofile/internal/logger"
)

// NewRouter создаёт HTTP-маршрутизатор Сервера с middleware и зарегистрированными маршрутами.
func NewRouter(baseLogger *zap.Logger) http.Handler {
	router := chi.NewRouter()

	router.Use(requestIDMiddleware(baseLogger))
	router.Use(accessLogMiddleware(baseLogger))

	router.Get("/", rootHandler(baseLogger))

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
