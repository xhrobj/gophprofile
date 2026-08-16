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

	"github.com/xhrobj/gophprofile/internal/broker/rabbitmq"
	"github.com/xhrobj/gophprofile/internal/config"
	"github.com/xhrobj/gophprofile/internal/health"
	"github.com/xhrobj/gophprofile/internal/imageprocessor"
	"github.com/xhrobj/gophprofile/internal/logger"
	"github.com/xhrobj/gophprofile/internal/observability"
	"github.com/xhrobj/gophprofile/internal/postgres"
	"github.com/xhrobj/gophprofile/internal/s3"
	httpserver "github.com/xhrobj/gophprofile/internal/server"
	"github.com/xhrobj/gophprofile/internal/worker"
)

const (
	serviceName              = "worker"
	metricsReadHeaderTimeout = 5 * time.Second
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
	cfg, err := config.LoadWorker()
	if err != nil {
		return fmt.Errorf("load worker config: %w", err)
	}

	lg, err := logger.New(serviceName, cfg.LogLevel)
	if err != nil {
		return fmt.Errorf("create worker logger: %w", err)
	}

	tracing, err := observability.NewTracing(ctx, serviceName, cfg.OTLPEndpoint, cfg.TracingEnabled)
	if err != nil {
		return fmt.Errorf("create worker tracing: %w", err)
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

	consumer, err := rabbitmq.OpenConsumer(cfg.RabbitMQURL, cfg.RabbitMQExchange, cfg.RabbitMQQueue)
	if err != nil {
		return fmt.Errorf("open RabbitMQ consumer: %w", err)
	}
	defer func() {
		if closeErr := consumer.Close(); closeErr != nil {
			lg.WarnContext(ctx, "failed to close RabbitMQ consumer", slog.Any("error", closeErr))
		}
	}()

	metrics := observability.NewWorkerMetrics()
	metrics.RegisterPostgreSQLPool(pool)
	metricsListener, err := net.Listen("tcp", cfg.MetricsAddress)
	if err != nil {
		return fmt.Errorf("listen for Worker metrics on %s: %w", cfg.MetricsAddress, err)
	}

	healthChecker := health.NewChecker(pool, storage, consumer)
	metricsMux := http.NewServeMux()
	metricsMux.Handle("/live", health.NewLivenessHandler())
	metricsMux.Handle("/health", health.NewReadinessHandler(healthChecker))
	metricsMux.Handle("/metrics", metrics.Handler())
	metricsServer := &http.Server{
		Addr:              cfg.MetricsAddress,
		Handler:           metricsMux,
		ReadHeaderTimeout: metricsReadHeaderTimeout,
		ErrorLog: slog.NewLogLogger(
			lg.With(slog.String("logger", "metrics/net/http")).Handler(),
			slog.LevelError,
		),
	}

	avatarRepository := postgres.NewResilientAvatarRepository(postgres.NewAvatarRepository(pool), lg)
	avatarWorker := worker.New(
		consumer,
		avatarRepository,
		storage,
		imageprocessor.New(),
		metrics,
		lg,
	)

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	metricsErrors := make(chan error, 1)
	go func() {
		metricsErrors <- httpserver.Run(
			runCtx,
			metricsListener,
			metricsServer,
			cfg.ShutdownTimeout,
			lg.With(slog.String("component", "metrics")),
		)
		cancel()
	}()

	workerErr := avatarWorker.Run(runCtx)
	cancel()
	metricsErr := <-metricsErrors

	if workerErr != nil {
		return workerErr
	}
	if metricsErr != nil {
		return fmt.Errorf("run Worker metrics server: %w", metricsErr)
	}

	return nil
}

func printBanner(output io.Writer) error {
	const banner = `
  ________              .__   __________                _____.__.__
 /  _____/  ____ ______ |  |__\______   \_______  _____/ ____\__|  |   ____
/   \  ___ /  _ \\____ \|  |  \|     ___/\_  __ \/  _ \   __\|  |  | _/ __ \
\    \_\  (  <_> )  |_> >   Y  \    |     |  | \(  <_> )  |  |  |  |_\  ___/
 \______  /\____/|   __/|___|  /____|     |__|   \____/|__|  |__|____/\___  >
        \/       |__|        \/                                           \/
         -= Worker: processing every avatar with care =-

`
	_, err := fmt.Fprint(output, banner)

	return err
}
