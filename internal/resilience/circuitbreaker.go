// Package resilience содержит общие механизмы защиты вызовов внешних зависимостей.
package resilience

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/sony/gobreaker/v2"
)

const (
	failureThreshold = 3
	openTimeout      = 10 * time.Second
)

type breakerConfig struct {
	failureThreshold uint32
	openTimeout      time.Duration
	excludedErrors   []error
}

// CircuitBreaker защищает одну внешнюю зависимость процесса от повторных заведомо неуспешных вызовов.
type CircuitBreaker struct {
	name  string
	inner *gobreaker.TwoStepCircuitBreaker[struct{}]
}

// ErrCircuitOpen означает, что вызов внешней зависимости отклонён открытым circuit breaker.
var ErrCircuitOpen = errors.New("circuit breaker is open")

// NewCircuitBreaker создаёт circuit breaker для одной внешней зависимости процесса.
func NewCircuitBreaker(name string, lg *slog.Logger, excludedErrors ...error) *CircuitBreaker {
	return newCircuitBreaker(name, lg, breakerConfig{
		failureThreshold: failureThreshold,
		openTimeout:      openTimeout,
		excludedErrors:   excludedErrors,
	})
}

// Execute выполняет операцию через circuit breaker и возвращает её результат.
func Execute[T any](breaker *CircuitBreaker, operation func() (T, error)) (T, error) {
	var zero T

	done, err := breaker.allow()
	if err != nil {
		return zero, err
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			done(fmt.Errorf("dependency operation panic: %v", recovered))
			panic(recovered)
		}
	}()

	result, operationErr := operation()
	done(operationErr)

	return result, operationErr
}

// Do выполняет операцию без возвращаемого значения через circuit breaker.
func Do(breaker *CircuitBreaker, operation func() error) error {
	_, err := Execute(breaker, func() (struct{}, error) {
		return struct{}{}, operation()
	})

	return err
}

func newCircuitBreaker(name string, lg *slog.Logger, cfg breakerConfig) *CircuitBreaker {
	if lg == nil {
		lg = slog.Default()
	}

	breaker := &CircuitBreaker{name: name}
	breaker.inner = gobreaker.NewTwoStepCircuitBreaker[struct{}](gobreaker.Settings{
		Name:        name,
		MaxRequests: 1,
		Timeout:     cfg.openTimeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= cfg.failureThreshold
		},
		OnStateChange: func(dependency string, from, to gobreaker.State) {
			lg.LogAttrs(
				context.Background(),
				slog.LevelInfo,
				"circuit breaker state changed",
				slog.String("dependency", dependency),
				slog.String("from", from.String()),
				slog.String("to", to.String()),
			)
		},
		IsExcluded: func(err error) bool {
			if errors.Is(err, context.Canceled) {
				return true
			}

			for _, excluded := range cfg.excludedErrors {
				if excluded != nil && errors.Is(err, excluded) {
					return true
				}
			}

			return false
		},
	})

	return breaker
}

func (b *CircuitBreaker) allow() (func(error), error) {
	done, err := b.inner.Allow()
	if err == nil {
		return done, nil
	}
	if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
		return nil, fmt.Errorf("%w: %s", ErrCircuitOpen, b.name)
	}

	return nil, fmt.Errorf("allow %s dependency request: %w", b.name, err)
}
