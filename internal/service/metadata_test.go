package service

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/xhrobj/gophprofile/internal/model"
)

func TestAvatarService_GetMetadata(t *testing.T) {
	want := model.Avatar{ID: avatarID42, UserID: "Alice"}
	repository := &downloadRepository{avatar: want}
	avatarService := NewAvatarService(repository, &downloadStorage{}, &fakeAvatarEventPublisher{}, nil, func() string { return avatarID42 }, func(_, _, _ string) string { return "" })

	got, err := avatarService.GetMetadata(context.Background(), avatarID42)
	if err != nil {
		t.Fatalf("GetMetadata() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetMetadata() avatar = %+v, want %+v", got, want)
	}
	if len(repository.avatarIDs) != 1 || repository.avatarIDs[0] != avatarID42 {
		t.Errorf("GetByID() calls = %#v, want %q", repository.avatarIDs, avatarID42)
	}
}

func TestAvatarService_GetMetadata_Error(t *testing.T) {
	repository := &downloadRepository{err: model.ErrAvatarNotFound}
	avatarService := NewAvatarService(repository, &downloadStorage{}, &fakeAvatarEventPublisher{}, nil, func() string { return avatarID42 }, func(_, _, _ string) string { return "" })

	_, err := avatarService.GetMetadata(context.Background(), avatarID42)
	if !errors.Is(err, model.ErrAvatarNotFound) {
		t.Errorf("GetMetadata() error = %v, want %v", err, model.ErrAvatarNotFound)
	}
}

func TestAvatarService_ListByUserID(t *testing.T) {
	want := []model.Avatar{{ID: avatarID42, UserID: "Alice"}}
	repository := &downloadRepository{avatars: want}
	avatarService := NewAvatarService(repository, &downloadStorage{}, &fakeAvatarEventPublisher{}, nil, func() string { return avatarID42 }, func(_, _, _ string) string { return "" })

	got, err := avatarService.ListByUserID(context.Background(), "Alice")
	if err != nil {
		t.Fatalf("ListByUserID() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ListByUserID() avatars = %+v, want %+v", got, want)
	}
	if len(repository.listUserIDs) != 1 || repository.listUserIDs[0] != "Alice" {
		t.Errorf("ListByUserID() calls = %#v, want Alice", repository.listUserIDs)
	}
}

func TestAvatarService_ListByUserID_Error(t *testing.T) {
	wantErr := errors.New("list avatars")
	repository := &downloadRepository{listErr: wantErr}
	avatarService := NewAvatarService(repository, &downloadStorage{}, &fakeAvatarEventPublisher{}, nil, func() string { return avatarID42 }, func(_, _, _ string) string { return "" })

	_, err := avatarService.ListByUserID(context.Background(), "Alice")
	if !errors.Is(err, wantErr) {
		t.Errorf("ListByUserID() error = %v, want %v", err, wantErr)
	}
}
