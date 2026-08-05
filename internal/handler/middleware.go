package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/xhrobj/gophprofile/internal/logger"
)

const (
	requestIDHeader = "X-Request-ID"
	requestIDSize   = 16
)

type requestIDContextKey struct{}

type responseWriter struct {
	http.ResponseWriter
	status int
}

// RequestIDFromContext возвращает идентификатор текущего HTTP-запроса.
func RequestIDFromContext(ctx context.Context) string {
	requestID, _ := ctx.Value(requestIDContextKey{}).(string)

	return requestID
}

func (w *responseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}

	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}

	return w.ResponseWriter.Write(data)
}

func (w *responseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func newRequestID() (string, error) {
	value := make([]byte, requestIDSize)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}

	return hex.EncodeToString(value), nil
}

func requestIDMiddleware(baseLogger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID, err := newRequestID()
			if err != nil {
				baseLogger.Error("failed to generate request ID", zap.Error(err))
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)

				return
			}

			w.Header().Set(requestIDHeader, requestID)
			ctx := context.WithValue(r.Context(), requestIDContextKey{}, requestID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func accessLogMiddleware(baseLogger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			startedAt := time.Now()
			writer := &responseWriter{ResponseWriter: w}

			next.ServeHTTP(writer, r)

			status := writer.status
			if status == 0 {
				status = http.StatusOK
			}

			logger.WithRequestID(baseLogger, RequestIDFromContext(r.Context())).Info(
				"HTTP request completed",
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.Int("status", status),
				zap.Duration("duration", time.Since(startedAt)),
			)
		})
	}
}
