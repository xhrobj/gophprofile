// Package logger создаёт структурированные логгеры приложения и добавляет к ним контекстные поля.
package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"go.opentelemetry.io/otel/trace"
)

const (
	serviceKey   = "service"
	requestIDKey = "request_id"
	messageIDKey = "message_id"
	traceIDKey   = "trace_id"
	spanIDKey    = "span_id"
)

type traceHandler struct {
	base slog.Handler
}

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

func (h traceHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.base.Enabled(ctx, level)
}

func (h traceHandler) Handle(ctx context.Context, record slog.Record) error {
	spanContext := trace.SpanContextFromContext(ctx)
	if spanContext.IsValid() {
		record.AddAttrs(
			slog.String(traceIDKey, spanContext.TraceID().String()),
			slog.String(spanIDKey, spanContext.SpanID().String()),
		)
	}

	return h.base.Handle(ctx, record)
}

func (h traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return traceHandler{base: h.base.WithAttrs(attrs)}
}

func (h traceHandler) WithGroup(name string) slog.Handler {
	return traceHandler{base: h.base.WithGroup(name)}
}

func newLogger(service, level string, output io.Writer) (*slog.Logger, error) {
	if strings.TrimSpace(service) == "" {
		return nil, fmt.Errorf("service name must not be empty")
	}

	parsedLevel, err := parseLevel(level)
	if err != nil {
		return nil, err
	}

	baseHandler := slog.NewJSONHandler(output, &slog.HandlerOptions{
		Level:       parsedLevel,
		ReplaceAttr: normalizeLevel,
	})

	return slog.New(traceHandler{base: baseHandler}).With(slog.String(serviceKey, service)), nil
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
