package event

import (
	"encoding/json"
	"testing"
	"time"
)

const (
	messageID42 = "c0decafe-babe-4bed-b042-feeddeadbeef"
	avatarID69  = "c0decafe-babe-4bed-b069-feeddeadbeef"
)

func TestAvatarUploaded_JSON(t *testing.T) {
	createdAt := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	event := AvatarUploaded{
		MessageID:     messageID42,
		AvatarID:      avatarID69,
		UserID:        "Alice",
		S3Key:         "originals/Alice/" + avatarID69 + "/avatar.png",
		SchemaVersion: AvatarUploadedSchemaVersion,
		CreatedAt:     createdAt,
	}

	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	want := `{"message_id":"c0decafe-babe-4bed-b042-feeddeadbeef","avatar_id":"c0decafe-babe-4bed-b069-feeddeadbeef","user_id":"Alice","s3_key":"originals/Alice/c0decafe-babe-4bed-b069-feeddeadbeef/avatar.png","schema_version":1,"created_at":"2026-08-04T12:00:00Z"}`
	if string(encoded) != want {
		t.Errorf("json.Marshal() = %s, want %s", encoded, want)
	}
}
