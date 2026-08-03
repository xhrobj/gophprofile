package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/xhrobj/gophprofile/internal/logger"
	"github.com/xhrobj/gophprofile/internal/model"
	"github.com/xhrobj/gophprofile/internal/service"
)

type avatarDownloader interface {
	Download(ctx context.Context, input service.DownloadInput) (service.DownloadOutput, error)
}

type downloadHandler struct {
	service avatarDownloader
	logger  *zap.Logger
}

func newDownloadHandler(avatarService avatarDownloader, baseLogger *zap.Logger) *downloadHandler {
	return &downloadHandler{service: avatarService, logger: baseLogger}
}

func (h *downloadHandler) byID(w http.ResponseWriter, r *http.Request) {
	avatarID := chi.URLParam(r, "avatarID")
	if err := validateAvatarID(avatarID); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error(), 0)

		return
	}

	h.serve(w, r, service.DownloadInput{
		AvatarID: avatarID,
		Size:     r.URL.Query().Get("size"),
	})
}

func (h *downloadHandler) currentByUserID(w http.ResponseWriter, r *http.Request) {
	userID, err := validateUserID(chi.URLParam(r, "userID"), "user_id")
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error(), 0)

		return
	}

	h.serve(w, r, service.DownloadInput{
		UserID: userID,
		Size:   r.URL.Query().Get("size"),
	})
}

func (h *downloadHandler) serve(w http.ResponseWriter, r *http.Request, input service.DownloadInput) {
	output, err := h.service.Download(r.Context(), input)
	if err != nil {
		switch {
		case service.IsInvalidAvatarSize(err):
			writeError(w, r, http.StatusBadRequest, "invalid_size", "supported sizes: original, 100x100, 300x300", 0)
		case errors.Is(err, model.ErrAvatarNotFound):
			writeError(w, r, http.StatusNotFound, "avatar_not_found", "avatar not found", 0)
		default:
			logger.WithRequestID(h.logger, RequestIDFromContext(r.Context())).Error(
				"failed to download avatar",
				zap.Error(err),
			)
			writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error", 0)
		}

		return
	}
	defer func() {
		if closeErr := output.Content.Close(); closeErr != nil {
			logger.WithRequestID(h.logger, RequestIDFromContext(r.Context())).Warn(
				"failed to close avatar content",
				zap.Error(closeErr),
			)
		}
	}()

	w.Header().Set("Content-Type", output.ContentType)
	if _, err := io.Copy(w, output.Content); err != nil {
		logger.WithRequestID(h.logger, RequestIDFromContext(r.Context())).Error(
			"failed to write avatar content",
			zap.Error(fmt.Errorf("copy avatar content: %w", err)),
		)
	}
}
