//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/xhrobj/gophprofile/internal/model"
	"github.com/xhrobj/gophprofile/internal/postgres"
)

const (
	avatarID42  = "c0decafe-babe-4bed-b042-feeddeadbeef"
	avatarID69  = "c0decafe-babe-4bed-b069-feeddeadbeef"
	avatarID99  = "c0decafe-babe-4bed-b099-feeddeadbeef"
	messageID47 = "deadbeef-f00d-4dad-b047-c0decafe0bad"
	messageID77 = "deadbeef-f00d-4dad-b077-c0decafe0bad"
)

func TestIntegration_PostgreSQLAvatarRepository_CreateReadAndList(t *testing.T) {
	ctx, pool := openMigratedTestDatabase(t)
	repository := postgres.NewAvatarRepository(pool)

	aliceFirst, err := repository.Create(ctx, newAvatar(avatarID42, "alice", "alice-first.jpg"))
	if err != nil {
		t.Fatalf("Create() first avatar error = %v", err)
	}
	assertCreatedAvatar(t, aliceFirst, avatarID42, "alice", "alice-first.jpg")

	if err := repository.UpdateUploadStatus(ctx, aliceFirst.ID, model.UploadStatusCompleted); err != nil {
		t.Fatalf("UpdateUploadStatus() first avatar error = %v", err)
	}
	setCreatedAt(t, ctx, pool, aliceFirst.ID, time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC))

	aliceSecond, err := repository.Create(ctx, newAvatar(avatarID69, "alice", "alice-second.png"))
	if err != nil {
		t.Fatalf("Create() second avatar error = %v", err)
	}
	setCreatedAt(t, ctx, pool, aliceSecond.ID, time.Date(2026, 7, 30, 11, 0, 0, 0, time.UTC))

	if _, err := repository.Create(ctx, newAvatar(avatarID99, "bob", "bob.webp")); err != nil {
		t.Fatalf("Create() Bob avatar error = %v", err)
	}

	found, err := repository.GetByID(ctx, aliceFirst.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if found.ID != aliceFirst.ID || found.UserID != "alice" {
		t.Errorf("GetByID() avatar = %+v, want Alice avatar %s", found, aliceFirst.ID)
	}

	current, err := repository.GetCurrentByUserID(ctx, "alice")
	if err != nil {
		t.Fatalf("GetCurrentByUserID() before second completion error = %v", err)
	}
	if current.ID != aliceFirst.ID {
		t.Errorf("GetCurrentByUserID() ID = %q, want %q", current.ID, aliceFirst.ID)
	}

	if err := repository.UpdateUploadStatus(ctx, aliceSecond.ID, model.UploadStatusCompleted); err != nil {
		t.Fatalf("UpdateUploadStatus() second avatar error = %v", err)
	}
	current, err = repository.GetCurrentByUserID(ctx, "alice")
	if err != nil {
		t.Fatalf("GetCurrentByUserID() after second completion error = %v", err)
	}
	if current.ID != aliceSecond.ID {
		t.Errorf("GetCurrentByUserID() ID = %q, want %q", current.ID, aliceSecond.ID)
	}

	avatars, err := repository.ListByUserID(ctx, "alice")
	if err != nil {
		t.Fatalf("ListByUserID() error = %v", err)
	}
	if len(avatars) != 2 {
		t.Fatalf("ListByUserID() len = %d, want 2", len(avatars))
	}
	if avatars[0].ID != aliceSecond.ID || avatars[1].ID != aliceFirst.ID {
		t.Errorf(
			"ListByUserID() order = [%s, %s], want [%s, %s]",
			avatars[0].ID,
			avatars[1].ID,
			aliceSecond.ID,
			aliceFirst.ID,
		)
	}

	_, err = repository.GetByID(ctx, "c0decafe-babe-4bed-b077-feeddeadbeef")
	if !errors.Is(err, model.ErrAvatarNotFound) {
		t.Errorf("GetByID() missing avatar error = %v, want ErrAvatarNotFound", err)
	}
	_, err = repository.GetCurrentByUserID(ctx, "eve")
	if !errors.Is(err, model.ErrAvatarNotFound) {
		t.Errorf("GetCurrentByUserID() missing user error = %v, want ErrAvatarNotFound", err)
	}
}

