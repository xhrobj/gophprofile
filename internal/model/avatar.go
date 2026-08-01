package model

import "time"

const (
	UploadStatusUploading UploadStatus = "uploading"
	UploadStatusCompleted UploadStatus = "completed"
	UploadStatusFailed    UploadStatus = "failed"

	ProcessingStatusPending    ProcessingStatus = "pending"
	ProcessingStatusProcessing ProcessingStatus = "processing"
	ProcessingStatusCompleted  ProcessingStatus = "completed"
	ProcessingStatusFailed     ProcessingStatus = "failed"

	ThumbnailSize100x100 ThumbnailSize = "100x100"
	ThumbnailSize300x300 ThumbnailSize = "300x300"
)

// UploadStatus описывает состояние загрузки оригинала аватарки в S3.
type UploadStatus string

// ProcessingStatus описывает состояние фоновой обработки аватарки.
type ProcessingStatus string

// ThumbnailSize определяет поддерживаемый размер миниатюры.
type ThumbnailSize string

// Avatar содержит метаданные оригинала аватарки и результатов её обработки.
type Avatar struct {
	ID     string
	UserID string

	FileName  string
	MIMEType  string
	SizeBytes int64
	Width     int
	Height    int

	S3Key           string
	ThumbnailS3Keys map[ThumbnailSize]string

	UploadStatus     UploadStatus
	ProcessingStatus ProcessingStatus

	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
}
