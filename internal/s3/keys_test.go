package s3

import (
	"testing"

	"github.com/xhrobj/gophprofile/internal/model"
)

func TestOriginalKey(t *testing.T) {
	got := OriginalKey("alice", "avatar-42", "avatar.jpg")
	want := "originals/alice/avatar-42/avatar.jpg"

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
			want: "thumbnails/alice/avatar-42/100x100.jpg",
		},
		{
			name: "300x300",
			size: model.ThumbnailSize300x300,
			want: "thumbnails/alice/avatar-42/300x300.jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ThumbnailKey("alice", "avatar-42", tt.size)
			if got != tt.want {
				t.Errorf("ThumbnailKey() = %q, want %q", got, tt.want)
			}
		})
	}
}