func TestIntegration_PostgreSQLAvatarRepository_CreateAcceptsLongS3Key(t *testing.T) {
	ctx, pool := openMigratedTestDatabase(t)
	repository := postgres.NewAvatarRepository(pool)

	avatar := newAvatar(
		avatarID42,
		strings.Repeat("u", 255),
		strings.Repeat("f", 255),
	)
	if len(avatar.S3Key) <= 500 {
		t.Fatalf("test S3 key length = %d, want > 500", len(avatar.S3Key))
	}

	created, err := repository.Create(ctx, avatar)
	if err != nil {
		t.Fatalf("Create() long S3 key error = %v", err)
	}
	if created.S3Key != avatar.S3Key {
		t.Errorf("Create() S3Key length = %d, want %d", len(created.S3Key), len(avatar.S3Key))
	}
}

func TestIntegration_PostgreSQLAvatarRepository_Processing(t *testing.T) {
	ctx, pool := openMigratedTestDatabase(t)
	repository := postgres.NewAvatarRepository(pool)

	avatar, err := repository.Create(ctx, newAvatar(avatarID42, "alice", "avatar.jpg"))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	claimed, err := repository.ClaimForProcessing(ctx, avatar.ID, messageID47, false)
	if err != nil {
		t.Fatalf("ClaimForProcessing() uploading avatar error = %v", err)
	}
	if claimed {
		t.Error("ClaimForProcessing() uploading avatar = true, want false")
	}

	if err := repository.UpdateUploadStatus(ctx, avatar.ID, model.UploadStatusCompleted); err != nil {
		t.Fatalf("UpdateUploadStatus() error = %v", err)
	}

	claimed, err = repository.ClaimForProcessing(ctx, avatar.ID, messageID47, false)
	if err != nil {
		t.Fatalf("first ClaimForProcessing() error = %v", err)
	}
	if !claimed {
		t.Error("first ClaimForProcessing() = false, want true")
	}
	claimed, err = repository.ClaimForProcessing(ctx, avatar.ID, messageID47, false)
	if err != nil {
		t.Fatalf("ClaimForProcessing() duplicate error = %v", err)
	}
	if claimed {
		t.Error("ClaimForProcessing() duplicate = true, want false")
	}

	claimed, err = repository.ClaimForProcessing(ctx, avatar.ID, messageID77, true)
	if err != nil {
		t.Fatalf("ClaimForProcessing() different message error = %v", err)
	}
	if claimed {
		t.Error("ClaimForProcessing() different message = true, want false")
	}

	claimed, err = repository.ClaimForProcessing(ctx, avatar.ID, messageID47, true)
	if err != nil {
		t.Fatalf("ClaimForProcessing() redelivery error = %v", err)
	}
	if !claimed {
		t.Error("ClaimForProcessing() redelivery = false, want true")
	}

	thumbnailKeys := map[model.ThumbnailSize]string{
		model.ThumbnailSize100x100: "thumbnails/alice/42/100x100.jpg",
		model.ThumbnailSize300x300: "thumbnails/alice/42/300x300.jpg",
	}
	if err := repository.CompleteProcessing(ctx, avatar.ID, thumbnailKeys); err != nil {
		t.Fatalf("CompleteProcessing() error = %v", err)
	}

	stored, err := repository.GetByID(ctx, avatar.ID)
	if err != nil {
		t.Fatalf("GetByID() after processing error = %v", err)
	}
	if stored.UploadStatus != model.UploadStatusCompleted {
		t.Errorf("UploadStatus = %q, want %q", stored.UploadStatus, model.UploadStatusCompleted)
	}
	if stored.ProcessingStatus != model.ProcessingStatusCompleted {
		t.Errorf("ProcessingStatus = %q, want %q", stored.ProcessingStatus, model.ProcessingStatusCompleted)
	}
	if !reflect.DeepEqual(stored.ThumbnailS3Keys, thumbnailKeys) {
		t.Errorf("ThumbnailS3Keys = %#v, want %#v", stored.ThumbnailS3Keys, thumbnailKeys)
	}

	claimed, err = repository.ClaimForProcessing(ctx, avatar.ID, messageID47, false)
	if err != nil {
		t.Fatalf("ClaimForProcessing() completed avatar error = %v", err)
	}
	if claimed {
		t.Error("ClaimForProcessing() completed avatar = true, want false")
	}

	failedAvatar, err := repository.Create(ctx, newAvatar(avatarID69, "alice", "failed.jpg"))
	if err != nil {
		t.Fatalf("Create() failed avatar error = %v", err)
	}
	if err := repository.UpdateUploadStatus(ctx, failedAvatar.ID, model.UploadStatusCompleted); err != nil {
		t.Fatalf("UpdateUploadStatus() failed avatar error = %v", err)
	}
	claimed, err = repository.ClaimForProcessing(ctx, failedAvatar.ID, messageID47, false)
	if err != nil {
		t.Fatalf("ClaimForProcessing() failed avatar error = %v", err)
	}
	if !claimed {
		t.Fatal("ClaimForProcessing() failed avatar = false, want true")
	}
	if err := repository.UpdateProcessingStatus(ctx, failedAvatar.ID, model.ProcessingStatusFailed); err != nil {
		t.Fatalf("UpdateProcessingStatus() error = %v", err)
	}

	failedStored, err := repository.GetByID(ctx, failedAvatar.ID)
	if err != nil {
		t.Fatalf("GetByID() failed avatar error = %v", err)
	}
	if failedStored.ProcessingStatus != model.ProcessingStatusFailed {
		t.Errorf("failed avatar ProcessingStatus = %q, want %q", failedStored.ProcessingStatus, model.ProcessingStatusFailed)
	}
}

