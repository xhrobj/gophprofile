package service

import "errors"

var (
	// ErrForbidden означает, что пользователь не имеет права выполнить операцию с аватаркой.
	ErrForbidden = errors.New("forbidden")

	// ErrServiceUnavailable означает, что синхронный сценарий нельзя завершить из-за временной недоступности зависимости.
	ErrServiceUnavailable = errors.New("service unavailable")
)
