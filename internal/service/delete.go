package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"

	"github.com/xhrobj/gophprofile/internal/model"
)

const deleteRollbackTimeout = 2 * time.Second

// DeleteByID мягко удаляет аватарку по идентификатору и ставит очистку файлов в очередь.
func (s *AvatarService) DeleteByID(ctx context.Context, avatarID, requesterUserID string) (resultErr error) {
	ctx, span := otel.Tracer(serviceInstrumentationName).Start(ctx, "delete avatar")
	defer func() {
		finishSpan(span, resultErr)
		if s.metrics != nil {
			s.metrics.ObserveAvatarDeletion(resultErr == nil)
		}
	}()

	avatar, err := s.repository.GetByID(ctx, avatarID)
	if err != nil {
		return fmt.Errorf("get avatar for deletion: %w", err)
	}

	return s.deleteAvatar(ctx, avatar, requesterUserID)
}

// DeleteCurrentByUserID мягко удаляет актуальную аватарку пользователя и ставит очистку файлов в очередь.
func (s *AvatarService) DeleteCurrentByUserID(ctx context.Context, userID, requesterUserID string) (resultErr error) {
	ctx, span := otel.Tracer(serviceInstrumentationName).Start(ctx, "delete avatar")
	defer func() {
		finishSpan(span, resultErr)
		if s.metrics != nil {
			s.metrics.ObserveAvatarDeletion(resultErr == nil)
		}
	}()

	if userID != requesterUserID {
		return ErrForbidden
	}

	avatar, err := s.repository.GetCurrentByUserID(ctx, userID)
	if err != nil {
		return fmt.Errorf("get current avatar for deletion: %w", err)
	}

	return s.deleteAvatar(ctx, avatar, requesterUserID)
}

func (s *AvatarService) deleteAvatar(ctx context.Context, avatar model.Avatar, requesterUserID string) error {
	if avatar.UserID != requesterUserID {
		return ErrForbidden
	}

	if err := s.repository.SoftDelete(ctx, avatar.ID); err != nil {
		return fmt.Errorf("soft delete avatar: %w", err)
	}

	if err := s.publisher.PublishAvatarDeleted(ctx, avatar); err != nil {
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), deleteRollbackTimeout)
		defer cancel()

		rollbackErr := s.repository.RestoreDeleted(rollbackCtx, avatar.ID)
		resultErr := errors.Join(
			ErrServiceUnavailable,
			fmt.Errorf("publish avatar deleted event: %w", err),
		)
		if rollbackErr != nil {
			resultErr = errors.Join(
				resultErr,
				fmt.Errorf("restore avatar after publish failure: %w", rollbackErr),
			)
		}

		return resultErr
	}

	return nil
}
