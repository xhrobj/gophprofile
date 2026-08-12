package s3

import (
	"testing"

	"github.com/xhrobj/gophprofile/internal/model"
)

func TestOriginalKey(t *testing.T) {
	got := OriginalKey("alice", "c0decafe-babe-4bed-b042-feeddeadbeef", "avatar.jpg")
	want := "originals/alice/c0decafe-babe-4bed-b042-feeddeadbeef/avatar.jpg"

	if got != want {
		t.Errorf("OriginalKey() = %q, want %q", got, want)
	}
}

func TestThumbnailKey(t *testing.T) {
	tests := []struct {
		name string
		size model.ThumbnailSize
		want string
	}{
		{
			name: "100x100",
			size: model.ThumbnailSize100x100,
			want: "thumbnails/alice/c0decafe-babe-4bed-b042-feeddeadbeef/100x100.jpg",
		},
		{
			name: "300x300",
			size: model.ThumbnailSize300x300,
			want: "thumbnails/alice/c0decafe-babe-4bed-b042-feeddeadbeef/300x300.jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ThumbnailKey("alice", "c0decafe-babe-4bed-b042-feeddeadbeef", tt.size)
			if got != tt.want {
				t.Errorf("ThumbnailKey() = %q, want %q", got, tt.want)
			}
		})
	}
}
