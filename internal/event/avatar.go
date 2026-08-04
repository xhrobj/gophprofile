// Package event содержит контракты асинхронных событий GophProfile.
package event

import "time"

const (
	AvatarUploadedRoutingKey    = "avatar.uploaded"
	AvatarUploadedSchemaVersion = 1

	AvatarDeletedRoutingKey    = "avatar.deleted"
	AvatarDeletedSchemaVersion = 1
)

// AvatarUploaded сообщает Worker о готовом к фоновой обработке оригинале аватарки.
type AvatarUploaded struct {
	MessageID     string    `json:"message_id"`
	AvatarID      string    `json:"avatar_id"`
	UserID        string    `json:"user_id"`
	S3Key         string    `json:"s3_key"`
	SchemaVersion int       `json:"schema_version"`
	CreatedAt     time.Time `json:"created_at"`
}

// AvatarDeleted сообщает Worker, какие S3-объекты удалённой аватарки нужно очистить.
type AvatarDeleted struct {
	MessageID     string    `json:"message_id"`
	AvatarID      string    `json:"avatar_id"`
	S3Keys        []string  `json:"s3_keys"`
	SchemaVersion int       `json:"schema_version"`
	CreatedAt     time.Time `json:"created_at"`
}
