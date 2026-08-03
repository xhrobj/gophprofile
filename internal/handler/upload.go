package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"go.uber.org/zap"

	"github.com/xhrobj/gophprofile/internal/logger"
	"github.com/xhrobj/gophprofile/internal/model"
	"github.com/xhrobj/gophprofile/internal/service"
)

const (
	userIDHeader = "X-User-ID"

	multipartOverheadAllowance = 1 << 20
)

type avatarUploader interface {
	Upload(ctx context.Context, input service.UploadInput) (model.Avatar, error)
}

type uploadHandler struct {
	service       avatarUploader
	maxUploadSize int64
	logger        *zap.Logger
}

type uploadedFile struct {
	fileName string
	content  []byte
}

var errFileTooLarge = errors.New("uploaded file is too large")

// ServeHTTP обрабатывает загрузку новой аватарки.
func (h *uploadHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	userID, err := validateUserID(r.Header.Get(userIDHeader), "X-User-ID header")
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error(), 0)

		return
	}

	file, err := h.readMultipartFile(w, r)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		switch {
		case errors.As(err, &maxBytesErr), errors.Is(err, errFileTooLarge):
			writeError(
				w,
				r,
				http.StatusRequestEntityTooLarge,
				"file_too_large",
				fmt.Sprintf("maximum upload size is %d bytes", h.maxUploadSize),
				h.maxUploadSize,
			)
		default:
			writeError(w, r, http.StatusBadRequest, "invalid_request", err.Error(), 0)
		}

		return
	}

	avatar, err := h.service.Upload(r.Context(), service.UploadInput{
		UserID:   userID,
		FileName: file.fileName,
		Content:  file.content,
	})
	if err != nil {
		if errors.Is(err, service.ErrInvalidImageFormat) {
			writeError(
				w,
				r,
				http.StatusBadRequest,
				"invalid_file_format",
				"supported formats: jpeg, png, webp",
				0,
			)

			return
		}

		logger.WithRequestID(h.logger, RequestIDFromContext(r.Context())).Error(
			"failed to upload avatar",
			zap.Error(err),
		)
		writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error", 0)

		return
	}

	location := "/api/v1/avatars/" + avatar.ID
	w.Header().Set("Location", location)
	writeJSON(w, http.StatusCreated, avatarUploadResponse{
		ID:        avatar.ID,
		UserID:    avatar.UserID,
		URL:       location,
		Status:    "processing",
		CreatedAt: avatar.CreatedAt,
	})
}

// newUploadHandler создаёт HTTP-handler загрузки аватарок.
func newUploadHandler(avatarService avatarUploader, maxUploadSize int64, baseLogger *zap.Logger) *uploadHandler {
	return &uploadHandler{
		service:       avatarService,
		maxUploadSize: maxUploadSize,
		logger:        baseLogger,
	}
}

func (h *uploadHandler) readMultipartFile(w http.ResponseWriter, r *http.Request) (uploadedFile, error) {
	r.Body = http.MaxBytesReader(w, r.Body, h.maxUploadSize+multipartOverheadAllowance)

	reader, err := r.MultipartReader()
	if err != nil {
		return uploadedFile{}, fmt.Errorf("read multipart request: %w", err)
	}

	var file uploadedFile
	found := false

	for {
		part, nextErr := reader.NextPart()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return uploadedFile{}, fmt.Errorf("read multipart part: %w", nextErr)
		}

		if part.FormName() != "file" {
			if _, copyErr := io.Copy(io.Discard, part); copyErr != nil {
				_ = part.Close()

				return uploadedFile{}, fmt.Errorf("discard multipart field: %w", copyErr)
			}
			_ = part.Close()

			continue
		}

		if found {
			_ = part.Close()

			return uploadedFile{}, errors.New("multipart field file must be provided exactly once")
		}
		found = true

		fileName := part.FileName()
		if fileName == "" {
			_ = part.Close()

			return uploadedFile{}, errors.New("multipart field file must contain a file")
		}
		if err := validateFileName(fileName); err != nil {
			_ = part.Close()

			return uploadedFile{}, err
		}

		content, readErr := io.ReadAll(io.LimitReader(part, h.maxUploadSize+1))
		_ = part.Close()
		if readErr != nil {
			return uploadedFile{}, fmt.Errorf("read multipart file: %w", readErr)
		}
		if int64(len(content)) > h.maxUploadSize {
			return uploadedFile{}, errFileTooLarge
		}

		file = uploadedFile{
			fileName: fileName,
			content:  content,
		}
	}

	if !found {
		return uploadedFile{}, errors.New("multipart field file is required")
	}

	return file, nil
}
