package rabbitmq

import (
	"context"
	"testing"

	"github.com/xhrobj/gophprofile/internal/model"
)

type recordingPublisher struct {
	calls map[string]int
}

func TestResilientPublisher_DelegatesOperations(t *testing.T) {
	base := &recordingPublisher{calls: make(map[string]int)}
	publisher := NewResilientPublisher(nil, nil)
	publisher.base = base
	ctx := context.Background()
	avatar := model.Avatar{ID: "c0decafe-babe-4bed-b042-feeddeadbeef"}

	if err := publisher.PublishAvatarUploaded(ctx, avatar); err != nil {
		t.Fatalf("PublishAvatarUploaded() error = %v", err)
	}
	if err := publisher.PublishAvatarDeleted(ctx, avatar); err != nil {
		t.Fatalf("PublishAvatarDeleted() error = %v", err)
	}
	if err := publisher.Ping(ctx); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
	if err := publisher.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	for _, operation := range []string{"PublishAvatarUploaded", "PublishAvatarDeleted", "Ping", "Close"} {
		if base.calls[operation] != 1 {
			t.Errorf("%s() calls = %d, want 1", operation, base.calls[operation])
		}
	}
}

func (p *recordingPublisher) PublishAvatarUploaded(context.Context, model.Avatar) error {
	p.calls["PublishAvatarUploaded"]++
	return nil
}

func (p *recordingPublisher) PublishAvatarDeleted(context.Context, model.Avatar) error {
	p.calls["PublishAvatarDeleted"]++
	return nil
}

func (p *recordingPublisher) Ping(context.Context) error {
	p.calls["Ping"]++
	return nil
}

func (p *recordingPublisher) Close() error {
	p.calls["Close"]++
	return nil
}
