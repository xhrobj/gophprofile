// Package worker выполняет асинхронную обработку аватаров.
package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"

	"github.com/xhrobj/gophprofile/internal/broker"
	"github.com/xhrobj/gophprofile/internal/event"
	"github.com/xhrobj/gophprofile/internal/imageprocessor"
	"github.com/xhrobj/gophprofile/internal/logger"
	"github.com/xhrobj/gophprofile/internal/model"
	"github.com/xhrobj/gophprofile/internal/s3"
)

const (
	workerInstrumentationName = "github.com/xhrobj/gophprofile/internal/worker"
	maxRetryAttempts          = 3
	initialRetryBackoff       = 250 * time.Millisecond
	recoveryTimeout           = 2 * time.Second
)

// Repository хранит состояние фоновой обработки аватаров.
type Repository interface {
	ClaimForProcessing(ctx context.Context, avatarID, messageID string, redelivered bool) (bool, error)
	CompleteProcessing(ctx context.Context, avatarID string, thumbnailS3Keys map[model.ThumbnailSize]string) error
	UpdateProcessingStatus(ctx context.Context, avatarID string, status model.ProcessingStatus) error
}

// Storage предоставляет Worker доступ к объектам в S3.
type Storage interface {
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Put(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error
	Delete(ctx context.Context, keys ...string) error
}

// ImageProcessor создаёт миниатюры из оригинального изображения.
type ImageProcessor interface {
	Process(reader io.Reader) ([]imageprocessor.Thumbnail, error)
}

type retryPolicy struct {
	maxAttempts    int
	initialBackoff time.Duration
}

// Worker получает события из broker и обрабатывает аватары.
type Worker struct {
	consumer       broker.Consumer
	repository     Repository
	storage        Storage
	imageProcessor ImageProcessor
	logger         *slog.Logger
	retry          retryPolicy
}

// New создаёт Worker с bounded retry и экспоненциальным backoff.
func New(
	consumer broker.Consumer,
	repository Repository,
	storage Storage,
	imageProcessor ImageProcessor,
	lg *slog.Logger,
) *Worker {
	return &Worker{
		consumer:       consumer,
		repository:     repository,
		storage:        storage,
		imageProcessor: imageProcessor,
		logger:         lg,
		retry: retryPolicy{
			maxAttempts:    maxRetryAttempts,
			initialBackoff: initialRetryBackoff,
		},
	}
}

// Run получает сообщения до отмены контекста или неожиданного закрытия consumer.
func (w *Worker) Run(ctx context.Context) error {
	deliveries, err := w.consumer.Consume(ctx)
	if err != nil {
		return fmt.Errorf("start broker consumer: %w", err)
	}

	w.logger.InfoContext(ctx, "worker started")
	defer w.logger.InfoContext(ctx, "worker stopped")

	for item := range deliveries {
		if err := w.handleDelivery(ctx, item); err != nil {
			if ctx.Err() != nil {
				return nil
			}

			return err
		}
	}

	if ctx.Err() != nil {
		return nil
	}

	return errors.New("broker deliveries channel closed unexpectedly")
}

func (w *Worker) handleDelivery(ctx context.Context, item broker.Delivery) error {
	switch item.RoutingKey() {
	case event.AvatarUploadedRoutingKey:
		return w.handleAvatarUploadedDelivery(ctx, item)
	case event.AvatarDeletedRoutingKey:
		return w.handleAvatarDeletedDelivery(ctx, item)
	default:
		return w.rejectInvalidMessage(ctx, item, fmt.Errorf("unsupported routing key %q", item.RoutingKey()))
	}
}

func (w *Worker) handleAvatarUploadedDelivery(ctx context.Context, item broker.Delivery) error {
	message, err := decodeAvatarUploaded(item)
	if err != nil {
		return w.rejectInvalidMessage(ctx, item, err)
	}

	ctx, span := otel.Tracer(workerInstrumentationName).Start(ctx, "process uploaded avatar")
	defer span.End()

	messageLogger := logger.WithMessageID(w.logger, message.MessageID).With(
		slog.String("avatar_id", message.AvatarID),
		slog.String("user_id", message.UserID),
	)

	claimed, err := w.claimWithRetry(ctx, messageLogger, message.AvatarID, message.MessageID, item.Redelivered())
	if err != nil {
		return w.handleAvatarClaimError(ctx, item, messageLogger, err)
	}
	if !claimed {
		return w.ackDuplicateAvatarUpload(ctx, item, messageLogger)
	}

	if err := w.processWithRetry(ctx, messageLogger, message); err != nil {
		return w.handleAvatarProcessingError(ctx, item, messageLogger, message, err)
	}

	if ackErr := item.Ack(); ackErr != nil {
		return fmt.Errorf("ack %s message: %w", event.AvatarUploadedRoutingKey, ackErr)
	}

	messageLogger.InfoContext(ctx, "avatar processing completed")

	return nil
}

func (w *Worker) handleAvatarClaimError(
	ctx context.Context,
	item broker.Delivery,
	lg *slog.Logger,
	claimErr error,
) error {
	if ctx.Err() != nil {
		return nil
	}

	lg.ErrorContext(ctx, "failed to claim avatar for processing", slog.Any("error", claimErr))
	if nackErr := item.Nack(false); nackErr != nil {
		return fmt.Errorf("dead-letter unclaimed %s message: %w", event.AvatarUploadedRoutingKey, nackErr)
	}

	return nil
}

func (w *Worker) ackDuplicateAvatarUpload(ctx context.Context, item broker.Delivery, lg *slog.Logger) error {
	lg.InfoContext(ctx, "avatar already claimed, processed or deleted; acknowledging duplicate message")

	if ackErr := item.Ack(); ackErr != nil {
		return fmt.Errorf("ack duplicate %s message: %w", event.AvatarUploadedRoutingKey, ackErr)
	}

	return nil
}

func (w *Worker) handleAvatarProcessingError(
	ctx context.Context,
	item broker.Delivery,
	lg *slog.Logger,
	message event.AvatarUploaded,
	processErr error,
) error {
	if ctx.Err() != nil {
		return w.requeueOnShutdown(ctx, item, lg, message.AvatarID)
	}
	if errors.Is(processErr, model.ErrAvatarNotFound) {
		return w.finishDeletedAvatarProcessing(ctx, item, lg, message)
	}

	lg.ErrorContext(ctx, "avatar processing failed", slog.Any("error", processErr))
	w.finalizeFailure(ctx, lg, message)

	if nackErr := item.Nack(false); nackErr != nil {
		return fmt.Errorf("dead-letter failed %s message: %w", event.AvatarUploadedRoutingKey, nackErr)
	}

	return nil
}

func (w *Worker) finishDeletedAvatarProcessing(
	ctx context.Context,
	item broker.Delivery,
	lg *slog.Logger,
	message event.AvatarUploaded,
) error {
	lg.InfoContext(ctx, "avatar was deleted during processing; cleaning up thumbnails")
	if cleanupErr := w.deleteKeysWithRetry(ctx, lg, thumbnailKeys(message)...); cleanupErr != nil {
		lg.ErrorContext(ctx, "failed to clean up thumbnails for deleted avatar", slog.Any("error", cleanupErr))
		if nackErr := item.Nack(false); nackErr != nil {
			return fmt.Errorf(
				"dead-letter deleted %s message after cleanup failure: %w",
				event.AvatarUploadedRoutingKey,
				nackErr,
			)
		}

		return nil
	}

	if ackErr := item.Ack(); ackErr != nil {
		return fmt.Errorf("ack deleted %s message: %w", event.AvatarUploadedRoutingKey, ackErr)
	}

	return nil
}

func (w *Worker) handleAvatarDeletedDelivery(ctx context.Context, item broker.Delivery) error {
	message, err := decodeAvatarDeleted(item)
	if err != nil {
		return w.rejectInvalidMessage(ctx, item, err)
	}

	ctx, span := otel.Tracer(workerInstrumentationName).Start(ctx, "process deleted avatar")
	defer span.End()

	messageLogger := logger.WithMessageID(w.logger, message.MessageID).With(
		slog.String("avatar_id", message.AvatarID),
	)

	if err := w.deleteKeysWithRetry(ctx, messageLogger, message.S3Keys...); err != nil {
		if ctx.Err() != nil {
			if nackErr := item.Nack(true); nackErr != nil {
				return fmt.Errorf("requeue %s message during shutdown: %w", event.AvatarDeletedRoutingKey, nackErr)
			}

			return nil
		}

		messageLogger.ErrorContext(ctx, "avatar file deletion failed", slog.Any("error", err))
		if nackErr := item.Nack(false); nackErr != nil {
			return fmt.Errorf("dead-letter failed %s message: %w", event.AvatarDeletedRoutingKey, nackErr)
		}

		return nil
	}

	if ackErr := item.Ack(); ackErr != nil {
		return fmt.Errorf("ack %s message: %w", event.AvatarDeletedRoutingKey, ackErr)
	}

	messageLogger.InfoContext(ctx, "avatar files deleted")

	return nil
}

func (w *Worker) rejectInvalidMessage(ctx context.Context, item broker.Delivery, err error) error {
	w.logger.WarnContext(ctx,
		"rejecting invalid broker message",
		slog.String("message_id", item.MessageID()),
		slog.String("routing_key", item.RoutingKey()),
		slog.Any("error", err),
	)

	if nackErr := item.Nack(false); nackErr != nil {
		return fmt.Errorf("reject invalid broker message: %w", nackErr)
	}

	return nil
}

func (w *Worker) claimWithRetry(
	ctx context.Context,
	lg *slog.Logger,
	avatarID, messageID string,
	redelivered bool,
) (bool, error) {
	var claimed bool

	err := w.retryOperation(ctx, lg, "claim avatar", func() error {
		var claimErr error
		claimed, claimErr = w.repository.ClaimForProcessing(ctx, avatarID, messageID, redelivered)

		return claimErr
	}, nil)

	return claimed, err
}

func (w *Worker) processWithRetry(ctx context.Context, lg *slog.Logger, message event.AvatarUploaded) error {
	return w.retryOperation(ctx, lg, "avatar processing", func() error {
		return w.processOnce(ctx, message)
	}, func(err error) bool {
		return !errors.Is(err, imageprocessor.ErrInvalidImage) && !errors.Is(err, model.ErrAvatarNotFound)
	})
}

func (w *Worker) processOnce(ctx context.Context, message event.AvatarUploaded) error {
	original, err := w.storage.Get(ctx, message.S3Key)
	if err != nil {
		return fmt.Errorf("download original: %w", err)
	}
	defer func() {
		_ = original.Close()
	}()

	thumbnails, err := w.imageProcessor.Process(original)
	if err != nil {
		return fmt.Errorf("create thumbnails: %w", err)
	}

	thumbnailS3Keys := make(map[model.ThumbnailSize]string, len(thumbnails))
	for _, thumbnail := range thumbnails {
		key := s3.ThumbnailKey(message.UserID, message.AvatarID, thumbnail.Size)
		if err := w.storage.Put(
			ctx,
			key,
			bytes.NewReader(thumbnail.Content),
			int64(len(thumbnail.Content)),
			thumbnail.ContentType,
		); err != nil {
			return fmt.Errorf("upload %s thumbnail: %w", thumbnail.Size, err)
		}

		thumbnailS3Keys[thumbnail.Size] = key
	}

	if err := w.repository.CompleteProcessing(ctx, message.AvatarID, thumbnailS3Keys); err != nil {
		return fmt.Errorf("complete avatar processing: %w", err)
	}

	return nil
}

func (w *Worker) deleteKeysWithRetry(ctx context.Context, lg *slog.Logger, keys ...string) error {
	return w.retryOperation(ctx, lg, "delete avatar files", func() error {
		return w.storage.Delete(ctx, keys...)
	}, nil)
}

func (w *Worker) finalizeFailure(ctx context.Context, lg *slog.Logger, message event.AvatarUploaded) {
	statusCtx, cancelStatus := context.WithTimeout(context.Background(), recoveryTimeout)
	if err := w.updateProcessingStatusWithRetry(
		statusCtx,
		lg,
		message.AvatarID,
		model.ProcessingStatusFailed,
	); err != nil {
		lg.ErrorContext(ctx, "failed to mark avatar processing as failed", slog.Any("error", err))
	}
	cancelStatus()

	cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), recoveryTimeout)
	defer cancelCleanup()

	if err := w.deleteKeysWithRetry(cleanupCtx, lg, thumbnailKeys(message)...); err != nil {
		lg.WarnContext(ctx, "failed to clean up thumbnails after processing error", slog.Any("error", err))
	}
}

