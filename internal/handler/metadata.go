package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/xhrobj/gophprofile/internal/logger"
	"github.com/xhrobj/gophprofile/internal/model"
	"github.com/xhrobj/gophprofile/internal/service"
)

type avatarMetadataReader interface {
	GetMetadata(ctx context.Context, avatarID string) (model.Avatar, error)
	ListByUserID(ctx context.Context, userID string) ([]model.Avatar, error)
}

type metadataHandler struct {
	service avatarMetadataReader
	logger  *slog.Logger
}

type dimensionsResponse struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type thumbnailResponse struct {
	Size string `json:"size"`
	URL  string `json:"url"`
}

type avatarMetadataResponse struct {
	ID     string `json:"id"`
	UserID string `json:"user_id"`

	FileName   string              `json:"file_name"`
	MIMEType   string              `json:"mime_type"`
	Size       int64               `json:"size"`
	Dimensions dimensionsResponse  `json:"dimensions"`
	Thumbnails []thumbnailResponse `json:"thumbnails"`

	UploadStatus     string `json:"upload_status"`
	ProcessingStatus string `json:"processing_status"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type avatarListResponse struct {
	Avatars []avatarUploadResponse `json:"avatars"`
	Total   int                    `json:"total"`
}

func newMetadataHandler(avatarService avatarMetadataReader, baseLogger *slog.Logger) *metadataHandler {
	return &metadataHandler{service: avatarService, logger: baseLogger}
}

func (h *metadataHandler) byID(w http.ResponseWriter, r *http.Request) {
	avatarID := chi.URLParam(r, "avatarID")
	if err := validateAvatarID(avatarID); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error(), 0)

		return
	}

	avatar, err := h.service.GetMetadata(r.Context(), avatarID)
	if err != nil {
		h.writeReadError(w, r, err, "failed to get avatar metadata")
		return
	}

	writeJSON(w, http.StatusOK, newAvatarMetadataResponse(avatar))
}

func (h *metadataHandler) listByUserID(w http.ResponseWriter, r *http.Request) {
	userID, err := validateUserID(chi.URLParam(r, "userID"), "user_id")
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error(), 0)

		return
	}

	avatars, err := h.service.ListByUserID(r.Context(), userID)
	if err != nil {
		h.writeReadError(w, r, err, "failed to list user avatars")

		return
	}

	response := avatarListResponse{
		Avatars: make([]avatarUploadResponse, 0, len(avatars)),
		Total:   len(avatars),
	}
	for _, avatar := range avatars {
		response.Avatars = append(response.Avatars, newAvatarSummaryResponse(avatar))
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *metadataHandler) writeReadError(w http.ResponseWriter, r *http.Request, err error, message string) {
	switch {
	case errors.Is(err, model.ErrAvatarNotFound):
		writeError(w, r, http.StatusNotFound, "avatar_not_found", "avatar not found", 0)

		return
	case errors.Is(err, service.ErrServiceUnavailable):
		logger.WithRequestID(h.logger, RequestIDFromContext(r.Context())).ErrorContext(
			r.Context(),
			"avatar metadata dependency unavailable",
			slog.Any("error", err),
		)
		writeError(w, r, http.StatusServiceUnavailable, "service_unavailable", "service temporarily unavailable", 0)

		return
	}

	logger.WithRequestID(h.logger, RequestIDFromContext(r.Context())).ErrorContext(r.Context(), message, slog.Any("error", err))
	writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error", 0)
}

func newAvatarMetadataResponse(avatar model.Avatar) avatarMetadataResponse {
	thumbnails := make([]thumbnailResponse, 0, 2)
	for _, size := range []model.ThumbnailSize{model.ThumbnailSize100x100, model.ThumbnailSize300x300} {
		if _, ok := avatar.ThumbnailS3Keys[size]; !ok {
			continue
		}

		thumbnails = append(thumbnails, thumbnailResponse{
			Size: string(size),
			URL:  "/api/v1/avatars/" + avatar.ID + "?size=" + string(size),
		})
	}

	return avatarMetadataResponse{
		ID:     avatar.ID,
		UserID: avatar.UserID,

		FileName: avatar.FileName,
		MIMEType: avatar.MIMEType,
		Size:     avatar.SizeBytes,
		Dimensions: dimensionsResponse{
			Width:  avatar.Width,
			Height: avatar.Height,
		},
		Thumbnails: thumbnails,

		UploadStatus:     string(avatar.UploadStatus),
		ProcessingStatus: string(avatar.ProcessingStatus),

		CreatedAt: avatar.CreatedAt,
		UpdatedAt: avatar.UpdatedAt,
	}
}

func newAvatarSummaryResponse(avatar model.Avatar) avatarUploadResponse {
	return avatarUploadResponse{
		ID:        avatar.ID,
		UserID:    avatar.UserID,
		URL:       "/api/v1/avatars/" + avatar.ID,
		Status:    publicAvatarStatus(avatar),
		CreatedAt: avatar.CreatedAt,
	}
}

func publicAvatarStatus(avatar model.Avatar) string {
	if avatar.UploadStatus == model.UploadStatusFailed || avatar.ProcessingStatus == model.ProcessingStatusFailed {
		return "failed"
	}
	if avatar.UploadStatus == model.UploadStatusCompleted && avatar.ProcessingStatus == model.ProcessingStatusCompleted {
		return "completed"
	}

	return "processing"
}
