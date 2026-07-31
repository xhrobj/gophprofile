package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadServer(t *testing.T) {
	setValidEnvironment(t)
	t.Setenv(envLogLevel, "WARN")
	t.Setenv(envRabbitMQQueue, "")

	got, err := LoadServer()
	if err != nil {
		t.Fatalf("LoadServer() error = %v", err)
	}

	want := Server{
		Common: Common{
			DatabaseDSN: "postgres://gophprofile:gophprofile@localhost:5432/gophprofile?sslmode=disable",

			S3Endpoint:  "localhost:9000",
			S3AccessKey: "minioadmin",
			S3SecretKey: "minioadmin",
			S3Bucket:    "avatars",
			S3UseSSL:    false,

			RabbitMQURL:      "amqp://guest:guest@localhost:5672/",
			RabbitMQExchange: "avatars.exchange",

			LogLevel: "warn",
		},
		HTTPAddress:     ":8080",
		MaxUploadSize:   10 * 1024 * 1024,
		ShutdownTimeout: 10 * time.Second,
	}

	if got != want {
		t.Errorf("LoadServer() = %#v, want %#v", got, want)
	}
}

func TestLoadWorker(t *testing.T) {
	setValidEnvironment(t)
	t.Setenv(envHTTPAddress, "")
	t.Setenv(envMaxUploadSize, "")
	t.Setenv(envShutdownTimeout, "")

	got, err := LoadWorker()
	if err != nil {
		t.Fatalf("LoadWorker() error = %v", err)
	}

	want := Worker{
		Common: Common{
			DatabaseDSN: "postgres://gophprofile:gophprofile@localhost:5432/gophprofile?sslmode=disable",

			S3Endpoint:  "localhost:9000",
			S3AccessKey: "minioadmin",
			S3SecretKey: "minioadmin",
			S3Bucket:    "avatars",
			S3UseSSL:    false,

			RabbitMQURL:      "amqp://guest:guest@localhost:5672/",
			RabbitMQExchange: "avatars.exchange",

			LogLevel: "info",
		},
		RabbitMQQueue: "avatars.processing",
	}

	if got != want {
		t.Errorf("LoadWorker() = %#v, want %#v", got, want)
	}
}

func TestLoadServer_Validation(t *testing.T) {
	tests := []struct {
		name    string
		envName string
		value   string
	}{
		{
			name:    "missing HTTP address",
			envName: envHTTPAddress,
			value:   "",
		},
		{
			name:    "missing max upload size",
			envName: envMaxUploadSize,
			value:   "",
		},
		{
			name:    "non-integer max upload size",
			envName: envMaxUploadSize,
			value:   "ten megabytes",
		},
		{
			name:    "zero max upload size",
			envName: envMaxUploadSize,
			value:   "0",
		},
		{
			name:    "negative max upload size",
			envName: envMaxUploadSize,
			value:   "-1",
		},
		{
			name:    "invalid shutdown timeout",
			envName: envShutdownTimeout,
			value:   "soon",
		},
		{
			name:    "zero shutdown timeout",
			envName: envShutdownTimeout,
			value:   "0s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setValidEnvironment(t)
			t.Setenv(tt.envName, tt.value)

			_, err := LoadServer()
			if err == nil {
				t.Fatal("LoadServer() error = nil, want validation error")
			}

			if !strings.Contains(err.Error(), tt.envName) {
				t.Errorf("LoadServer() error = %q, want variable name %q", err, tt.envName)
			}
		})
	}
}

func TestLoadWorker_Validation(t *testing.T) {
	tests := []struct {
		name    string
		envName string
		value   string
	}{
		{
			name:    "missing database DSN",
			envName: envDatabaseDSN,
			value:   "",
		},
		{
			name:    "missing S3 endpoint",
			envName: envS3Endpoint,
			value:   "",
		},
		{
			name:    "missing S3 access key",
			envName: envS3AccessKey,
			value:   "",
		},
		{
			name:    "missing S3 secret key",
			envName: envS3SecretKey,
			value:   "",
		},
		{
			name:    "missing S3 bucket",
			envName: envS3Bucket,
			value:   "",
		},
		{
			name:    "invalid S3 SSL flag",
			envName: envS3UseSSL,
			value:   "sometimes",
		},
		{
			name:    "missing RabbitMQ URL",
			envName: envRabbitMQURL,
			value:   "",
		},
		{
			name:    "missing RabbitMQ exchange",
			envName: envRabbitMQExchange,
			value:   "",
		},
		{
			name:    "missing RabbitMQ queue",
			envName: envRabbitMQQueue,
			value:   "",
		},
		{
			name:    "unknown log level",
			envName: envLogLevel,
			value:   "trace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setValidEnvironment(t)
			t.Setenv(tt.envName, tt.value)

			_, err := LoadWorker()
			if err == nil {
				t.Fatal("LoadWorker() error = nil, want validation error")
			}
			if !strings.Contains(err.Error(), tt.envName) {
				t.Errorf("LoadWorker() error = %q, want variable name %q", err, tt.envName)
			}
		})
	}
}

func setValidEnvironment(t *testing.T) {
	t.Helper()

	t.Setenv(envHTTPAddress, ":8080")
	t.Setenv(envDatabaseDSN, "postgres://gophprofile:gophprofile@localhost:5432/gophprofile?sslmode=disable")

	t.Setenv(envS3Endpoint, "localhost:9000")
	t.Setenv(envS3AccessKey, "minioadmin")
	t.Setenv(envS3SecretKey, "minioadmin")
	t.Setenv(envS3Bucket, "avatars")
	t.Setenv(envS3UseSSL, "false")

	t.Setenv(envRabbitMQURL, "amqp://guest:guest@localhost:5672/")
	t.Setenv(envRabbitMQExchange, "avatars.exchange")
	t.Setenv(envRabbitMQQueue, "avatars.processing")

	t.Setenv(envMaxUploadSize, "10485760")
	t.Setenv(envLogLevel, "info")
	t.Setenv(envShutdownTimeout, "10s")
}