func TestIntegration_PostgreSQLAvatarRepository_StorageUsageBytes(t *testing.T) {
	ctx, pool := openMigratedTestDatabase(t)
	repository := postgres.NewAvatarRepository(pool)

	completed := newAvatar(avatarID42, "alice", "completed.jpg")
	completed.SizeBytes = 1024
	completed, err := repository.Create(ctx, completed)
	if err != nil {
		t.Fatalf("Create() completed avatar error = %v", err)
	}
	if err := repository.UpdateUploadStatus(ctx, completed.ID, model.UploadStatusCompleted); err != nil {
		t.Fatalf("UpdateUploadStatus() completed avatar error = %v", err)
	}

	uploading := newAvatar(avatarID69, "alice", "uploading.jpg")
	uploading.SizeBytes = 2048
	if _, err := repository.Create(ctx, uploading); err != nil {
		t.Fatalf("Create() uploading avatar error = %v", err)
	}

	deleted := newAvatar(avatarID99, "bob", "deleted.jpg")
	deleted.SizeBytes = 4096
	deleted, err = repository.Create(ctx, deleted)
	if err != nil {
		t.Fatalf("Create() deleted avatar error = %v", err)
	}
	if err := repository.UpdateUploadStatus(ctx, deleted.ID, model.UploadStatusCompleted); err != nil {
		t.Fatalf("UpdateUploadStatus() deleted avatar error = %v", err)
	}
	if err := repository.SoftDelete(ctx, deleted.ID); err != nil {
		t.Fatalf("SoftDelete() error = %v", err)
	}

	usage, err := repository.StorageUsageBytes(ctx)
	if err != nil {
		t.Fatalf("StorageUsageBytes() error = %v", err)
	}
	if usage != completed.SizeBytes {
		t.Errorf("StorageUsageBytes() = %d, want %d", usage, completed.SizeBytes)
	}
}

func TestIntegration_PostgreSQLAvatarRepository_DeletePermanent(t *testing.T) {
	ctx, pool := openMigratedTestDatabase(t)
	repository := postgres.NewAvatarRepository(pool)

	avatar, err := repository.Create(ctx, newAvatar(avatarID42, "alice", "avatar.jpg"))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if err := repository.DeletePermanent(ctx, avatar.ID); err != nil {
		t.Fatalf("DeletePermanent() error = %v", err)
	}

	var count int
	if err := pool.QueryRow(
		ctx,
		"SELECT COUNT(*) FROM avatars WHERE id = $1",
		avatar.ID,
	).Scan(&count); err != nil {
		t.Fatalf("count deleted avatar rows: %v", err)
	}
	if count != 0 {
		t.Errorf("deleted avatar row count = %d, want 0", count)
	}

	if err := repository.DeletePermanent(ctx, avatar.ID); !errors.Is(err, model.ErrAvatarNotFound) {
		t.Errorf("second DeletePermanent() error = %v, want ErrAvatarNotFound", err)
	}
}

