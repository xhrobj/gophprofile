package service

import (
	"context"
	"fmt"

	"github.com/xhrobj/gophprofile/internal/model"
)

// GetMetadata возвращает метаданные неудалённой аватарки по идентификатору.
func (s *AvatarService) GetMetadata(ctx context.Context, avatarID string) (result model.Avatar, resultErr error) {
	defer func() {
		resultErr = normalizeDependencyError(resultErr)
	}()

	avatar, err := s.repository.GetByID(ctx, avatarID)
	if err != nil {
		return model.Avatar{}, fmt.Errorf("get avatar metadata: %w", err)
	}

	return avatar, nil
}

// ListByUserID возвращает неудалённые аватарки пользователя от новых к старым.
func (s *AvatarService) ListByUserID(ctx context.Context, userID string) (result []model.Avatar, resultErr error) {
	defer func() {
		resultErr = normalizeDependencyError(resultErr)
	}()

	avatars, err := s.repository.ListByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list user avatars: %w", err)
	}

	return avatars, nil
}
