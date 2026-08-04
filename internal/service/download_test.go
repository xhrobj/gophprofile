package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/xhrobj/gophprofile/internal/model"
)

type downloadRepository struct {
	avatar      model.Avatar
	avatars     []model.Avatar
	err         error
	listErr     error
	avatarIDs   []string
	userIDs     []string
	listUserIDs []string
}

type downloadStorage struct {
	content []byte
	err     error
	keys    []string
}

func TestAvatarService_Download(t *testing.T) {
	avatar := model.Avatar{
		ID:       avatarID42,
		UserID:   "Alice",
		MIMEType: "image/png",
		S3Key:    "original.png",
		ThumbnailS3Keys: map[model.ThumbnailSize]string{
			model.ThumbnailSize100x100: "100.jpg",
			model.ThumbnailSize300x300: "300.jpg",
		},
	}

	tests := []struct {
		name            string
		input           DownloadInput
		wantKey         string
		wantContentType string
	}{
		{name: "gets original by ID", input: DownloadInput{AvatarID: avatarID42}, wantKey: "original.png", wantContentType: "image/png"},
		{name: "gets explicit original by user", input: DownloadInput{UserID: "Alice", Size: OriginalAvatarSize}, wantKey: "original.png", wantContentType: "image/png"},
		{name: "gets small thumbnail", input: DownloadInput{AvatarID: avatarID42, Size: "100x100"}, wantKey: "100.jpg", wantContentType: "image/jpeg"},
		{name: "gets large thumbnail", input: DownloadInput{AvatarID: avatarID42, Size: "300x300"}, wantKey: "300.jpg", wantContentType: "image/jpeg"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository := &downloadRepository{avatar: avatar}
			storage := &downloadStorage{content: []byte("image")}
			avatarService := NewAvatarService(repository, storage, &fakeAvatarEventPublisher{}, func() string { return avatarID42 }, func(_, _, _ string) string { return "" })

			output, err := avatarService.Download(context.Background(), tt.input)
			if err != nil {
				t.Fatalf("Download() error = %v", err)
			}
			content, err := io.ReadAll(output.Content)
			if err != nil {
				t.Fatalf("read content: %v", err)
			}
			if err := output.Content.Close(); err != nil {
				t.Errorf("close content: %v", err)
			}
			if !bytes.Equal(content, []byte("image")) {
				t.Errorf("content = %q, want image", content)
			}
			if output.ContentType != tt.wantContentType {
				t.Errorf("ContentType = %q, want %q", output.ContentType, tt.wantContentType)
			}
			if len(storage.keys) != 1 || storage.keys[0] != tt.wantKey {
				t.Errorf("Get() keys = %#v, want %q", storage.keys, tt.wantKey)
			}
		})
	}
}

func TestAvatarService_Download_Error(t *testing.T) {
	errStorage := errors.New("storage")
	avatar := model.Avatar{S3Key: "original.png", MIMEType: "image/png"}
	tests := []struct {
		name       string
		input      DownloadInput
		avatar     model.Avatar
		repoErr    error
		storageErr error
		wantErr    error
	}{
		{name: "rejects invalid size", input: DownloadInput{AvatarID: avatarID42, Size: "42x42"}, avatar: avatar, wantErr: ErrInvalidAvatarSize},
		{name: "returns not found thumbnail", input: DownloadInput{AvatarID: avatarID42, Size: "100x100"}, avatar: avatar, wantErr: model.ErrAvatarNotFound},
		{name: "returns repository error", input: DownloadInput{AvatarID: avatarID42}, repoErr: model.ErrAvatarNotFound, wantErr: model.ErrAvatarNotFound},
		{name: "returns storage error", input: DownloadInput{AvatarID: avatarID42}, avatar: avatar, storageErr: errStorage, wantErr: errStorage},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository := &downloadRepository{avatar: tt.avatar, err: tt.repoErr}
			storage := &downloadStorage{err: tt.storageErr}
			avatarService := NewAvatarService(repository, storage, &fakeAvatarEventPublisher{}, func() string { return avatarID42 }, func(_, _, _ string) string { return "" })

			_, err := avatarService.Download(context.Background(), tt.input)
			if err == nil {
				t.Fatal("Download() error = nil")
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Download() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func (r *downloadRepository) Create(context.Context, model.Avatar) (model.Avatar, error) {
	return model.Avatar{}, nil
}

func (r *downloadRepository) GetByID(_ context.Context, id string) (model.Avatar, error) {
	r.avatarIDs = append(r.avatarIDs, id)
	return r.avatar, r.err
}

func (r *downloadRepository) GetCurrentByUserID(_ context.Context, id string) (model.Avatar, error) {
	r.userIDs = append(r.userIDs, id)
	return r.avatar, r.err
}

func (r *downloadRepository) ListByUserID(_ context.Context, id string) ([]model.Avatar, error) {
	r.listUserIDs = append(r.listUserIDs, id)
	return r.avatars, r.listErr
}

func (r *downloadRepository) UpdateUploadStatus(context.Context, string, model.UploadStatus) error {
	return nil
}

func (r *downloadRepository) DeletePermanent(context.Context, string) error { return nil }

func (s *downloadStorage) Put(context.Context, string, io.Reader, int64, string) error { return nil }

func (s *downloadStorage) Get(_ context.Context, key string) (io.ReadCloser, error) {
	s.keys = append(s.keys, key)
	if s.err != nil {
		return nil, s.err
	}
	return io.NopCloser(bytes.NewReader(s.content)), nil
}

func (s *downloadStorage) Delete(context.Context, ...string) error { return nil }
