package s3

import (
	"context"
	"io"
	"strings"
	"testing"
)

type recordingStorage struct {
	calls map[string]int
}

func TestResilientStorage_DelegatesOperations(t *testing.T) {
	base := &recordingStorage{calls: make(map[string]int)}
	storage := NewResilientStorage(nil, nil)
	storage.base = base
	ctx := context.Background()

	if err := storage.Put(ctx, "avatar.webp", strings.NewReader("image"), 5, "image/webp"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	reader, err := storage.Get(ctx, "avatar.webp")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read Get() result: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close Get() result: %v", err)
	}
	if string(data) != "image" {
		t.Fatalf("Get() data = %q, want %q", data, "image")
	}

	if err := storage.Delete(ctx, "avatar.webp", "avatar-100.webp"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if err := storage.Ping(ctx); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}

	for _, operation := range []string{"Put", "Get", "Delete", "Ping"} {
		if base.calls[operation] != 1 {
			t.Errorf("%s() calls = %d, want 1", operation, base.calls[operation])
		}
	}
}

func (s *recordingStorage) Put(context.Context, string, io.Reader, int64, string) error {
	s.calls["Put"]++
	return nil
}

func (s *recordingStorage) Get(context.Context, string) (io.ReadCloser, error) {
	s.calls["Get"]++
	return io.NopCloser(strings.NewReader("image")), nil
}

func (s *recordingStorage) Delete(context.Context, ...string) error {
	s.calls["Delete"]++
	return nil
}

func (s *recordingStorage) Ping(context.Context) error {
	s.calls["Ping"]++
	return nil
}
