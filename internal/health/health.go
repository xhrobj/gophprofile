package health

import (
	"context"
	"time"
)

const (
	statusOK          = "ok"
	statusUnavailable = "unavailable"

	componentStatusError = "error"

	checkTimeout = time.Second
)

// Pinger проверяет доступность внешней зависимости.
type Pinger interface {
	Ping(context.Context) error
}

// Component описывает результат проверки одной зависимости.
type Component struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// Components содержит состояния обязательных зависимостей приложения.
type Components struct {
	Database Component `json:"database"`
	S3       Component `json:"s3"`
	Broker   Component `json:"broker"`
}

// Report содержит итоговый статус готовности приложения.
type Report struct {
	Status     string     `json:"status"`
	Components Components `json:"components"`
}

// Checker проверяет готовность PostgreSQL, S3 и RabbitMQ.
type Checker struct {
	database Pinger
	s3       Pinger
	broker   Pinger
	timeout  time.Duration
}

// NewChecker создаёт проверку обязательных зависимостей приложения.
func NewChecker(database, s3, broker Pinger) *Checker {
	return &Checker{
		database: database,
		s3:       s3,
		broker:   broker,
		timeout:  checkTimeout,
	}
}

// Available сообщает, доступны ли все обязательные зависимости.
func (r Report) Available() bool {
	return r.Status == statusOK
}

// Check проверяет каждую зависимость с отдельным ограничением по времени.
func (c *Checker) Check(ctx context.Context) Report {
	components := Components{
		Database: checkComponent(ctx, c.database, c.timeout),
		S3:       checkComponent(ctx, c.s3, c.timeout),
		Broker:   checkComponent(ctx, c.broker, c.timeout),
	}

	status := statusOK
	if components.Database.Status != statusOK ||
		components.S3.Status != statusOK ||
		components.Broker.Status != statusOK {
		status = statusUnavailable
	}

	return Report{
		Status:     status,
		Components: components,
	}
}

func checkComponent(ctx context.Context, dependency Pinger, timeout time.Duration) Component {
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := dependency.Ping(checkCtx); err != nil {
		return Component{
			Status: componentStatusError,
			Error:  err.Error(),
		}
	}

	return Component{Status: statusOK}
}
