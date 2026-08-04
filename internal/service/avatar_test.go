package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/xhrobj/gophprofile/internal/model"
)

const avatarID42 = "c0decafe-babe-4bed-b042-feeddeadbeef"

type repositoryCall struct {
	avatarID string
	status   model.UploadStatus
}

type fakeAvatarRepository struct {
	createErr          error
	updateErrors       []error
	deletePermanentErr error

	createCalls        []model.Avatar
	updateCalls        []repositoryCall
	deletePermanentIDs []string
}

type fakeAvatarStorage struct {
	putErr    error
	deleteErr error

	putCalls       int
	putKey         string
	putContent     []byte
	putSize        int64
	putContentType string
	deleteKeys     []string
}

type fakeAvatarEventPublisher struct {
	err   error
	calls []model.Avatar
}

var (
	errCreateMetadata = errors.New("create metadata")
	errPutOriginal    = errors.New("put original")
	errUpdateStatus   = errors.New("update status")
	errDeleteOriginal = errors.New("delete original")
	errPublishEvent   = errors.New("publish event")
	errDeleteMetadata = errors.New("delete metadata")
)

func TestAvatarService_Upload(t *testing.T) {
	content := encodePNG(t)
	input := UploadInput{
		UserID:   "Alice",
		FileName: "avatar.png",
		Content:  content,
	}

	tests := []struct {
		name                  string
		createErr             error
		putErr                error
		updateErrors          []error
		deleteErr             error
		publishErr            error
		deletePermanentErr    error
		wantErrors            []error
		wantPutCalls          int
		wantPublishCalls      int
		wantUpdateCalls       []repositoryCall
		wantDeleteKeys        []string
		wantDeleteMetadataIDs []string
	}{
		{
			name:             "uploads original and publishes event",
			wantPutCalls:     1,
			wantPublishCalls: 1,
			wantUpdateCalls: []repositoryCall{
				{avatarID: avatarID42, status: model.UploadStatusCompleted},
			},
		},
		{
			name:       "returns repository create error",
			createErr:  errCreateMetadata,
			wantErrors: []error{errCreateMetadata},
		},
		{
			name:         "marks failed after storage error",
			putErr:       errPutOriginal,
			wantErrors:   []error{errPutOriginal},
			wantPutCalls: 1,
			wantUpdateCalls: []repositoryCall{
				{avatarID: avatarID42, status: model.UploadStatusFailed},
			},
		},
		{
			name:         "joins storage and status errors",
			putErr:       errPutOriginal,
			updateErrors: []error{errUpdateStatus},
			wantErrors:   []error{errPutOriginal, errUpdateStatus},
			wantPutCalls: 1,
			wantUpdateCalls: []repositoryCall{
				{avatarID: avatarID42, status: model.UploadStatusFailed},
			},
		},
		{
			name:         "compensates final status error",
			updateErrors: []error{errUpdateStatus, nil},
			wantErrors:   []error{errUpdateStatus},
			wantPutCalls: 1,
			wantUpdateCalls: []repositoryCall{
				{avatarID: avatarID42, status: model.UploadStatusCompleted},
				{avatarID: avatarID42, status: model.UploadStatusFailed},
			},
			wantDeleteKeys: []string{"originals/Alice/" + avatarID42 + "/avatar.png"},
		},

		{
			name:             "compensates publish error",
			publishErr:       errPublishEvent,
			wantErrors:       []error{ErrServiceUnavailable, errPublishEvent},
			wantPutCalls:     1,
			wantPublishCalls: 1,
			wantUpdateCalls: []repositoryCall{
				{avatarID: avatarID42, status: model.UploadStatusCompleted},
			},
			wantDeleteKeys:        []string{"originals/Alice/" + avatarID42 + "/avatar.png"},
			wantDeleteMetadataIDs: []string{avatarID42},
		},
		{
			name:               "joins publish compensation errors",
			deleteErr:          errDeleteOriginal,
			publishErr:         errPublishEvent,
			deletePermanentErr: errDeleteMetadata,
			wantErrors:         []error{ErrServiceUnavailable, errPublishEvent, errDeleteOriginal, errDeleteMetadata},
			wantPutCalls:       1,
			wantPublishCalls:   1,
			wantUpdateCalls: []repositoryCall{
				{avatarID: avatarID42, status: model.UploadStatusCompleted},
			},
			wantDeleteKeys:        []string{"originals/Alice/" + avatarID42 + "/avatar.png"},
			wantDeleteMetadataIDs: []string{avatarID42},
		},
		{
			name:         "joins compensation errors",
			updateErrors: []error{errUpdateStatus, errUpdateStatus},
			deleteErr:    errDeleteOriginal,
			wantErrors:   []error{errUpdateStatus, errDeleteOriginal},
			wantPutCalls: 1,
			wantUpdateCalls: []repositoryCall{
				{avatarID: avatarID42, status: model.UploadStatusCompleted},
				{avatarID: avatarID42, status: model.UploadStatusFailed},
			},
			wantDeleteKeys: []string{"originals/Alice/" + avatarID42 + "/avatar.png"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository := &fakeAvatarRepository{
				createErr:          tt.createErr,
				updateErrors:       append([]error(nil), tt.updateErrors...),
				deletePermanentErr: tt.deletePermanentErr,
			}
			storage := &fakeAvatarStorage{
				putErr:    tt.putErr,
				deleteErr: tt.deleteErr,
			}
			publisher := &fakeAvatarEventPublisher{err: tt.publishErr}
			service := newTestAvatarService(repository, storage, publisher)

			avatar, err := service.Upload(context.Background(), input)
			assertErrors(t, err, tt.wantErrors)

			if len(repository.createCalls) != 1 {
				t.Fatalf("Create() calls = %d, want 1", len(repository.createCalls))
			}
			if storage.putCalls != tt.wantPutCalls {
				t.Errorf("Put() calls = %d, want %d", storage.putCalls, tt.wantPutCalls)
			}
			if !reflect.DeepEqual(repository.updateCalls, tt.wantUpdateCalls) {
				t.Errorf("UpdateUploadStatus() calls = %#v, want %#v", repository.updateCalls, tt.wantUpdateCalls)
			}
			if !reflect.DeepEqual(storage.deleteKeys, tt.wantDeleteKeys) {
				t.Errorf("Delete() keys = %#v, want %#v", storage.deleteKeys, tt.wantDeleteKeys)
			}
			if len(publisher.calls) != tt.wantPublishCalls {
				t.Errorf("PublishAvatarUploaded() calls = %d, want %d", len(publisher.calls), tt.wantPublishCalls)
			}
			if !reflect.DeepEqual(repository.deletePermanentIDs, tt.wantDeleteMetadataIDs) {
				t.Errorf("DeletePermanent() IDs = %#v, want %#v", repository.deletePermanentIDs, tt.wantDeleteMetadataIDs)
			}
			if len(publisher.calls) == 1 {
				published := publisher.calls[0]
				if published.ID != avatarID42 || published.UserID != "Alice" || published.S3Key != "originals/Alice/"+avatarID42+"/avatar.png" {
					t.Errorf("PublishAvatarUploaded() avatar = %+v, want uploaded Alice avatar", published)
				}
			}

			created := repository.createCalls[0]
			assertCreatedAvatarInput(t, created, input)

			if len(tt.wantErrors) != 0 {
				return
			}

			if avatar.ID != avatarID42 {
				t.Errorf("Upload() ID = %q, want %q", avatar.ID, avatarID42)
			}
			if avatar.UploadStatus != model.UploadStatusCompleted {
				t.Errorf("Upload() UploadStatus = %q, want %q", avatar.UploadStatus, model.UploadStatusCompleted)
			}
			if storage.putKey != created.S3Key {
				t.Errorf("Put() key = %q, want %q", storage.putKey, created.S3Key)
			}
			if !bytes.Equal(storage.putContent, content) {
				t.Errorf("Put() content = %q, want %q", storage.putContent, content)
			}
			if storage.putSize != int64(len(content)) {
				t.Errorf("Put() size = %d, want %d", storage.putSize, len(content))
			}
			if storage.putContentType != mimeTypePNG {
				t.Errorf("Put() content type = %q, want %q", storage.putContentType, mimeTypePNG)
			}
		})
	}
}

