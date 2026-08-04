package service

import "errors"

// ErrServiceUnavailable означает, что синхронный сценарий нельзя завершить из-за временной недоступности зависимости.
var ErrServiceUnavailable = errors.New("service unavailable")