func (w *Worker) requeueOnShutdown(
	ctx context.Context,
	item broker.Delivery,
	lg *slog.Logger,
	avatarID string,
) error {
	recoveryCtx, cancel := context.WithTimeout(context.Background(), recoveryTimeout)
	defer cancel()

	if err := w.updateProcessingStatusWithRetry(
		recoveryCtx,
		lg,
		avatarID,
		model.ProcessingStatusPending,
	); err != nil {
		lg.ErrorContext(ctx, "failed to release avatar claim during shutdown; dead-lettering message", slog.Any("error", err))

		if nackErr := item.Nack(false); nackErr != nil {
			return fmt.Errorf("dead-letter %s message after failed shutdown recovery: %w", event.AvatarUploadedRoutingKey, nackErr)
		}

		return nil
	}

	lg.InfoContext(ctx, "released avatar claim during shutdown")
	if nackErr := item.Nack(true); nackErr != nil {
		return fmt.Errorf("requeue %s message during shutdown: %w", event.AvatarUploadedRoutingKey, nackErr)
	}

	return nil
}

func (w *Worker) updateProcessingStatusWithRetry(
	ctx context.Context,
	lg *slog.Logger,
	avatarID string,
	status model.ProcessingStatus,
) error {
	retryLogger := lg.With(slog.String("status", string(status)))

	return w.retryOperation(ctx, retryLogger, "update avatar processing status", func() error {
		return w.repository.UpdateProcessingStatus(ctx, avatarID, status)
	}, nil)
}

