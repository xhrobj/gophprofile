package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"

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
	logger        *slog.Logger
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
		switch {
		case errors.Is(err, service.ErrInvalidImageFormat):
			writeError(
				w,
				r,
				http.StatusBadRequest,
				"invalid_file_format",
				"supported formats: jpeg, png, webp",
				0,
			)
		case errors.Is(err, service.ErrServiceUnavailable):
			logger.WithRequestID(h.logger, RequestIDFromContext(r.Context())).ErrorContext(r.Context(),
				"failed to publish avatar processing event",
				slog.Any("error", err),
			)
			writeError(
				w,
				r,
				http.StatusServiceUnavailable,
				"service_unavailable",
				"service temporarily unavailable",
				0,
			)
		default:
			logger.WithRequestID(h.logger, RequestIDFromContext(r.Context())).ErrorContext(r.Context(),
				"failed to upload avatar",
				slog.Any("error", err),
			)
			writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error", 0)
		}

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
func newUploadHandler(avatarService avatarUploader, maxUploadSize int64, baseLogger *slog.Logger) *uploadHandler {
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

		partFile, isFile, err := h.readMultipartPart(part, found)
		if err != nil {
			return uploadedFile{}, err
		}
		if isFile {
			file = partFile
			found = true
		}
	}

	if !found {
		return uploadedFile{}, errors.New("multipart field file is required")
	}

	return file, nil
}

func (h *uploadHandler) readMultipartPart(part *multipart.Part, fileFound bool) (uploadedFile, bool, error) {
	if part.FormName() != "file" {
		return uploadedFile{}, false, discardMultipartPart(part)
	}
	if fileFound {
		_ = part.Close()

		return uploadedFile{}, false, errors.New("multipart field file must be provided exactly once")
	}

	file, err := h.readUploadedFilePart(part)
	if err != nil {
		return uploadedFile{}, false, err
	}

	return file, true, nil
}

func (h *uploadHandler) readUploadedFilePart(part *multipart.Part) (uploadedFile, error) {
	defer func() {
		_ = part.Close()
	}()

	fileName := part.FileName()
	if fileName == "" {
		return uploadedFile{}, errors.New("multipart field file must contain a file")
	}
	if err := validateFileName(fileName); err != nil {
		return uploadedFile{}, err
	}

	content, err := io.ReadAll(io.LimitReader(part, h.maxUploadSize+1))
	if err != nil {
		return uploadedFile{}, fmt.Errorf("read multipart file: %w", err)
	}
	if int64(len(content)) > h.maxUploadSize {
		return uploadedFile{}, errFileTooLarge
	}

	return uploadedFile{
		fileName: fileName,
		content:  content,
	}, nil
}

func discardMultipartPart(part *multipart.Part) error {
	defer func() {
		_ = part.Close()
	}()

	if _, err := io.Copy(io.Discard, part); err != nil {
		return fmt.Errorf("discard multipart field: %w", err)
	}

	return nil
}
