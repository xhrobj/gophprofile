package service

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/xhrobj/gophprofile/internal/model"
)

type avatarMetricsRecorder struct {
	uploadResults   []bool
	uploadSizes     []int64
	deletionResults []bool
}

func TestAvatarService_RecordsUploadMetrics(t *testing.T) {
	metrics := &avatarMetricsRecorder{}
	avatarService := NewAvatarService(
		&fakeAvatarRepository{},
		&fakeAvatarStorage{},
		&fakeAvatarEventPublisher{},
		metrics,
		func() string { return avatarID42 },
		func(_, _, _ string) string { return "originals/Alice/avatar.png" },
	)

	content := encodePNG(t)
	if _, err := avatarService.Upload(context.Background(), UploadInput{
		UserID:   "Alice",
		FileName: "avatar.png",
		Content:  content,
	}); err != nil {
		t.Fatalf("Upload() success case error = %v", err)
	}
	if _, err := avatarService.Upload(context.Background(), UploadInput{
		UserID:   "Alice",
		FileName: "avatar.png",
		Content:  []byte("not an image"),
	}); err == nil {
		t.Fatal("Upload() error case = nil, want error")
	}

	if !reflect.DeepEqual(metrics.uploadResults, []bool{true, false}) {
		t.Errorf("upload metric results = %#v, want [true false]", metrics.uploadResults)
	}
	if !reflect.DeepEqual(metrics.uploadSizes, []int64{int64(len(content)), int64(len("not an image"))}) {
		t.Errorf("upload metric sizes = %#v, want successful and failed input sizes", metrics.uploadSizes)
	}
}

func TestAvatarService_RecordsDeletionMetrics(t *testing.T) {
	metrics := &avatarMetricsRecorder{}
	repository := &deleteRepository{avatar: model.Avatar{ID: avatarID42, UserID: "Alice"}}
	avatarService := NewAvatarService(
		repository,
		&fakeAvatarStorage{},
		&deletePublisher{},
		metrics,
		func() string { return avatarID42 },
		func(_, _, _ string) string { return "" },
	)

	if err := avatarService.DeleteByID(context.Background(), avatarID42, "Alice"); err != nil {
		t.Fatalf("DeleteByID() success case error = %v", err)
	}
	if err := avatarService.DeleteByID(context.Background(), avatarID42, "Eve"); err == nil {
		t.Fatal("DeleteByID() error case = nil, want error")
	}

	if !reflect.DeepEqual(metrics.deletionResults, []bool{true, false}) {
		t.Errorf("deletion metric results = %#v, want [true false]", metrics.deletionResults)
	}
}

func (m *avatarMetricsRecorder) ObserveAvatarUpload(success bool, sizeBytes int64, _ time.Duration) {
	m.uploadResults = append(m.uploadResults, success)
	m.uploadSizes = append(m.uploadSizes, sizeBytes)
}

func (m *avatarMetricsRecorder) ObserveAvatarDeletion(success bool) {
	m.deletionResults = append(m.deletionResults, success)
}
