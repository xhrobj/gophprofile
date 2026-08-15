package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/xhrobj/gophprofile/internal/broker/rabbitmq"
	"github.com/xhrobj/gophprofile/internal/config"
	"github.com/xhrobj/gophprofile/internal/handler"
	"github.com/xhrobj/gophprofile/internal/health"
	"github.com/xhrobj/gophprofile/internal/logger"
	"github.com/xhrobj/gophprofile/internal/observability"
	"github.com/xhrobj/gophprofile/internal/postgres"
	"github.com/xhrobj/gophprofile/internal/s3"
	"github.com/xhrobj/gophprofile/internal/server"
	"github.com/xhrobj/gophprofile/internal/service"
)

const (
	// serviceName определяет имя сервиса в структурированных логах
	serviceName = "server"

	// readHeaderTimeout ограничивает время чтения HTTP-заголовков запроса
	readHeaderTimeout = 5 * time.Second

	// idleTimeout ограничивает время простоя keep-alive-соединения
	idleTimeout = 30 * time.Second
)

func main() {
	if err := printBanner(os.Stdout); err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	cfg, err := config.LoadServer()
	if err != nil {
		return fmt.Errorf("load server config: %w", err)
	}

	lg, err := logger.New(serviceName, cfg.LogLevel)
	if err != nil {
		return fmt.Errorf("create server logger: %w", err)
	}

	tracing, err := observability.NewTracing(ctx, serviceName, cfg.OTLPEndpoint, cfg.TracingEnabled)
	if err != nil {
		return fmt.Errorf("create server tracing: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()

		if shutdownErr := tracing.Shutdown(shutdownCtx); shutdownErr != nil {
			lg.WarnContext(shutdownCtx, "failed to shutdown tracing", slog.Any("error", shutdownErr))
		}
	}()

	pool, err := postgres.Open(ctx, cfg.DatabaseDSN)
	if err != nil {
		return fmt.Errorf("open PostgreSQL: %w", err)
	}
	defer pool.Close()

	storageClient, err := s3.Open(
		ctx,
		cfg.S3Endpoint,
		cfg.S3AccessKey,
		cfg.S3SecretKey,
		cfg.S3Bucket,
		cfg.S3UseSSL,
	)
	if err != nil {
		return fmt.Errorf("open S3 storage: %w", err)
	}
	storage := s3.NewResilientStorage(storageClient, lg)

	publisherClient, err := rabbitmq.OpenPublisher(cfg.RabbitMQURL, cfg.RabbitMQExchange, cfg.RabbitMQQueue)
	if err != nil {
		return fmt.Errorf("open RabbitMQ publisher: %w", err)
	}
	publisher := rabbitmq.NewResilientPublisher(publisherClient, lg)
	defer func() {
		if closeErr := publisher.Close(); closeErr != nil {
			lg.WarnContext(ctx, "failed to close RabbitMQ publisher", slog.Any("error", closeErr))
		}
	}()

	metrics := observability.NewServerMetrics()
	metrics.RegisterPostgreSQLPool(pool)

	avatarRepository := postgres.NewAvatarRepository(pool)
	metrics.RegisterAvatarStorageUsage(ctx, avatarRepository)
	resilientAvatarRepository := postgres.NewResilientAvatarRepository(avatarRepository, lg)
	avatarService := service.NewAvatarService(
		resilientAvatarRepository,
		storage,
		publisher,
		metrics,
		uuid.NewString,
		s3.OriginalKey,
	)
	healthChecker := health.NewChecker(pool, storage, publisher)

	listener, err := net.Listen("tcp", cfg.HTTPAddress)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.HTTPAddress, err)
	}

	errorLog := slog.NewLogLogger(
		lg.With(slog.String("logger", "net/http")).Handler(),
		slog.LevelError,
	)

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddress,
		Handler:           handler.NewRouter(lg, avatarService, healthChecker, cfg.MaxUploadSize, metrics),
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
		ErrorLog:          errorLog,
	}

	return server.Run(ctx, listener, httpServer, cfg.ShutdownTimeout, lg)
}

func printBanner(output io.Writer) error {
	const banner = `
  ________              .__   __________                _____.__.__
 /  _____/  ____ ______ |  |__\______   \_______  _____/ ____\__|  |   ____
/   \  ___ /  _ \\____ \|  |  \|     ___/\_  __ \/  _ \   __\|  |  | _/ __ \
\    \_\  (  <_> )  |_> >   Y  \    |     |  | \(  <_> )  |  |  |  |_\  ___/
 \______  /\____/|   __/|___|  /____|     |__|   \____/|__|  |__|____/\___  >
        \/       |__|        \/                                           \/
         -= Server: serving every avatar reliably =-

`
	_, err := fmt.Fprint(output, banner)

	return err
}
