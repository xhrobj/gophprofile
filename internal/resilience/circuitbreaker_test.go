package resilience

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"runtime"
	"testing"
	"time"
)

func TestCircuitBreakerOpenFailFastAndRecover(t *testing.T) {
	const threshold = 3

	breaker := newCircuitBreaker("postgresql", discardLogger(), breakerConfig{
		failureThreshold: threshold,
		openTimeout:      time.Millisecond,
	})
	dependencyErr := errors.New("dependency unavailable")
	calls := 0

	for range threshold {
		err := Do(breaker, func() error {
			calls++
			return dependencyErr
		})
		if !errors.Is(err, dependencyErr) {
			t.Fatalf("Do() error = %v, want dependency error", err)
		}
	}

	err := Do(breaker, func() error {
		calls++
		return nil
	})
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("Do() open error = %v, want ErrCircuitOpen", err)
	}
	if calls != threshold {
		t.Fatalf("dependency calls while open = %d, want %d", calls, threshold)
	}

	deadline := time.Now().Add(100 * time.Millisecond)
	for {
		err = Do(breaker, func() error {
			calls++
			return nil
		})
		if err == nil {
			break
		}
		if !errors.Is(err, ErrCircuitOpen) {
			t.Fatalf("Do() half-open probe error = %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("circuit breaker did not become half-open before deadline")
		}

		runtime.Gosched()
	}

	if err := Do(breaker, func() error {
		calls++
		return nil
	}); err != nil {
		t.Fatalf("Do() after recovery error = %v", err)
	}
	if calls != threshold+2 {
		t.Fatalf("dependency calls after recovery = %d, want %d", calls, threshold+2)
	}
}

func TestCircuitBreakerExcludesCancellationAndBusinessErrors(t *testing.T) {
	businessErr := errors.New("business outcome")
	breaker := newCircuitBreaker("postgresql", discardLogger(), breakerConfig{
		failureThreshold: 1,
		openTimeout:      time.Minute,
		excludedErrors:   []error{businessErr},
	})

	for _, err := range []error{
		fmtWrapped(context.Canceled),
		fmtWrapped(businessErr),
	} {
		if got := Do(breaker, func() error { return err }); !errors.Is(got, err) {
			t.Fatalf("Do() error = %v, want %v", got, err)
		}
	}

	called := false
	if err := Do(breaker, func() error {
		called = true
		return nil
	}); err != nil {
		t.Fatalf("Do() after excluded errors = %v", err)
	}
	if !called {
		t.Fatal("dependency call was rejected after excluded errors")
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func fmtWrapped(err error) error {
	return fmt.Errorf("wrapped: %w", err)
}