func TestAvatarService_Upload_InvalidImageFormat(t *testing.T) {
	repository := &fakeAvatarRepository{}
	storage := &fakeAvatarStorage{}
	service := newTestAvatarService(repository, storage, &fakeAvatarEventPublisher{})

	_, err := service.Upload(context.Background(), UploadInput{
		UserID:   "Alice",
		FileName: "avatar.jpg",
		Content:  []byte("not an image"),
	})
	if !errors.Is(err, ErrInvalidImageFormat) {
		t.Fatalf("Upload() error = %v, want %v", err, ErrInvalidImageFormat)
	}
	if len(repository.createCalls) != 0 {
		t.Errorf("Create() calls = %d, want 0", len(repository.createCalls))
	}
	if storage.putCalls != 0 {
		t.Errorf("Put() calls = %d, want 0", storage.putCalls)
	}
}

func newTestAvatarService(
	repository AvatarRepository,
	storage AvatarStorage,
	publisher AvatarEventPublisher,
) *AvatarService {
	return NewAvatarService(
		repository,
		storage,
		publisher,
		func() string { return avatarID42 },
		func(userID, avatarID, fileName string) string {
			return "originals/" + userID + "/" + avatarID + "/" + fileName
		},
	)
}

func assertErrors(t *testing.T, err error, wantErrors []error) {
	t.Helper()

	if len(wantErrors) == 0 {
		if err != nil {
			t.Fatalf("Upload() error = %v", err)
		}
		return
	}

	if err == nil {
		t.Fatal("Upload() error = nil, want error")
	}
	for _, wantErr := range wantErrors {
		if !errors.Is(err, wantErr) {
			t.Errorf("Upload() error = %v, want %v", err, wantErr)
		}
	}
}

