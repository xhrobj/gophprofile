package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/xhrobj/gophprofile/internal/logger"
	"github.com/xhrobj/gophprofile/internal/model"
	"github.com/xhrobj/gophprofile/internal/service"
)

type avatarDownloader interface {
	Download(ctx context.Context, input service.DownloadInput) (service.DownloadOutput, error)
}

type downloadHandler struct {
	service avatarDownloader
	logger  *slog.Logger
}

func newDownloadHandler(avatarService avatarDownloader, baseLogger *slog.Logger) *downloadHandler {
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
		case errors.Is(err, service.ErrInvalidAvatarSize):
			writeError(w, r, http.StatusBadRequest, "invalid_size", "supported sizes: original, 100x100, 300x300", 0)
		case errors.Is(err, model.ErrAvatarNotFound):
			writeError(w, r, http.StatusNotFound, "avatar_not_found", "avatar not found", 0)
		case errors.Is(err, service.ErrServiceUnavailable):
			logger.WithRequestID(h.logger, RequestIDFromContext(r.Context())).ErrorContext(r.Context(),
				"avatar download dependency unavailable",
				slog.Any("error", err),
			)
			writeError(w, r, http.StatusServiceUnavailable, "service_unavailable", "service temporarily unavailable", 0)
		default:
			logger.WithRequestID(h.logger, RequestIDFromContext(r.Context())).ErrorContext(r.Context(),
				"failed to download avatar",
				slog.Any("error", err),
			)
			writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error", 0)
		}

		return
	}
	defer func() {
		if closeErr := output.Content.Close(); closeErr != nil {
			logger.WithRequestID(h.logger, RequestIDFromContext(r.Context())).WarnContext(r.Context(),
				"failed to close avatar content",
				slog.Any("error", closeErr),
			)
		}
	}()

	w.Header().Set("Content-Type", output.ContentType)
	if _, err := io.Copy(w, output.Content); err != nil {
		logger.WithRequestID(h.logger, RequestIDFromContext(r.Context())).ErrorContext(r.Context(),
			"failed to write avatar content",
			slog.Any("error", fmt.Errorf("copy avatar content: %w", err)),
		)
	}
}
