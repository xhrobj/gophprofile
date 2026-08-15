package service

import (
	"errors"
	"testing"

	"github.com/xhrobj/gophprofile/internal/resilience"
)

func TestNormalizeDependencyError(t *testing.T) {
	dependencyErr := errors.New("dependency error")

	tests := []struct {
		name              string
		err               error
		wantUnavailable   bool
		wantOriginalErrIs error
	}{
		{
			name:              "maps open circuit breaker to unavailable",
			err:               resilience.ErrCircuitOpen,
			wantUnavailable:   true,
			wantOriginalErrIs: resilience.ErrCircuitOpen,
		},
		{
			name:              "maps dependency failure to unavailable",
			err:               resilience.ErrDependencyUnavailable,
			wantUnavailable:   true,
			wantOriginalErrIs: resilience.ErrDependencyUnavailable,
		},
		{
			name:              "keeps ordinary dependency error",
			err:               dependencyErr,
			wantOriginalErrIs: dependencyErr,
		},
		{
			name:              "keeps existing unavailable error",
			err:               ErrServiceUnavailable,
			wantUnavailable:   true,
			wantOriginalErrIs: ErrServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeDependencyError(tt.err)
			if errors.Is(got, ErrServiceUnavailable) != tt.wantUnavailable {
				t.Errorf(
					"errors.Is(ErrServiceUnavailable) = %t, want %t",
					errors.Is(got, ErrServiceUnavailable),
					tt.wantUnavailable,
				)
			}
			if !errors.Is(got, tt.wantOriginalErrIs) {
				t.Errorf("normalizeDependencyError() = %v, want to preserve %v", got, tt.wantOriginalErrIs)
			}
		})
	}
}