func assertCreatedAvatarInput(t *testing.T, created model.Avatar, input UploadInput) {
	t.Helper()

	if created.ID != avatarID42 {
		t.Errorf("Create() ID = %q, want %q", created.ID, avatarID42)
	}
	if created.UserID != input.UserID || created.FileName != input.FileName {
		t.Errorf("Create() avatar = %+v, want user %q and file %q", created, input.UserID, input.FileName)
	}
	if created.MIMEType != mimeTypePNG {
		t.Errorf("Create() MIMEType = %q, want %q", created.MIMEType, mimeTypePNG)
	}
	if created.SizeBytes != int64(len(input.Content)) || created.Width != testImageWidth || created.Height != testImageHeight {
		t.Errorf(
			"Create() image metadata = size %d, %dx%d, want size %d, %dx%d",
			created.SizeBytes,
			created.Width,
			created.Height,
			len(input.Content),
			testImageWidth,
			testImageHeight,
		)
	}
	wantKey := "originals/Alice/" + avatarID42 + "/avatar.png"
	if created.S3Key != wantKey {
		t.Errorf("Create() S3Key = %q, want %q", created.S3Key, wantKey)
	}
}

func (r *fakeAvatarRepository) Create(_ context.Context, avatar model.Avatar) (model.Avatar, error) {
	r.createCalls = append(r.createCalls, avatar)
	if r.createErr != nil {
		return model.Avatar{}, r.createErr
	}

	avatar.UploadStatus = model.UploadStatusUploading
	avatar.ProcessingStatus = model.ProcessingStatusPending
	avatar.CreatedAt = time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)

	return avatar, nil
}

func (r *fakeAvatarRepository) UpdateUploadStatus(
	_ context.Context,
	avatarID string,
	status model.UploadStatus,
) error {
	r.updateCalls = append(r.updateCalls, repositoryCall{avatarID: avatarID, status: status})
	if len(r.updateErrors) == 0 {
		return nil
	}

	err := r.updateErrors[0]
	r.updateErrors = r.updateErrors[1:]

	return err
}

func (r *fakeAvatarRepository) DeletePermanent(_ context.Context, avatarID string) error {
	r.deletePermanentIDs = append(r.deletePermanentIDs, avatarID)

	return r.deletePermanentErr
}

func (p *fakeAvatarEventPublisher) PublishAvatarUploaded(_ context.Context, avatar model.Avatar) error {
	p.calls = append(p.calls, avatar)

	return p.err
}

func (s *fakeAvatarStorage) Put(
	_ context.Context,
	key string,
	reader io.Reader,
	size int64,
	contentType string,
) error {
	s.putCalls++
	s.putKey = key
	s.putSize = size
	s.putContentType = contentType

	content, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	s.putContent = content

	return s.putErr
}

func (s *fakeAvatarStorage) Delete(_ context.Context, keys ...string) error {
	s.deleteKeys = append(s.deleteKeys, keys...)

	return s.deleteErr
}

func (f *fakeAvatarRepository) GetByID(context.Context, string) (model.Avatar, error) {
	return model.Avatar{}, nil
}

func (f *fakeAvatarRepository) GetCurrentByUserID(context.Context, string) (model.Avatar, error) {
	return model.Avatar{}, nil
}

func (f *fakeAvatarRepository) ListByUserID(context.Context, string) ([]model.Avatar, error) {
	return nil, nil
}

func (f *fakeAvatarStorage) Get(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), nil
}