func (w *Worker) retryOperation(
	ctx context.Context,
	lg *slog.Logger,
	operation string,
	action func() error,
	shouldRetry func(error) bool,
) error {
	var resultErr error

	for attempt := 1; attempt <= w.retry.maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		resultErr = action()
		if resultErr == nil {
			return nil
		}
		if attempt == w.retry.maxAttempts || shouldRetry != nil && !shouldRetry(resultErr) {
			return resultErr
		}

		lg.WarnContext(ctx,
			operation+" attempt failed",
			slog.Int("attempt", attempt),
			slog.Int("max_attempts", w.retry.maxAttempts),
			slog.Any("error", resultErr),
		)

		if err := waitRetry(ctx, w.retry.backoff(attempt)); err != nil {
			return err
		}
	}

	return resultErr
}

func (p retryPolicy) backoff(failedAttempt int) time.Duration {
	return p.initialBackoff * time.Duration(1<<(failedAttempt-1))
}

func thumbnailKeys(message event.AvatarUploaded) []string {
	return []string{
		s3.ThumbnailKey(message.UserID, message.AvatarID, model.ThumbnailSize100x100),
		s3.ThumbnailKey(message.UserID, message.AvatarID, model.ThumbnailSize300x300),
	}
}

func decodeAvatarUploaded(item broker.Delivery) (event.AvatarUploaded, error) {
	if item.RoutingKey() != event.AvatarUploadedRoutingKey {
		return event.AvatarUploaded{}, fmt.Errorf("unsupported routing key %q", item.RoutingKey())
	}

	var message event.AvatarUploaded
	if err := json.Unmarshal(item.Body(), &message); err != nil {
		return event.AvatarUploaded{}, fmt.Errorf("decode %s event: %w", event.AvatarUploadedRoutingKey, err)
	}

	if message.SchemaVersion != event.AvatarUploadedSchemaVersion {
		return event.AvatarUploaded{}, fmt.Errorf("unsupported schema version %d", message.SchemaVersion)
	}
	if err := uuid.Validate(message.MessageID); err != nil {
		return event.AvatarUploaded{}, fmt.Errorf("invalid message_id: %w", err)
	}
	if err := uuid.Validate(message.AvatarID); err != nil {
		return event.AvatarUploaded{}, fmt.Errorf("invalid avatar_id: %w", err)
	}
	if strings.TrimSpace(message.UserID) == "" {
		return event.AvatarUploaded{}, errors.New("user_id must not be empty")
	}
	if strings.TrimSpace(message.S3Key) == "" {
		return event.AvatarUploaded{}, errors.New("s3_key must not be empty")
	}
	if message.CreatedAt.IsZero() {
		return event.AvatarUploaded{}, errors.New("created_at must not be zero")
	}

	return message, nil
}