func TestIntegration_PostgreSQLAvatarRepository_SoftDelete(t *testing.T) {
	ctx, pool := openMigratedTestDatabase(t)
	repository := postgres.NewAvatarRepository(pool)

	avatar, err := repository.Create(ctx, newAvatar(avatarID42, "alice", "avatar.jpg"))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := repository.SoftDelete(ctx, avatar.ID); err != nil {
		t.Fatalf("SoftDelete() error = %v", err)
	}

	_, err = repository.GetByID(ctx, avatar.ID)
	if !errors.Is(err, model.ErrAvatarNotFound) {
		t.Errorf("GetByID() deleted avatar error = %v, want ErrAvatarNotFound", err)
	}

	avatars, err := repository.ListByUserID(ctx, "alice")
	if err != nil {
		t.Fatalf("ListByUserID() error = %v", err)
	}
	if len(avatars) != 0 {
		t.Errorf("ListByUserID() len = %d, want 0", len(avatars))
	}

	if err := repository.SoftDelete(ctx, avatar.ID); !errors.Is(err, model.ErrAvatarNotFound) {
		t.Errorf("second SoftDelete() error = %v, want ErrAvatarNotFound", err)
	}
	if err := repository.UpdateUploadStatus(ctx, avatar.ID, model.UploadStatusFailed); !errors.Is(err, model.ErrAvatarNotFound) {
		t.Errorf("UpdateUploadStatus() deleted avatar error = %v, want ErrAvatarNotFound", err)
	}
	if err := repository.UpdateProcessingStatus(ctx, avatar.ID, model.ProcessingStatusFailed); !errors.Is(err, model.ErrAvatarNotFound) {
		t.Errorf("UpdateProcessingStatus() deleted avatar error = %v, want ErrAvatarNotFound", err)
	}
	if err := repository.CompleteProcessing(ctx, avatar.ID, nil); !errors.Is(err, model.ErrAvatarNotFound) {
		t.Errorf("CompleteProcessing() deleted avatar error = %v, want ErrAvatarNotFound", err)
	}

	claimed, err := repository.ClaimForProcessing(ctx, avatar.ID, messageID47, false)
	if err != nil {
		t.Fatalf("ClaimForProcessing() deleted avatar error = %v", err)
	}
	if claimed {
		t.Error("ClaimForProcessing() deleted avatar = true, want false")
	}

	var deletedAt *time.Time
	if err := pool.QueryRow(
		ctx,
		"SELECT deleted_at FROM avatars WHERE id = $1",
		avatar.ID,
	).Scan(&deletedAt); err != nil {
		t.Fatalf("read deleted avatar row: %v", err)
	}
	if deletedAt == nil {
		t.Error("deleted_at is nil, want soft-deleted row")
	}

	if err := repository.RestoreDeleted(ctx, avatar.ID); err != nil {
		t.Fatalf("RestoreDeleted() error = %v", err)
	}
	restored, err := repository.GetByID(ctx, avatar.ID)
	if err != nil {
		t.Fatalf("GetByID() restored avatar error = %v", err)
	}
	if restored.ID != avatar.ID {
		t.Errorf("restored avatar ID = %q, want %q", restored.ID, avatar.ID)
	}
	if err := repository.RestoreDeleted(ctx, avatar.ID); !errors.Is(err, model.ErrAvatarNotFound) {
		t.Errorf("second RestoreDeleted() error = %v, want ErrAvatarNotFound", err)
	}
}

func newAvatar(id, userID, fileName string) model.Avatar {
	return model.Avatar{
		ID:        id,
		UserID:    userID,
		FileName:  fileName,
		MIMEType:  "image/jpeg",
		SizeBytes: 1024,
		Width:     512,
		Height:    512,
		S3Key:     "originals/" + userID + "/" + id + "/" + fileName,
	}
}

func assertCreatedAvatar(t *testing.T, avatar model.Avatar, id, userID, fileName string) {
	t.Helper()

	if avatar.ID != id {
		t.Errorf("Create() ID = %q, want %q", avatar.ID, id)
	}
	if avatar.UserID != userID {
		t.Errorf("Create() UserID = %q, want %q", avatar.UserID, userID)
	}
	if avatar.FileName != fileName {
		t.Errorf("Create() FileName = %q, want %q", avatar.FileName, fileName)
	}
	if avatar.UploadStatus != model.UploadStatusUploading {
		t.Errorf("Create() UploadStatus = %q, want %q", avatar.UploadStatus, model.UploadStatusUploading)
	}
	if avatar.ProcessingStatus != model.ProcessingStatusPending {
		t.Errorf("Create() ProcessingStatus = %q, want %q", avatar.ProcessingStatus, model.ProcessingStatusPending)
	}
	if avatar.CreatedAt.IsZero() {
		t.Error("Create() CreatedAt is zero")
	}
	if avatar.UpdatedAt.IsZero() {
		t.Error("Create() UpdatedAt is zero")
	}
	if avatar.DeletedAt != nil {
		t.Errorf("Create() DeletedAt = %v, want nil", avatar.DeletedAt)
	}
}

func setCreatedAt(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	avatarID string,
	createdAt time.Time,
) {
	t.Helper()

	if _, err := pool.Exec(
		ctx,
		"UPDATE avatars SET created_at = $2 WHERE id = $1",
		avatarID,
		createdAt,
	); err != nil {
		t.Fatalf("set created_at: %v", err)
	}
}
