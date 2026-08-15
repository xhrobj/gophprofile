package service

import (
	"errors"

	"github.com/xhrobj/gophprofile/internal/resilience"
)

var (
	// ErrForbidden означает, что пользователь не имеет права выполнить операцию с аватаркой.
	ErrForbidden = errors.New("forbidden")

	// ErrServiceUnavailable означает, что синхронный сценарий
	// нельзя завершить из-за временной недоступности зависимости.
	ErrServiceUnavailable = errors.New("service unavailable")
)

func normalizeDependencyError(err error) error {
	if err == nil || errors.Is(err, ErrServiceUnavailable) {
		return err
	}
	if errors.Is(err, resilience.ErrCircuitOpen) || errors.Is(err, resilience.ErrDependencyUnavailable) {
		return errors.Join(ErrServiceUnavailable, err)
	}

	return err
}
