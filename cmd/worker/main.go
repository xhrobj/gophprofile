package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"

	"github.com/xhrobj/gophprofile/internal/broker/rabbitmq"
	"github.com/xhrobj/gophprofile/internal/config"
	"github.com/xhrobj/gophprofile/internal/imageprocessor"
	"github.com/xhrobj/gophprofile/internal/logger"
	"github.com/xhrobj/gophprofile/internal/postgres"
	"github.com/xhrobj/gophprofile/internal/s3"
	"github.com/xhrobj/gophprofile/internal/worker"
)

const serviceName = "worker"

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
	defer func() {
		_ = lg.Sync()
	}()

	pool, err := postgres.Open(ctx, cfg.DatabaseDSN)
	if err != nil {
		return fmt.Errorf("open PostgreSQL: %w", err)
	}
	defer pool.Close()

	storage, err := s3.Open(
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

	consumer, err := rabbitmq.OpenConsumer(cfg.RabbitMQURL, cfg.RabbitMQExchange, cfg.RabbitMQQueue)
	if err != nil {
		return fmt.Errorf("open RabbitMQ consumer: %w", err)
	}
	defer func() {
		if closeErr := consumer.Close(); closeErr != nil {
			lg.Warn("failed to close RabbitMQ consumer", zap.Error(closeErr))
		}
	}()

	avatarWorker := worker.New(
		consumer,
		postgres.NewAvatarRepository(pool),
		storage,
		imageprocessor.New(),
		lg,
	)

	return avatarWorker.Run(ctx)
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
