package service

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/xhrobj/gophprofile/internal/model"
)

type deleteRepository struct {
	avatar         model.Avatar
	getByIDErr     error
	getCurrentErr  error
	softDeleteErr  error
	restoreErr     error
	getByIDCalls   []string
	getCurrentIDs  []string
	softDeleteIDs  []string
	restoreIDs     []string
	restoreCtxErrs []error
}

type deletePublisher struct {
	err       error
	calls     []model.Avatar
	beforeErr func()
}

var (
	errGetAvatar      = errors.New("get avatar")
	errSoftDelete     = errors.New("soft delete")
	errPublishDeleted = errors.New("publish deleted")
	errRestoreDeleted = errors.New("restore deleted")
)

func TestAvatarService_DeleteByID(t *testing.T) {
	avatar := model.Avatar{ID: avatarID42, UserID: "Alice", S3Key: "originals/Alice/avatar.png"}

	tests := []struct {
		name            string
		requesterUserID string
		getErr          error
		softDeleteErr   error
		publishErr      error
		restoreErr      error
		wantErrors      []error
		wantSoftDelete  bool
		wantPublish     bool
		wantRestore     bool
	}{
		{
			name:            "deletes owned avatar",
			requesterUserID: "Alice",
			wantSoftDelete:  true,
			wantPublish:     true,
		},
		{
			name:            "returns not found",
			requesterUserID: "Alice",
			getErr:          model.ErrAvatarNotFound,
			wantErrors:      []error{model.ErrAvatarNotFound},
		},
		{
			name:            "rejects another owner",
			requesterUserID: "Eve",
			wantErrors:      []error{ErrForbidden},
		},
		{
			name:            "returns soft delete error",
			requesterUserID: "Alice",
			softDeleteErr:   errSoftDelete,
			wantErrors:      []error{errSoftDelete},
			wantSoftDelete:  true,
		},
		{
			name:            "restores avatar after publish error",
			requesterUserID: "Alice",
			publishErr:      errPublishDeleted,
			wantErrors:      []error{ErrServiceUnavailable, errPublishDeleted},
			wantSoftDelete:  true,
			wantPublish:     true,
			wantRestore:     true,
		},
		{
			name:            "joins restore error after publish error",
			requesterUserID: "Alice",
			publishErr:      errPublishDeleted,
			restoreErr:      errRestoreDeleted,
			wantErrors:      []error{ErrServiceUnavailable, errPublishDeleted, errRestoreDeleted},
			wantSoftDelete:  true,
			wantPublish:     true,
			wantRestore:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository := &deleteRepository{
				avatar:        avatar,
				getByIDErr:    tt.getErr,
				softDeleteErr: tt.softDeleteErr,
				restoreErr:    tt.restoreErr,
			}
			publisher := &deletePublisher{err: tt.publishErr}
			avatarService := newDeleteTestAvatarService(repository, publisher)

			err := avatarService.DeleteByID(context.Background(), avatarID42, tt.requesterUserID)
			assertDeleteErrors(t, err, tt.wantErrors)

			if !reflect.DeepEqual(repository.getByIDCalls, []string{avatarID42}) {
				t.Errorf("GetByID() calls = %#v, want [%q]", repository.getByIDCalls, avatarID42)
			}
			if got := len(repository.softDeleteIDs) == 1; got != tt.wantSoftDelete {
				t.Errorf("SoftDelete() called = %t, want %t", got, tt.wantSoftDelete)
			}
			if got := len(publisher.calls) == 1; got != tt.wantPublish {
				t.Errorf("PublishAvatarDeleted() called = %t, want %t", got, tt.wantPublish)
			}
			if got := len(repository.restoreIDs) == 1; got != tt.wantRestore {
				t.Errorf("RestoreDeleted() called = %t, want %t", got, tt.wantRestore)
			}
			if tt.wantPublish && !reflect.DeepEqual(publisher.calls[0], avatar) {
				t.Errorf("PublishAvatarDeleted() avatar = %+v, want %+v", publisher.calls[0], avatar)
			}
		})
	}
}

