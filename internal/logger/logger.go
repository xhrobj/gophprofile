// Package logger создаёт структурированные логгеры приложения и добавляет к ним контекстные поля.
package logger

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

const (
	serviceKey   = "service"
	requestIDKey = "request_id"
	messageIDKey = "message_id"
)

// New создаёт production-логгер для указанного сервиса и уровня логирования.
func New(service, level string) (*slog.Logger, error) {
	return newLogger(service, level, os.Stdout)
}

// WithRequestID возвращает дочерний логгер с идентификатором HTTP-запроса.
func WithRequestID(base *slog.Logger, requestID string) *slog.Logger {
	return base.With(slog.String(requestIDKey, requestID))
}

// WithMessageID возвращает дочерний логгер с идентификатором сообщения.
func WithMessageID(base *slog.Logger, messageID string) *slog.Logger {
	return base.With(slog.String(messageIDKey, messageID))
}

func newLogger(service, level string, output io.Writer) (*slog.Logger, error) {
	if strings.TrimSpace(service) == "" {
		return nil, fmt.Errorf("service name must not be empty")
	}

	parsedLevel, err := parseLevel(level)
	if err != nil {
		return nil, err
	}

	handler := slog.NewJSONHandler(output, &slog.HandlerOptions{
		Level:       parsedLevel,
		ReplaceAttr: normalizeLevel,
	})

	return slog.New(handler).With(slog.String(serviceKey, service)), nil
}

func parseLevel(value string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("parse log level %q: unsupported value", value)
	}
}

func normalizeLevel(_ []string, attr slog.Attr) slog.Attr {
	if attr.Key != slog.LevelKey {
		return attr
	}

	level, ok := attr.Value.Any().(slog.Level)
	if !ok {
		return attr
	}

	return slog.String(slog.LevelKey, strings.ToLower(level.String()))
}
