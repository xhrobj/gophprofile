// Package event содержит контракты асинхронных событий GophProfile.
package event

import "time"

const (
	AvatarUploadedRoutingKey    = "avatar.uploaded"
	AvatarDeletedRoutingKey     = "avatar.deleted"
	AvatarUploadedSchemaVersion = 1
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
