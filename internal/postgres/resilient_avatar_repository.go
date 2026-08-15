package postgres

import (
	"context"
	"log/slog"

	"github.com/xhrobj/gophprofile/internal/model"
	"github.com/xhrobj/gophprofile/internal/resilience"
)

// ResilientAvatarRepository защищает PostgreSQL-операции аватаров общим circuit breaker процесса.
type ResilientAvatarRepository struct {
	base    *AvatarRepository
	breaker *resilience.CircuitBreaker
}

// NewResilientAvatarRepository добавляет circuit breaker к PostgreSQL-репозиторию.
func NewResilientAvatarRepository(base *AvatarRepository, lg *slog.Logger) *ResilientAvatarRepository {
	return &ResilientAvatarRepository{
		base:    base,
		breaker: resilience.NewCircuitBreaker("postgresql", lg, model.ErrAvatarNotFound),
	}
}

func (r *ResilientAvatarRepository) Create(ctx context.Context, avatar model.Avatar) (model.Avatar, error) {
	return resilience.Execute(r.breaker, func() (model.Avatar, error) {
		return r.base.Create(ctx, avatar)
	})
}

func (r *ResilientAvatarRepository) GetByID(ctx context.Context, avatarID string) (model.Avatar, error) {
	return resilience.Execute(r.breaker, func() (model.Avatar, error) {
		return r.base.GetByID(ctx, avatarID)
	})
}

func (r *ResilientAvatarRepository) GetCurrentByUserID(ctx context.Context, userID string) (model.Avatar, error) {
	return resilience.Execute(r.breaker, func() (model.Avatar, error) {
		return r.base.GetCurrentByUserID(ctx, userID)
	})
}

func (r *ResilientAvatarRepository) ListByUserID(ctx context.Context, userID string) ([]model.Avatar, error) {
	return resilience.Execute(r.breaker, func() ([]model.Avatar, error) {
		return r.base.ListByUserID(ctx, userID)
	})
}

func (r *ResilientAvatarRepository) UpdateUploadStatus(
	ctx context.Context,
	avatarID string,
	status model.UploadStatus,
) error {
	return resilience.Do(r.breaker, func() error {
		return r.base.UpdateUploadStatus(ctx, avatarID, status)
	})
}

func (r *ResilientAvatarRepository) UpdateProcessingStatus(
	ctx context.Context,
	avatarID string,
	status model.ProcessingStatus,
) error {
	return resilience.Do(r.breaker, func() error {
		return r.base.UpdateProcessingStatus(ctx, avatarID, status)
	})
}

func (r *ResilientAvatarRepository) CompleteProcessing(
	ctx context.Context,
	avatarID string,
	thumbnailS3Keys map[model.ThumbnailSize]string,
) error {
	return resilience.Do(r.breaker, func() error {
		return r.base.CompleteProcessing(ctx, avatarID, thumbnailS3Keys)
	})
}

func (r *ResilientAvatarRepository) DeletePermanent(ctx context.Context, avatarID string) error {
	return resilience.Do(r.breaker, func() error {
		return r.base.DeletePermanent(ctx, avatarID)
	})
}

func (r *ResilientAvatarRepository) SoftDelete(ctx context.Context, avatarID string) error {
	return resilience.Do(r.breaker, func() error {
		return r.base.SoftDelete(ctx, avatarID)
	})
}

func (r *ResilientAvatarRepository) RestoreDeleted(ctx context.Context, avatarID string) error {
	return resilience.Do(r.breaker, func() error {
		return r.base.RestoreDeleted(ctx, avatarID)
	})
}

func (r *ResilientAvatarRepository) StorageUsageBytes(ctx context.Context) (int64, error) {
	return resilience.Execute(r.breaker, func() (int64, error) {
		return r.base.StorageUsageBytes(ctx)
	})
}

func (r *ResilientAvatarRepository) ClaimForProcessing(
	ctx context.Context,
	avatarID, messageID string,
	redelivered bool,
) (bool, error) {
	return resilience.Execute(r.breaker, func() (bool, error) {
		return r.base.ClaimForProcessing(ctx, avatarID, messageID, redelivered)
	})
}
