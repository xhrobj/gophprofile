// Package config загружает и проверяет конфигурацию приложения из переменных окружения.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	envDatabaseDSN = "DATABASE_DSN"

	envS3Endpoint  = "S3_ENDPOINT"
	envS3AccessKey = "S3_ACCESS_KEY"
	envS3SecretKey = "S3_SECRET_KEY"
	envS3Bucket    = "S3_BUCKET"
	envS3UseSSL    = "S3_USE_SSL"

	envRabbitMQURL      = "RABBITMQ_URL"
	envRabbitMQExchange = "RABBITMQ_EXCHANGE"
	envRabbitMQQueue    = "RABBITMQ_QUEUE"

	envTracingEnabled = "TRACING_ENABLED"
	envOTLPEndpoint   = "OTEL_EXPORTER_OTLP_ENDPOINT"

	envLogLevel = "LOG_LEVEL"

	envHTTPAddress     = "HTTP_ADDRESS"
	envMaxUploadSize   = "MAX_UPLOAD_SIZE"
	envShutdownTimeout = "SHUTDOWN_TIMEOUT"
)

// Common содержит параметры конфигурации, общие для Сервера и Воркера.
type Common struct {
	DatabaseDSN string

	S3Endpoint  string
	S3AccessKey string
	S3SecretKey string
	S3Bucket    string
	S3UseSSL    bool

	RabbitMQURL      string
	RabbitMQExchange string
	RabbitMQQueue    string

	TracingEnabled  bool
	OTLPEndpoint    string
	ShutdownTimeout time.Duration

	LogLevel string
}

// Server содержит конфигурацию HTTP-сервера.
type Server struct {
	Common
	HTTPAddress   string
	MaxUploadSize int64
}

// Worker содержит конфигурацию фонового воркера.
type Worker struct {
	Common
}

// LoadServer загружает и проверяет конфигурацию HTTP-сервера.
func LoadServer() (Server, error) {
	common, err := loadCommon()
	if err != nil {
		return Server{}, err
	}

	httpAddress, err := required(envHTTPAddress)
	if err != nil {
		return Server{}, err
	}

	maxUploadSize, err := positiveInt64(envMaxUploadSize)
	if err != nil {
		return Server{}, err
	}

	return Server{
		Common:        common,
		HTTPAddress:   httpAddress,
		MaxUploadSize: maxUploadSize,
	}, nil
}

// LoadWorker загружает и проверяет конфигурацию фонового воркера.
func LoadWorker() (Worker, error) {
	common, err := loadCommon()
	if err != nil {
		return Worker{}, err
	}

	return Worker{Common: common}, nil
}

func loadCommon() (Common, error) {
	databaseDSN, err := required(envDatabaseDSN)
	if err != nil {
		return Common{}, err
	}

	s3Endpoint, err := required(envS3Endpoint)
	if err != nil {
		return Common{}, err
	}

	s3AccessKey, err := required(envS3AccessKey)
	if err != nil {
		return Common{}, err
	}

	s3SecretKey, err := required(envS3SecretKey)
	if err != nil {
		return Common{}, err
	}

	s3Bucket, err := required(envS3Bucket)
	if err != nil {
		return Common{}, err
	}

	s3UseSSL, err := boolean(envS3UseSSL)
	if err != nil {
		return Common{}, err
	}

	rabbitMQURL, err := required(envRabbitMQURL)
	if err != nil {
		return Common{}, err
	}

	rabbitMQExchange, err := required(envRabbitMQExchange)
	if err != nil {
		return Common{}, err
	}

	rabbitMQQueue, err := required(envRabbitMQQueue)
	if err != nil {
		return Common{}, err
	}

	tracingEnabled, err := optionalBoolean(envTracingEnabled)
	if err != nil {
		return Common{}, err
	}

	otlpExporterEndpoint := strings.TrimSpace(os.Getenv(envOTLPEndpoint))
	if tracingEnabled && otlpExporterEndpoint == "" {
		return Common{}, fmt.Errorf("environment variable %s is required when tracing is enabled", envOTLPEndpoint)
	}

	shutdownTimeout, err := positiveDuration(envShutdownTimeout)
	if err != nil {
		return Common{}, err
	}

	logLevel, err := logLevel()
	if err != nil {
		return Common{}, err
	}

	return Common{
		DatabaseDSN: databaseDSN,

		S3Endpoint:  s3Endpoint,
		S3AccessKey: s3AccessKey,
		S3SecretKey: s3SecretKey,
		S3Bucket:    s3Bucket,
		S3UseSSL:    s3UseSSL,

		RabbitMQURL:      rabbitMQURL,
		RabbitMQExchange: rabbitMQExchange,
		RabbitMQQueue:    rabbitMQQueue,

		TracingEnabled:  tracingEnabled,
		OTLPEndpoint:    otlpExporterEndpoint,
		ShutdownTimeout: shutdownTimeout,

		LogLevel: logLevel,
	}, nil
}

func required(name string) (string, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return "", fmt.Errorf("environment variable %s is required", name)
	}

	return value, nil
}

func boolean(name string) (bool, error) {
	value, err := required(name)
	if err != nil {
		return false, err
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("environment variable %s must be a boolean: %w", name, err)
	}

	return parsed, nil
}

func optionalBoolean(name string) (bool, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return false, nil
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("environment variable %s must be a boolean: %w", name, err)
	}

	return parsed, nil
}

func positiveInt64(name string) (int64, error) {
	value, err := required(name)
	if err != nil {
		return 0, err
	}

	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("environment variable %s must be an integer: %w", name, err)
	}

	if parsed <= 0 {
		return 0, fmt.Errorf("environment variable %s must be greater than zero", name)
	}

	return parsed, nil
}

func positiveDuration(name string) (time.Duration, error) {
	value, err := required(name)
	if err != nil {
		return 0, err
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("environment variable %s must be a duration: %w", name, err)
	}

	if parsed <= 0 {
		return 0, fmt.Errorf("environment variable %s must be greater than zero", name)
	}

	return parsed, nil
}

func logLevel() (string, error) {
	value, err := required(envLogLevel)
	if err != nil {
		return "", err
	}

	level := strings.ToLower(value)
	switch level {
	case "debug", "info", "warn", "error":
		return level, nil
	default:
		return "", fmt.Errorf(
			"environment variable %s must be one of debug, info, warn or error",
			envLogLevel,
		)
	}
}