func TestAvatarService_DeleteCurrentByUserID(t *testing.T) {
	avatar := model.Avatar{ID: avatarID42, UserID: "Alice"}

	tests := []struct {
		name            string
		userID          string
		requesterUserID string
		getCurrentErr   error
		wantErrors      []error
		wantGetCurrent  bool
		wantPublish     bool
	}{
		{
			name:            "deletes current avatar",
			userID:          "Alice",
			requesterUserID: "Alice",
			wantGetCurrent:  true,
			wantPublish:     true,
		},
		{
			name:            "rejects another user before lookup",
			userID:          "Alice",
			requesterUserID: "Eve",
			wantErrors:      []error{ErrForbidden},
		},
		{
			name:            "returns current avatar lookup error",
			userID:          "Alice",
			requesterUserID: "Alice",
			getCurrentErr:   errGetAvatar,
			wantErrors:      []error{errGetAvatar},
			wantGetCurrent:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository := &deleteRepository{avatar: avatar, getCurrentErr: tt.getCurrentErr}
			publisher := &deletePublisher{}
			avatarService := newDeleteTestAvatarService(repository, publisher)

			err := avatarService.DeleteCurrentByUserID(context.Background(), tt.userID, tt.requesterUserID)
			assertDeleteErrors(t, err, tt.wantErrors)

			if got := len(repository.getCurrentIDs) == 1; got != tt.wantGetCurrent {
				t.Errorf("GetCurrentByUserID() called = %t, want %t", got, tt.wantGetCurrent)
			}
			if got := len(publisher.calls) == 1; got != tt.wantPublish {
				t.Errorf("PublishAvatarDeleted() called = %t, want %t", got, tt.wantPublish)
			}
		})
	}
}

func TestAvatarService_DeleteByID_RollbackUsesIndependentContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	repository := &deleteRepository{avatar: model.Avatar{ID: avatarID42, UserID: "Alice"}}
	publisher := &deletePublisher{
		err: errPublishDeleted,
		beforeErr: func() {
			cancel()
		},
	}
	avatarService := newDeleteTestAvatarService(repository, publisher)

	err := avatarService.DeleteByID(ctx, avatarID42, "Alice")
	if !errors.Is(err, ErrServiceUnavailable) {
		t.Fatalf("DeleteByID() error = %v, want ErrServiceUnavailable", err)
	}
	if len(repository.restoreCtxErrs) != 1 {
		t.Fatalf("RestoreDeleted() context checks = %d, want 1", len(repository.restoreCtxErrs))
	}
	if repository.restoreCtxErrs[0] != nil {
		t.Errorf("RestoreDeleted() context error = %v, want nil", repository.restoreCtxErrs[0])
	}
}

func (r *deleteRepository) Create(context.Context, model.Avatar) (model.Avatar, error) {
	return model.Avatar{}, nil
}

func (r *deleteRepository) GetByID(_ context.Context, avatarID string) (model.Avatar, error) {
	r.getByIDCalls = append(r.getByIDCalls, avatarID)
	return r.avatar, r.getByIDErr
}

func (r *deleteRepository) GetCurrentByUserID(_ context.Context, userID string) (model.Avatar, error) {
	r.getCurrentIDs = append(r.getCurrentIDs, userID)
	return r.avatar, r.getCurrentErr
}

func (*deleteRepository) ListByUserID(context.Context, string) ([]model.Avatar, error) {
	return nil, nil
}

func (*deleteRepository) UpdateUploadStatus(context.Context, string, model.UploadStatus) error {
	return nil
}

func (*deleteRepository) DeletePermanent(context.Context, string) error {
	return nil
}

func (r *deleteRepository) SoftDelete(_ context.Context, avatarID string) error {
	r.softDeleteIDs = append(r.softDeleteIDs, avatarID)
	return r.softDeleteErr
}

func (r *deleteRepository) RestoreDeleted(ctx context.Context, avatarID string) error {
	r.restoreIDs = append(r.restoreIDs, avatarID)
	r.restoreCtxErrs = append(r.restoreCtxErrs, ctx.Err())
	return r.restoreErr
}

func (*deletePublisher) PublishAvatarUploaded(context.Context, model.Avatar) error {
	return nil
}

func (p *deletePublisher) PublishAvatarDeleted(_ context.Context, avatar model.Avatar) error {
	p.calls = append(p.calls, avatar)
	if p.beforeErr != nil {
		p.beforeErr()
	}

	return p.err
}

func newDeleteTestAvatarService(repository AvatarRepository, publisher AvatarEventPublisher) *AvatarService {
	return NewAvatarService(repository, &fakeAvatarStorage{}, publisher, func() string { return avatarID42 }, func(_, _, _ string) string { return "" })
}

func assertDeleteErrors(t *testing.T, err error, wantErrors []error) {
	t.Helper()

	if len(wantErrors) == 0 {
		if err != nil {
			t.Fatalf("delete error = %v, want nil", err)
		}
		return
	}
	if err == nil {
		t.Fatal("delete error = nil, want error")
	}
	for _, wantErr := range wantErrors {
		if !errors.Is(err, wantErr) {
			t.Errorf("delete error = %v, want %v", err, wantErr)
		}
	}
}
