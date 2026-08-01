package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/xhrobj/gophprofile/internal/model"
)

// AvatarRepository является PostgreSQL-адаптером репозитория аватаров.
type AvatarRepository struct {
	pool *pgxpool.Pool
}

// NewAvatarRepository создаёт PostgreSQL-адаптер репозитория аватаров.
func NewAvatarRepository(pool *pgxpool.Pool) *AvatarRepository {
	return &AvatarRepository{pool: pool}
}

// Create сохраняет метаданные новой аватарки и возвращает состояние, зафиксированное PostgreSQL.
func (r *AvatarRepository) Create(ctx context.Context, avatar model.Avatar) (model.Avatar, error) {
	thumbnailS3Keys, err := marshalThumbnailS3Keys(avatar.ThumbnailS3Keys)
	if err != nil {
		return model.Avatar{}, fmt.Errorf("create avatar: %w", err)
	}

	created, err := scanAvatar(r.pool.QueryRow(
		ctx,
		`INSERT INTO avatars (
			id,
			user_id,
			file_name,
			mime_type,
			size_bytes,
			width,
			height,
			s3_key,
			thumbnail_s3_keys
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id::text, user_id, file_name, mime_type, size_bytes, width, height,
			s3_key, thumbnail_s3_keys, upload_status, processing_status,
			created_at, updated_at, deleted_at`,
		avatar.ID,
		avatar.UserID,
		avatar.FileName,
		avatar.MIMEType,
		avatar.SizeBytes,
		avatar.Width,
		avatar.Height,
		avatar.S3Key,
		thumbnailS3Keys,
	))
	if err != nil {
		return model.Avatar{}, fmt.Errorf("create avatar: %w", err)
	}

	return created, nil
}

