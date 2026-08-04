package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/xhrobj/gophprofile/internal/model"
)

// AvatarRepository описывает операции с метаданными аватаров, необходимые application-сервису.
type AvatarRepository interface {
	Create(ctx context.Context, avatar model.Avatar) (model.Avatar, error)
	GetByID(ctx context.Context, avatarID string) (model.Avatar, error)
	GetCurrentByUserID(ctx context.Context, userID string) (model.Avatar, error)
	ListByUserID(ctx context.Context, userID string) ([]model.Avatar, error)
	UpdateUploadStatus(ctx context.Context, avatarID string, status model.UploadStatus) error
	DeletePermanent(ctx context.Context, avatarID string) error
}

// AvatarStorage описывает операции с объектным хранилищем, необходимые application-сервису.
type AvatarStorage interface {
	Put(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, keys ...string) error
}

// AvatarEventPublisher публикует события, необходимые application-сервису.
type AvatarEventPublisher interface {
	PublishAvatarUploaded(ctx context.Context, avatar model.Avatar) error
}

// IDGenerator создаёт идентификатор новой аватарки.
type IDGenerator func() string

// OriginalKeyBuilder создаёт канонический ключ оригинала в объектном хранилище.
type OriginalKeyBuilder func(userID, avatarID, fileName string) string

// UploadInput содержит данные загружаемого изображения.
type UploadInput struct {
	UserID   string
	FileName string
	Content  []byte
}

// AvatarService выполняет application-сценарии работы с аватарками.
type AvatarService struct {
	repository       AvatarRepository
	storage          AvatarStorage
	publisher        AvatarEventPublisher
	generateID       IDGenerator
	buildOriginalKey OriginalKeyBuilder
}

// NewAvatarService создаёт application-сервис аватаров.
func NewAvatarService(
	repository AvatarRepository,
	storage AvatarStorage,
	publisher AvatarEventPublisher,
	generateID IDGenerator,
	buildOriginalKey OriginalKeyBuilder,
) *AvatarService {
	return &AvatarService{
		repository:       repository,
		storage:          storage,
		publisher:        publisher,
		generateID:       generateID,
		buildOriginalKey: buildOriginalKey,
	}
}

// Upload создаёт метаданные аватарки, сохраняет оригинал и публикует событие для фоновой обработки.
func (s *AvatarService) Upload(ctx context.Context, input UploadInput) (model.Avatar, error) {
	metadata, err := inspectImage(input.Content)
	if err != nil {
		return model.Avatar{}, fmt.Errorf("inspect avatar image: %w", err)
	}

	avatarID := s.generateID()
	key := s.buildOriginalKey(input.UserID, avatarID, input.FileName)

	avatar, err := s.repository.Create(ctx, model.Avatar{
		ID:        avatarID,
		UserID:    input.UserID,
		FileName:  input.FileName,
		MIMEType:  metadata.mimeType,
		SizeBytes: int64(len(input.Content)),
		Width:     metadata.width,
		Height:    metadata.height,
		S3Key:     key,
	})
	if err != nil {
		return model.Avatar{}, fmt.Errorf("create avatar metadata: %w", err)
	}

	if err := s.storage.Put(
		ctx,
		key,
		bytes.NewReader(input.Content),
		int64(len(input.Content)),
		metadata.mimeType,
	); err != nil {
		statusErr := s.repository.UpdateUploadStatus(ctx, avatar.ID, model.UploadStatusFailed)
		if statusErr != nil {
			return model.Avatar{}, errors.Join(
				fmt.Errorf("store avatar original: %w", err),
				fmt.Errorf("mark avatar upload failed: %w", statusErr),
			)
		}

		return model.Avatar{}, fmt.Errorf("store avatar original: %w", err)
	}

	if err := s.repository.UpdateUploadStatus(ctx, avatar.ID, model.UploadStatusCompleted); err != nil {
		deleteErr := s.storage.Delete(ctx, key)
		statusErr := s.repository.UpdateUploadStatus(ctx, avatar.ID, model.UploadStatusFailed)

		resultErr := fmt.Errorf("complete avatar upload: %w", err)
		if deleteErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("delete avatar original after upload failure: %w", deleteErr))
		}
		if statusErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("mark avatar upload failed: %w", statusErr))
		}

		return model.Avatar{}, resultErr
	}

	avatar.UploadStatus = model.UploadStatusCompleted

	if err := s.publisher.PublishAvatarUploaded(ctx, avatar); err != nil {
		deleteOriginalErr := s.storage.Delete(ctx, key)
		deleteMetadataErr := s.repository.DeletePermanent(ctx, avatar.ID)

		resultErr := errors.Join(
			ErrServiceUnavailable,
			fmt.Errorf("publish avatar uploaded event: %w", err),
		)
		if deleteOriginalErr != nil {
			resultErr = errors.Join(
				resultErr,
				fmt.Errorf("delete avatar original after publish failure: %w", deleteOriginalErr),
			)
		}
		if deleteMetadataErr != nil {
			resultErr = errors.Join(
				resultErr,
				fmt.Errorf("delete avatar metadata after publish failure: %w", deleteMetadataErr),
			)
		}

		return model.Avatar{}, resultErr
	}

	return avatar, nil
}
