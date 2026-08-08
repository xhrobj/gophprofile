package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"go.opentelemetry.io/otel"

	"github.com/xhrobj/gophprofile/internal/model"
)

const (
	serviceInstrumentationName = "github.com/xhrobj/gophprofile/internal/service"
	uploadRecoveryTimeout      = 2 * time.Second
)

// AvatarRepository описывает операции с метаданными аватаров, необходимые application-сервису.
type AvatarRepository interface {
	// Create сохраняет метаданные новой аватарки.
	Create(ctx context.Context, avatar model.Avatar) (model.Avatar, error)

	// GetByID возвращает аватарку по идентификатору.
	GetByID(ctx context.Context, avatarID string) (model.Avatar, error)

	// GetCurrentByUserID возвращает текущую аватарку пользователя.
	GetCurrentByUserID(ctx context.Context, userID string) (model.Avatar, error)

	// ListByUserID возвращает список аватарок пользователя.
	ListByUserID(ctx context.Context, userID string) ([]model.Avatar, error)

	// UpdateUploadStatus обновляет статус загрузки аватарки.
	UpdateUploadStatus(ctx context.Context, avatarID string, status model.UploadStatus) error

	// DeletePermanent безвозвратно удаляет метаданные аватарки.
	DeletePermanent(ctx context.Context, avatarID string) error

	// SoftDelete помечает аватарку удалённой.
	SoftDelete(ctx context.Context, avatarID string) error

	// RestoreDeleted восстанавливает ранее удалённую аватарку.
	RestoreDeleted(ctx context.Context, avatarID string) error
}

// AvatarStorage описывает операции с объектным хранилищем, необходимые application-сервису.
type AvatarStorage interface {
	// Put сохраняет объект в хранилище.
	Put(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error

	// Get открывает объект из хранилища для чтения.
	Get(ctx context.Context, key string) (io.ReadCloser, error)

	// Delete удаляет объекты из хранилища по ключам.
	Delete(ctx context.Context, keys ...string) error
}

// AvatarEventPublisher публикует события, необходимые application-сервису.
type AvatarEventPublisher interface {
	// PublishAvatarUploaded публикует событие об успешной загрузке аватарки.
	PublishAvatarUploaded(ctx context.Context, avatar model.Avatar) error

	// PublishAvatarDeleted публикует событие об удалении аватарки.
	PublishAvatarDeleted(ctx context.Context, avatar model.Avatar) error
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
func (s *AvatarService) Upload(ctx context.Context, input UploadInput) (result model.Avatar, resultErr error) {
	ctx, span := otel.Tracer(serviceInstrumentationName).Start(ctx, "upload avatar")
	defer func() {
		finishSpan(span, resultErr)
	}()

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
		return model.Avatar{}, s.recoverStoreFailure(avatar.ID, err)
	}

	if err := s.repository.UpdateUploadStatus(ctx, avatar.ID, model.UploadStatusCompleted); err != nil {
		return model.Avatar{}, s.recoverUploadCompletionFailure(avatar.ID, key, err)
	}

	avatar.UploadStatus = model.UploadStatusCompleted

	if err := s.publisher.PublishAvatarUploaded(ctx, avatar); err != nil {
		return model.Avatar{}, s.rollbackPublishFailure(avatar.ID, key, err)
	}

	return avatar, nil
}

// recoverStoreFailure помечает загрузку неуспешной после ошибки сохранения оригинала
func (s *AvatarService) recoverStoreFailure(avatarID string, cause error) error {
	resultErr := fmt.Errorf("store avatar original: %w", cause)

	statusCtx, cancelStatus := context.WithTimeout(context.Background(), uploadRecoveryTimeout)
	statusErr := s.repository.UpdateUploadStatus(statusCtx, avatarID, model.UploadStatusFailed)
	cancelStatus()
	if statusErr != nil {
		return errors.Join(
			resultErr,
			fmt.Errorf("mark avatar upload failed: %w", statusErr),
		)
	}

	return resultErr
}

// recoverUploadCompletionFailure помечает загрузку неуспешной и удаляет сохранённый оригинал
func (s *AvatarService) recoverUploadCompletionFailure(avatarID, key string, cause error) error {
	resultErr := fmt.Errorf("complete avatar upload: %w", cause)

	statusCtx, cancelStatus := context.WithTimeout(context.Background(), uploadRecoveryTimeout)
	statusErr := s.repository.UpdateUploadStatus(statusCtx, avatarID, model.UploadStatusFailed)
	cancelStatus()
	if statusErr != nil {
		return errors.Join(
			resultErr,
			fmt.Errorf("mark avatar upload failed: %w", statusErr),
		)
	}

	cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), uploadRecoveryTimeout)
	deleteErr := s.storage.Delete(cleanupCtx, key)
	cancelCleanup()
	if deleteErr != nil {
		resultErr = errors.Join(resultErr, fmt.Errorf("delete avatar original after upload failure: %w", deleteErr))
	}

	return resultErr
}

// rollbackPublishFailure откатывает метаданные и оригинал после ошибки публикации события
func (s *AvatarService) rollbackPublishFailure(avatarID, key string, cause error) error {
	resultErr := errors.Join(
		ErrServiceUnavailable,
		fmt.Errorf("publish avatar uploaded event: %w", cause),
	)

	metadataCtx, cancelMetadata := context.WithTimeout(context.Background(), uploadRecoveryTimeout)
	deleteMetadataErr := s.repository.DeletePermanent(metadataCtx, avatarID)
	cancelMetadata()
	if deleteMetadataErr != nil {
		return errors.Join(
			resultErr,
			fmt.Errorf("delete avatar metadata after publish failure: %w", deleteMetadataErr),
		)
	}

	cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), uploadRecoveryTimeout)
	deleteOriginalErr := s.storage.Delete(cleanupCtx, key)
	cancelCleanup()
	if deleteOriginalErr != nil {
		resultErr = errors.Join(
			resultErr,
			fmt.Errorf("delete avatar original after publish failure: %w", deleteOriginalErr),
		)
	}

	return resultErr
}
