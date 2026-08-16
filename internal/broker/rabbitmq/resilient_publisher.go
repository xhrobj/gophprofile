package rabbitmq

import (
	"context"
	"log/slog"

	"github.com/xhrobj/gophprofile/internal/model"
	"github.com/xhrobj/gophprofile/internal/resilience"
)

type publisher interface {
	PublishAvatarUploaded(context.Context, model.Avatar) error
	PublishAvatarDeleted(context.Context, model.Avatar) error
	Ping(context.Context) error
	Close() error
}

// ResilientPublisher защищает runtime publish-операции общим RabbitMQ circuit breaker процесса.
type ResilientPublisher struct {
	base    publisher
	breaker *resilience.CircuitBreaker
}

var _ publisher = (*Publisher)(nil)

// NewResilientPublisher добавляет circuit breaker к RabbitMQ publisher.
func NewResilientPublisher(base *Publisher, lg *slog.Logger) *ResilientPublisher {
	return &ResilientPublisher{
		base:    base,
		breaker: resilience.NewCircuitBreaker("rabbitmq", lg),
	}
}

func (p *ResilientPublisher) PublishAvatarUploaded(ctx context.Context, avatar model.Avatar) error {
	return resilience.Do(p.breaker, func() error {
		return p.base.PublishAvatarUploaded(ctx, avatar)
	})
}

func (p *ResilientPublisher) PublishAvatarDeleted(ctx context.Context, avatar model.Avatar) error {
	return resilience.Do(p.breaker, func() error {
		return p.base.PublishAvatarDeleted(ctx, avatar)
	})
}

// Ping намеренно обходит circuit breaker: readiness должна проверять реальную dependency.
func (p *ResilientPublisher) Ping(ctx context.Context) error {
	return p.base.Ping(ctx)
}

func (p *ResilientPublisher) Close() error {
	return p.base.Close()
}
