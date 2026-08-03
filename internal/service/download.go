package service

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/xhrobj/gophprofile/internal/model"
)

const OriginalAvatarSize = "original"

// DownloadInput определяет аватарку и размер изображения для получения.
type DownloadInput struct {
	AvatarID string
	UserID   string
	Size     string
}

// DownloadOutput содержит поток изображения и его MIME-тип.
type DownloadOutput struct {
	Content     io.ReadCloser
	ContentType string
}

// ErrInvalidAvatarSize означает, что запрошен неподдерживаемый размер изображения.
var ErrInvalidAvatarSize = errors.New("invalid avatar size")

// Download открывает оригинал или готовую миниатюру аватарки для потоковой отдачи.
func (s *AvatarService) Download(ctx context.Context, input DownloadInput) (DownloadOutput, error) {
	avatar, err := s.findAvatar(ctx, input)
	if err != nil {
		return DownloadOutput{}, err
	}

	key, contentType, err := selectObject(avatar, input.Size)
	if err != nil {
		return DownloadOutput{}, err
	}

	content, err := s.storage.Get(ctx, key)
	if err != nil {
		return DownloadOutput{}, fmt.Errorf("get avatar object: %w", err)
	}

	return DownloadOutput{
		Content:     content,
		ContentType: contentType,
	}, nil
}

// IsInvalidAvatarSize сообщает, что запрошен неподдерживаемый размер изображения.
func IsInvalidAvatarSize(err error) bool {
	return errors.Is(err, ErrInvalidAvatarSize)
}

func (s *AvatarService) findAvatar(ctx context.Context, input DownloadInput) (model.Avatar, error) {
	if input.AvatarID != "" {
		avatar, err := s.repository.GetByID(ctx, input.AvatarID)
		if err != nil {
			return model.Avatar{}, fmt.Errorf("get avatar by ID: %w", err)
		}

		return avatar, nil
	}

	avatar, err := s.repository.GetCurrentByUserID(ctx, input.UserID)
	if err != nil {
		return model.Avatar{}, fmt.Errorf("get current user avatar: %w", err)
	}

	return avatar, nil
}

func selectObject(avatar model.Avatar, size string) (string, string, error) {
	switch size {
	case "", OriginalAvatarSize:
		return avatar.S3Key, avatar.MIMEType, nil
	case string(model.ThumbnailSize100x100):
		return thumbnailObject(avatar, model.ThumbnailSize100x100)
	case string(model.ThumbnailSize300x300):
		return thumbnailObject(avatar, model.ThumbnailSize300x300)
	default:
		return "", "", fmt.Errorf("select avatar object: %w", ErrInvalidAvatarSize)
	}
}

func thumbnailObject(avatar model.Avatar, size model.ThumbnailSize) (string, string, error) {
	key := avatar.ThumbnailS3Keys[size]
	if key == "" {
		return "", "", fmt.Errorf("select avatar thumbnail %s: %w", size, model.ErrAvatarNotFound)
	}

	return key, "image/jpeg", nil
}
