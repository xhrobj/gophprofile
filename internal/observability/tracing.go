package observability

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.39.0"
)

// Tracing управляет жизненным циклом трассировки приложения.
type Tracing struct {
	provider *sdktrace.TracerProvider
}

// NewTracing создаёт и регистрирует OpenTelemetry tracing для сервиса.
func NewTracing(ctx context.Context, serviceName, endpoint string, enabled bool) (*Tracing, error) {
	if !enabled {
		return &Tracing{}, nil
	}

	endpoint, err := traceEndpoint(endpoint)
	if err != nil {
		return nil, err
	}

	res, err := traceResource(serviceName)
	if err != nil {
		return nil, err
	}

	exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint))
	if err != nil {
		return nil, fmt.Errorf("create OTLP trace exporter: %w", err)
	}

	return installTracing(res, exporter), nil
}

// Shutdown сбрасывает накопленные spans и останавливает tracing.
func (t *Tracing) Shutdown(ctx context.Context) error {
	if t == nil || t.provider == nil {
		return nil
	}

	if err := t.provider.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown tracing: %w", err)
	}

	return nil
}

func traceEndpoint(value string) (string, error) {
	value = strings.TrimSpace(value)
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("invalid OTLP endpoint %q", value)
	}

	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/v1/traces"

	return parsed.String(), nil
}

func traceResource(serviceName string) (*resource.Resource, error) {
	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		return nil, fmt.Errorf("service name must not be empty")
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewSchemaless(semconv.ServiceName(serviceName)),
	)
	if err != nil {
		return nil, fmt.Errorf("create tracing resource: %w", err)
	}

	return res, nil
}

func installTracing(res *resource.Resource, exporter sdktrace.SpanExporter) *Tracing {
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	return &Tracing{provider: provider}
}
