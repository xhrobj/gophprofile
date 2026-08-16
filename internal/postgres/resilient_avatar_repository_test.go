package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/xhrobj/gophprofile/internal/model"
	"github.com/xhrobj/gophprofile/internal/resilience"
)

type recordingAvatarRepository struct {
	calls map[string]int
	err   error
}

func TestResilientAvatarRepository_DelegatesOperations(t *testing.T) {
	base := &recordingAvatarRepository{calls: make(map[string]int)}
	repository := NewResilientAvatarRepository(nil, nil)
	repository.base = base

	ctx := context.Background()
	avatar := model.Avatar{ID: "c0decafe-babe-4bed-b042-feeddeadbeef", UserID: "Alice"}
	thumbnailKeys := map[model.ThumbnailSize]string{
		model.ThumbnailSize100x100: "avatars/100.webp",
	}

	created, err := repository.Create(ctx, avatar)
	if err != nil || created.ID != avatar.ID {
		t.Fatalf("Create() = (%+v, %v), want avatar %q", created, err, avatar.ID)
	}

	got, err := repository.GetByID(ctx, avatar.ID)
	if err != nil || got.ID != avatar.ID {
		t.Fatalf("GetByID() = (%+v, %v), want avatar %q", got, err, avatar.ID)
	}

	current, err := repository.GetCurrentByUserID(ctx, avatar.UserID)
	if err != nil || current.UserID != avatar.UserID {
		t.Fatalf("GetCurrentByUserID() = (%+v, %v), want user %q", current, err, avatar.UserID)
	}

	listed, err := repository.ListByUserID(ctx, avatar.UserID)
	if err != nil || len(listed) != 1 || listed[0].UserID != avatar.UserID {
		t.Fatalf("ListByUserID() = (%+v, %v), want one avatar for %q", listed, err, avatar.UserID)
	}

	requireRepositoryNoError(t, "UpdateUploadStatus", repository.UpdateUploadStatus(
		ctx,
		avatar.ID,
		model.UploadStatusCompleted,
	))
	requireRepositoryNoError(t, "UpdateProcessingStatus", repository.UpdateProcessingStatus(
		ctx,
		avatar.ID,
		model.ProcessingStatusProcessing,
	))
	requireRepositoryNoError(t, "CompleteProcessing", repository.CompleteProcessing(ctx, avatar.ID, thumbnailKeys))
	requireRepositoryNoError(t, "DeletePermanent", repository.DeletePermanent(ctx, avatar.ID))
	requireRepositoryNoError(t, "SoftDelete", repository.SoftDelete(ctx, avatar.ID))
	requireRepositoryNoError(t, "RestoreDeleted", repository.RestoreDeleted(ctx, avatar.ID))

	usage, err := repository.StorageUsageBytes(ctx)
	if err != nil || usage != 42 {
		t.Fatalf("StorageUsageBytes() = (%d, %v), want (42, nil)", usage, err)
	}

	claimed, err := repository.ClaimForProcessing(ctx, avatar.ID, "deadbeef-f00d-4dad-b042-c0decafe0bad", true)
	if err != nil || !claimed {
		t.Fatalf("ClaimForProcessing() = (%t, %v), want (true, nil)", claimed, err)
	}

	for _, operation := range []string{
		"Create",
		"GetByID",
		"GetCurrentByUserID",
		"ListByUserID",
		"UpdateUploadStatus",
		"UpdateProcessingStatus",
		"CompleteProcessing",
		"DeletePermanent",
		"SoftDelete",
		"RestoreDeleted",
		"StorageUsageBytes",
		"ClaimForProcessing",
	} {
		if base.calls[operation] != 1 {
			t.Errorf("%s() calls = %d, want 1", operation, base.calls[operation])
		}
	}
}

func TestResilientAvatarRepository_DoesNotTripOnNotFound(t *testing.T) {
	base := &recordingAvatarRepository{
		calls: make(map[string]int),
		err:   fmt.Errorf("repository: %w", model.ErrAvatarNotFound),
	}
	repository := NewResilientAvatarRepository(nil, nil)
	repository.base = base

	for range 3 {
		_, err := repository.GetByID(context.Background(), "missing")
		if !errors.Is(err, model.ErrAvatarNotFound) {
			t.Fatalf("GetByID() error = %v, want ErrAvatarNotFound", err)
		}
		if errors.Is(err, resilience.ErrDependencyUnavailable) {
			t.Fatalf("GetByID() error = %v, business error must not be dependency unavailable", err)
		}
	}

	base.err = nil
	if _, err := repository.GetByID(context.Background(), "available"); err != nil {
		t.Fatalf("GetByID() after not-found results = %v, breaker must remain closed", err)
	}
}

func (r *recordingAvatarRepository) Create(_ context.Context, avatar model.Avatar) (model.Avatar, error) {
	r.record("Create")
	return avatar, r.err
}

func (r *recordingAvatarRepository) GetByID(_ context.Context, avatarID string) (model.Avatar, error) {
	r.record("GetByID")
	return model.Avatar{ID: avatarID}, r.err
}

func (r *recordingAvatarRepository) GetCurrentByUserID(_ context.Context, userID string) (model.Avatar, error) {
	r.record("GetCurrentByUserID")
	return model.Avatar{UserID: userID}, r.err
}

func (r *recordingAvatarRepository) ListByUserID(_ context.Context, userID string) ([]model.Avatar, error) {
	r.record("ListByUserID")
	return []model.Avatar{{UserID: userID}}, r.err
}

func (r *recordingAvatarRepository) UpdateUploadStatus(
	_ context.Context,
	_ string,
	_ model.UploadStatus,
) error {
	r.record("UpdateUploadStatus")
	return r.err
}

func (r *recordingAvatarRepository) UpdateProcessingStatus(
	_ context.Context,
	_ string,
	_ model.ProcessingStatus,
) error {
	r.record("UpdateProcessingStatus")
	return r.err
}

func (r *recordingAvatarRepository) CompleteProcessing(
	_ context.Context,
	_ string,
	_ map[model.ThumbnailSize]string,
) error {
	r.record("CompleteProcessing")
	return r.err
}

func (r *recordingAvatarRepository) DeletePermanent(context.Context, string) error {
	r.record("DeletePermanent")
	return r.err
}

func (r *recordingAvatarRepository) SoftDelete(context.Context, string) error {
	r.record("SoftDelete")
	return r.err
}

func (r *recordingAvatarRepository) RestoreDeleted(context.Context, string) error {
	r.record("RestoreDeleted")
	return r.err
}

func (r *recordingAvatarRepository) StorageUsageBytes(context.Context) (int64, error) {
	r.record("StorageUsageBytes")
	return 42, r.err
}

func (r *recordingAvatarRepository) ClaimForProcessing(context.Context, string, string, bool) (bool, error) {
	r.record("ClaimForProcessing")
	return true, r.err
}

func (r *recordingAvatarRepository) record(call string) {
	r.calls[call]++
}

func requireRepositoryNoError(t *testing.T, operation string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s() error = %v", operation, err)
	}
}