func decodeAvatarDeleted(item broker.Delivery) (event.AvatarDeleted, error) {
	if item.RoutingKey() != event.AvatarDeletedRoutingKey {
		return event.AvatarDeleted{}, fmt.Errorf("unsupported routing key %q", item.RoutingKey())
	}

	var message event.AvatarDeleted
	if err := json.Unmarshal(item.Body(), &message); err != nil {
		return event.AvatarDeleted{}, fmt.Errorf("decode %s event: %w", event.AvatarDeletedRoutingKey, err)
	}

	if message.SchemaVersion != event.AvatarDeletedSchemaVersion {
		return event.AvatarDeleted{}, fmt.Errorf("unsupported schema version %d", message.SchemaVersion)
	}
	if err := uuid.Validate(message.MessageID); err != nil {
		return event.AvatarDeleted{}, fmt.Errorf("invalid message_id: %w", err)
	}
	if err := uuid.Validate(message.AvatarID); err != nil {
		return event.AvatarDeleted{}, fmt.Errorf("invalid avatar_id: %w", err)
	}
	if len(message.S3Keys) == 0 {
		return event.AvatarDeleted{}, errors.New("s3_keys must not be empty")
	}
	for _, key := range message.S3Keys {
		if strings.TrimSpace(key) == "" {
			return event.AvatarDeleted{}, errors.New("s3_keys must not contain empty values")
		}
	}
	if message.CreatedAt.IsZero() {
		return event.AvatarDeleted{}, errors.New("created_at must not be zero")
	}

	return message, nil
}

func waitRetry(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
