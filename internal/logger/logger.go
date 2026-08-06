// Package logger создаёт структурированные логгеры приложения и добавляет к ним контекстные поля.
package logger

import (
	"fmt"
	"strings"

	"go.uber.org/zap"
)

const (
	serviceKey   = "service"
	requestIDKey = "request_id"
	messageIDKey = "message_id"
)

// New создаёт production-логгер для указанного сервиса и уровня логирования.
func New(service, level string) (*zap.Logger, error) {
	if strings.TrimSpace(service) == "" {
		return nil, fmt.Errorf("service name must not be empty")
	}

	cfg := zap.NewProductionConfig()

	parsedLevel, err := parseLevel(level)
	if err != nil {
		return nil, err
	}
	cfg.Level = parsedLevel

	baseLogger, err := cfg.Build()
	if err != nil {
		return nil, fmt.Errorf("build logger: %w", err)
	}

	return baseLogger.With(zap.String(serviceKey, service)), nil
}

// WithRequestID возвращает дочерний логгер с идентификатором HTTP-запроса.
func WithRequestID(base *zap.Logger, requestID string) *zap.Logger {
	return base.With(zap.String(requestIDKey, requestID))
}

// WithMessageID возвращает дочерний логгер с идентификатором сообщения.
func WithMessageID(base *zap.Logger, messageID string) *zap.Logger {
	return base.With(zap.String(messageIDKey, messageID))
}

func parseLevel(value string) (zap.AtomicLevel, error) {
	levelName := strings.ToLower(strings.TrimSpace(value))

	level, err := zap.ParseAtomicLevel(levelName)
	if err != nil {
		return zap.AtomicLevel{}, fmt.Errorf("parse log level %q: %w", value, err)
	}

	return level, nil
}