// GetByID возвращает неудалённую аватарку по идентификатору.
func (r *AvatarRepository) GetByID(ctx context.Context, avatarID string) (model.Avatar, error) {
	avatar, err := scanAvatar(r.pool.QueryRow(
		ctx,
		`SELECT id::text, user_id, file_name, mime_type, size_bytes, width, height,
			s3_key, thumbnail_s3_keys, upload_status, processing_status,
			created_at, updated_at, deleted_at
		FROM avatars
		WHERE id = $1 AND deleted_at IS NULL`,
		avatarID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Avatar{}, fmt.Errorf("get avatar: %w", model.ErrAvatarNotFound)
	}
	if err != nil {
		return model.Avatar{}, fmt.Errorf("get avatar: %w", err)
	}

	return avatar, nil
}

// GetCurrentByUserID возвращает последнюю успешно загруженную неудалённую аватарку пользователя.
func (r *AvatarRepository) GetCurrentByUserID(ctx context.Context, userID string) (model.Avatar, error) {
	avatar, err := scanAvatar(r.pool.QueryRow(
		ctx,
		`SELECT id::text, user_id, file_name, mime_type, size_bytes, width, height,
			s3_key, thumbnail_s3_keys, upload_status, processing_status,
			created_at, updated_at, deleted_at
		FROM avatars
		WHERE user_id = $1
			AND deleted_at IS NULL
			AND upload_status = 'completed'
		ORDER BY created_at DESC, id DESC
		LIMIT 1`,
		userID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Avatar{}, fmt.Errorf("get current user avatar: %w", model.ErrAvatarNotFound)
	}
	if err != nil {
		return model.Avatar{}, fmt.Errorf("get current user avatar: %w", err)
	}

	return avatar, nil
}

// ListByUserID возвращает неудалённые аватарки пользователя от новых к старым.
func (r *AvatarRepository) ListByUserID(ctx context.Context, userID string) ([]model.Avatar, error) {
	rows, err := r.pool.Query(
		ctx,
		`SELECT id::text, user_id, file_name, mime_type, size_bytes, width, height,
			s3_key, thumbnail_s3_keys, upload_status, processing_status,
			created_at, updated_at, deleted_at
		FROM avatars
		WHERE user_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC, id DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list user avatars: %w", err)
	}
	defer rows.Close()

	avatars := make([]model.Avatar, 0)
	for rows.Next() {
		avatar, scanErr := scanAvatar(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan user avatar: %w", scanErr)
		}

		avatars = append(avatars, avatar)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate user avatars: %w", err)
	}

	return avatars, nil
}

// UpdateUploadStatus изменяет статус загрузки оригинала аватарки.
func (r *AvatarRepository) UpdateUploadStatus(
	ctx context.Context,
	avatarID string,
	status model.UploadStatus,
) error {
	return r.updateStatus(
		ctx,
		`UPDATE avatars
		SET upload_status = $2, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND deleted_at IS NULL`,
		avatarID,
		string(status),
		"update avatar upload status",
	)
}

// UpdateProcessingStatus изменяет статус фоновой обработки аватарки.
func (r *AvatarRepository) UpdateProcessingStatus(
	ctx context.Context,
	avatarID string,
	status model.ProcessingStatus,
) error {
	return r.updateStatus(
		ctx,
		`UPDATE avatars
		SET processing_status = $2, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND deleted_at IS NULL`,
		avatarID,
		string(status),
		"update avatar processing status",
	)
}

// CompleteProcessing атомарно сохраняет ключи миниатюр и завершает обработку аватарки.
func (r *AvatarRepository) CompleteProcessing(
	ctx context.Context,
	avatarID string,
	thumbnailS3Keys map[model.ThumbnailSize]string,
) error {
	encodedKeys, err := marshalThumbnailS3Keys(thumbnailS3Keys)
	if err != nil {
		return fmt.Errorf("complete avatar processing: %w", err)
	}

	commandTag, err := r.pool.Exec(
		ctx,
		`UPDATE avatars
		SET thumbnail_s3_keys = $2,
			processing_status = 'completed',
			updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND deleted_at IS NULL`,
		avatarID,
		encodedKeys,
	)
	if err != nil {
		return fmt.Errorf("complete avatar processing: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("complete avatar processing: %w", model.ErrAvatarNotFound)
	}

	return nil
}

// SoftDelete помечает аватарку удалённой, не удаляя запись физически.
func (r *AvatarRepository) SoftDelete(ctx context.Context, avatarID string) error {
	commandTag, err := r.pool.Exec(
		ctx,
		`UPDATE avatars
		SET deleted_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND deleted_at IS NULL`,
		avatarID,
	)
	if err != nil {
		return fmt.Errorf("soft delete avatar: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("soft delete avatar: %w", model.ErrAvatarNotFound)
	}

	return nil
}

// ClaimForProcessing атомарно переводит готовую к обработке аватарку из pending в processing.
func (r *AvatarRepository) ClaimForProcessing(ctx context.Context, avatarID string) (bool, error) {
	commandTag, err := r.pool.Exec(
		ctx,
		`UPDATE avatars
		SET processing_status = 'processing', updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
			AND deleted_at IS NULL
			AND upload_status = 'completed'
			AND processing_status = 'pending'`,
		avatarID,
	)
	if err != nil {
		return false, fmt.Errorf("claim avatar for processing: %w", err)
	}

	return commandTag.RowsAffected() == 1, nil
}

func (r *AvatarRepository) updateStatus(
	ctx context.Context,
	query string,
	avatarID string,
	status string,
	operation string,
) error {
	commandTag, err := r.pool.Exec(ctx, query, avatarID, status)
	if err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("%s: %w", operation, model.ErrAvatarNotFound)
	}

	return nil
}

func scanAvatar(row pgx.Row) (model.Avatar, error) {
	var avatar model.Avatar
	var thumbnailS3Keys []byte
	var uploadStatus string
	var processingStatus string

	if err := row.Scan(
		&avatar.ID,
		&avatar.UserID,

		&avatar.FileName,
		&avatar.MIMEType,
		&avatar.SizeBytes,
		&avatar.Width,
		&avatar.Height,

		&avatar.S3Key,
		&thumbnailS3Keys,

		&uploadStatus,
		&processingStatus,

		&avatar.CreatedAt,
		&avatar.UpdatedAt,
		&avatar.DeletedAt,
	); err != nil {
		return model.Avatar{}, err
	}

	keys, err := unmarshalThumbnailS3Keys(thumbnailS3Keys)
	if err != nil {
		return model.Avatar{}, err
	}

	avatar.ThumbnailS3Keys = keys
	avatar.UploadStatus = model.UploadStatus(uploadStatus)
	avatar.ProcessingStatus = model.ProcessingStatus(processingStatus)

	return avatar, nil
}

func marshalThumbnailS3Keys(keys map[model.ThumbnailSize]string) ([]byte, error) {
	if keys == nil {
		return nil, nil
	}

	encoded, err := json.Marshal(keys)
	if err != nil {
		return nil, fmt.Errorf("marshal thumbnail S3 keys: %w", err)
	}

	return encoded, nil
}

func unmarshalThumbnailS3Keys(data []byte) (map[model.ThumbnailSize]string, error) {
	if len(data) == 0 || string(data) == "null" {
		return nil, nil
	}

	var keys map[model.ThumbnailSize]string
	if err := json.Unmarshal(data, &keys); err != nil {
		return nil, fmt.Errorf("unmarshal thumbnail S3 keys: %w", err)
	}

	return keys, nil
}
