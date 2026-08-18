package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/xhrobj/gophprofile/internal/logger"
	"github.com/xhrobj/gophprofile/internal/model"
	"github.com/xhrobj/gophprofile/internal/service"
)

type avatarDeleter interface {
	DeleteByID(ctx context.Context, avatarID, requesterUserID string) error
	DeleteCurrentByUserID(ctx context.Context, userID, requesterUserID string) error
}

type deleteHandler struct {
	service avatarDeleter
	logger  *slog.Logger
}

func newDeleteHandler(avatarService avatarDeleter, baseLogger *slog.Logger) *deleteHandler {
	return &deleteHandler{service: avatarService, logger: baseLogger}
}

func (h *deleteHandler) byID(w http.ResponseWriter, r *http.Request) {
	avatarID := chi.URLParam(r, "avatarID")
	if err := validateAvatarID(avatarID); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error(), 0)

		return
	}

	requesterUserID, err := validateUserID(r.Header.Get(userIDHeader), "X-User-ID header")
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error(), 0)
		return
	}

	if err := h.service.DeleteByID(r.Context(), avatarID, requesterUserID); err != nil {
		h.writeDeleteError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *deleteHandler) currentByUserID(w http.ResponseWriter, r *http.Request) {
	userID, err := validateUserID(chi.URLParam(r, "userID"), "user_id")
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error(), 0)
		return
	}

	requesterUserID, err := validateUserID(r.Header.Get(userIDHeader), "X-User-ID header")
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error(), 0)
		return
	}

	if err := h.service.DeleteCurrentByUserID(r.Context(), userID, requesterUserID); err != nil {
		h.writeDeleteError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *deleteHandler) writeDeleteError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, service.ErrForbidden):
		writeError(w, r, http.StatusForbidden, "forbidden", "you can only delete your own avatars", 0)
	case errors.Is(err, model.ErrAvatarNotFound):
		writeError(w, r, http.StatusNotFound, "avatar_not_found", "avatar not found", 0)
	case errors.Is(err, service.ErrServiceUnavailable):
		logger.WithRequestID(h.logger, RequestIDFromContext(r.Context())).ErrorContext(r.Context(),
			"avatar deletion dependency unavailable",
			slog.Any("error", err),
		)
		writeError(w, r, http.StatusServiceUnavailable, "service_unavailable", "service temporarily unavailable", 0)
	default:
		logger.WithRequestID(h.logger, RequestIDFromContext(r.Context())).ErrorContext(r.Context(),
			"failed to delete avatar",
			slog.Any("error", err),
		)
		writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error", 0)
	}
}
