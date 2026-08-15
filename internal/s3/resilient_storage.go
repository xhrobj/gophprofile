package s3

import (
	"context"
	"io"
	"log/slog"

	"github.com/xhrobj/gophprofile/internal/resilience"
)

// ResilientStorage защищает runtime S3-операции общим circuit breaker процесса.
type ResilientStorage struct {
	base    *Storage
	breaker *resilience.CircuitBreaker
}

// NewResilientStorage добавляет circuit breaker к S3 storage.
func NewResilientStorage(base *Storage, lg *slog.Logger) *ResilientStorage {
	return &ResilientStorage{
		base:    base,
		breaker: resilience.NewCircuitBreaker("s3", lg),
	}
}

func (s *ResilientStorage) Put(
	ctx context.Context,
	key string,
	reader io.Reader,
	size int64,
	contentType string,
) error {
	return resilience.Do(s.breaker, func() error {
		return s.base.Put(ctx, key, reader, size, contentType)
	})
}

func (s *ResilientStorage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	return resilience.Execute(s.breaker, func() (io.ReadCloser, error) {
		return s.base.Get(ctx, key)
	})
}

func (s *ResilientStorage) Delete(ctx context.Context, keys ...string) error {
	return resilience.Do(s.breaker, func() error {
		return s.base.Delete(ctx, keys...)
	})
}

// Ping намеренно обходит circuit breaker: readiness должна проверять реальную dependency.
func (s *ResilientStorage) Ping(ctx context.Context) error {
	return s.base.Ping(ctx)
}
