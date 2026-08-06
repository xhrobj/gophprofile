//go:build integration

package worker_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.uber.org/zap"

	"github.com/xhrobj/gophprofile/internal/broker"
	"github.com/xhrobj/gophprofile/internal/broker/rabbitmq"
	"github.com/xhrobj/gophprofile/internal/event"
	"github.com/xhrobj/gophprofile/internal/imageprocessor"
	"github.com/xhrobj/gophprofile/internal/migration"
	"github.com/xhrobj/gophprofile/internal/model"
	"github.com/xhrobj/gophprofile/internal/postgres"
	"github.com/xhrobj/gophprofile/internal/s3"
	"github.com/xhrobj/gophprofile/internal/worker"
)

const (
	workerComponentTestTimeout = 30 * time.Second
	workerTestAvatarID         = "c0decafe-babe-4bed-b042-feeddeadbeef"
	workerTestMessageID        = "c0decafe-babe-4bed-b043-feeddeadbeef"
)

type ackObserverConsumer struct {
	inner broker.Consumer
	acks  chan string
}

type ackObserverDelivery struct {
	broker.Delivery
	acks chan<- string
}

func TestComponent_WorkerAvatarProcessing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), workerComponentTestTimeout)
	t.Cleanup(cancel)

	pool := openWorkerTestDatabase(t, ctx)
	repository := postgres.NewAvatarRepository(pool)

	storage := openWorkerTestStorage(t, ctx)
	original := testPNG(t)
	originalKey := s3.OriginalKey("Alice", workerTestAvatarID, "avatar.png")
	prepareWorkerAvatar(t, ctx, repository, storage, originalKey, original)

	rabbitMQURL := requireWorkerEnv(t, "RABBITMQ_URL")
	exchange, queue := workerTopologyNames()
	t.Cleanup(func() {
		cleanupWorkerTopology(t, rabbitMQURL, exchange, queue)
	})

	consumer, err := rabbitmq.OpenConsumer(rabbitMQURL, exchange, queue)
	if err != nil {
		t.Fatalf("rabbitmq.OpenConsumer() error = %v", err)
	}
	t.Cleanup(func() {
		if err := consumer.Close(); err != nil {
			t.Errorf("Consumer.Close() error = %v", err)
		}
	})

	publisherConnection, publisherChannel := openWorkerTestPublisher(t, rabbitMQURL)
	defer func() {
		if err := publisherChannel.Close(); err != nil {
			t.Errorf("close RabbitMQ test publisher channel: %v", err)
		}
		if err := publisherConnection.Close(); err != nil {
			t.Errorf("close RabbitMQ test publisher connection: %v", err)
		}
	}()

	trackedConsumer := &ackObserverConsumer{
		inner: consumer,
		acks:  make(chan string, 4),
	}
	avatarWorker := worker.New(
		trackedConsumer,
		repository,
		storage,
		imageprocessor.New(),
		zap.NewNop(),
	)
	workerCtx, stopWorker := context.WithCancel(ctx)
	workerDone := make(chan error, 1)
	go func() {
		workerDone <- avatarWorker.Run(workerCtx)
	}()
	t.Cleanup(func() {
		stopWorker()
		select {
		case err := <-workerDone:
			if err != nil {
				t.Errorf("Worker.Run() error = %v", err)
			}
		case <-time.After(workerComponentTestTimeout):
			t.Error("Worker.Run() did not stop during component test cleanup")
		}
	})

	message := event.AvatarUploaded{
		MessageID:     workerTestMessageID,
		AvatarID:      workerTestAvatarID,
		UserID:        "Alice",
		S3Key:         originalKey,
		SchemaVersion: event.AvatarUploadedSchemaVersion,
		CreatedAt:     time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC),
	}

	publishWorkerEvent(t, ctx, publisherChannel, exchange, message)
	processed := waitForCompletedAvatar(t, ctx, repository, workerTestAvatarID)
	assertThumbnail(t, ctx, storage, processed.ThumbnailS3Keys[model.ThumbnailSize100x100], 100)
	assertThumbnail(t, ctx, storage, processed.ThumbnailS3Keys[model.ThumbnailSize300x300], 300)
	firstUpdatedAt := processed.UpdatedAt
	if ackedMessageID := receiveAck(t, ctx, trackedConsumer.acks); ackedMessageID != workerTestMessageID {
		t.Errorf("first ack message_id = %q, want %q", ackedMessageID, workerTestMessageID)
	}

	publishWorkerEvent(t, ctx, publisherChannel, exchange, message)
	if ackedMessageID := receiveAck(t, ctx, trackedConsumer.acks); ackedMessageID != workerTestMessageID {
		t.Errorf("duplicate ack message_id = %q, want %q", ackedMessageID, workerTestMessageID)
	}

	afterDuplicate, err := repository.GetByID(ctx, workerTestAvatarID)
	if err != nil {
		t.Fatalf("GetByID() after duplicate error = %v", err)
	}
	if !afterDuplicate.UpdatedAt.Equal(firstUpdatedAt) {
		t.Errorf("UpdatedAt changed after duplicate: got %v, want %v", afterDuplicate.UpdatedAt, firstUpdatedAt)
	}
	if afterDuplicate.ProcessingStatus != model.ProcessingStatusCompleted {
		t.Errorf(
			"ProcessingStatus after duplicate = %q, want %q",
			afterDuplicate.ProcessingStatus,
			model.ProcessingStatusCompleted,
		)
	}
}

func (c *ackObserverConsumer) Consume(ctx context.Context) (<-chan broker.Delivery, error) {
	source, err := c.inner.Consume(ctx)
	if err != nil {
		return nil, err
	}

	target := make(chan broker.Delivery)
	go func() {
		defer close(target)

		for item := range source {
			select {
			case target <- ackObserverDelivery{Delivery: item, acks: c.acks}:
			case <-ctx.Done():
				return
			}
		}
	}()

	return target, nil
}

func (d ackObserverDelivery) Ack() error {
	if err := d.Delivery.Ack(); err != nil {
		return err
	}

	d.acks <- d.MessageID()

	return nil
}

func openWorkerTestDatabase(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()

	dsn := requireWorkerEnv(t, "DATABASE_DSN")
	adminPool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("create Worker component admin pool: %v", err)
	}
	t.Cleanup(adminPool.Close)
	if err := adminPool.Ping(ctx); err != nil {
		t.Fatalf("ping Worker component PostgreSQL: %v", err)
	}

	databaseName := fmt.Sprintf("gophprofile_worker_test_%d", time.Now().UnixNano())
	quotedDatabaseName := pgx.Identifier{databaseName}.Sanitize()
	if _, err := adminPool.Exec(ctx, "CREATE DATABASE "+quotedDatabaseName); err != nil {
		t.Fatalf("create Worker component database: %v", err)
	}

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse DATABASE_DSN for Worker component: %v", err)
	}
	cfg.ConnConfig.Database = databaseName

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("create Worker component pool: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()

		dropCtx, cancel := context.WithTimeout(context.Background(), workerComponentTestTimeout)
		defer cancel()
		if _, err := adminPool.Exec(dropCtx, "DROP DATABASE "+quotedDatabaseName+" WITH (FORCE)"); err != nil {
			t.Errorf("drop Worker component database: %v", err)
		}
	})

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping Worker component database: %v", err)
	}
	if err := migration.Run(pool); err != nil {
		t.Fatalf("migration.Run() Worker component error = %v", err)
	}

	return pool
}

func openWorkerTestStorage(t *testing.T, ctx context.Context) *s3.Storage {
	t.Helper()

	endpoint := requireWorkerEnv(t, "S3_ENDPOINT")
	accessKey := requireWorkerEnv(t, "S3_ACCESS_KEY")
	secretKey := requireWorkerEnv(t, "S3_SECRET_KEY")
	useSSL, err := strconv.ParseBool(requireWorkerEnv(t, "S3_USE_SSL"))
	if err != nil {
		t.Fatalf("parse S3_USE_SSL: %v", err)
	}

	bucket := fmt.Sprintf("gophprofile-worker-test-%d", time.Now().UnixNano())
	cleanupClient, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		t.Fatalf("create Worker component MinIO cleanup client: %v", err)
	}
	t.Cleanup(func() {
		cleanupWorkerBucket(t, cleanupClient, bucket)
	})

	storage, err := s3.Open(ctx, endpoint, accessKey, secretKey, bucket, useSSL)
	if err != nil {
		t.Fatalf("s3.Open() error = %v", err)
	}

	return storage
}

func cleanupWorkerBucket(t *testing.T, client *minio.Client, bucket string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), workerComponentTestTimeout)
	defer cancel()

	for object := range client.ListObjects(ctx, bucket, minio.ListObjectsOptions{Recursive: true}) {
		if object.Err != nil {
			t.Errorf("list Worker component MinIO objects: %v", object.Err)
			continue
		}
		if err := client.RemoveObject(ctx, bucket, object.Key, minio.RemoveObjectOptions{}); err != nil {
			t.Errorf("remove Worker component MinIO object %q: %v", object.Key, err)
		}
	}
	if err := client.RemoveBucket(ctx, bucket); err != nil {
		t.Errorf("remove Worker component MinIO bucket %q: %v", bucket, err)
	}
}

func prepareWorkerAvatar(
	t *testing.T,
	ctx context.Context,
	repository *postgres.AvatarRepository,
	storage *s3.Storage,
	originalKey string,
	original []byte,
) {
	t.Helper()

	avatar := model.Avatar{
		ID:        workerTestAvatarID,
		UserID:    "Alice",
		FileName:  "avatar.png",
		MIMEType:  "image/png",
		SizeBytes: int64(len(original)),
		Width:     6,
		Height:    4,
		S3Key:     originalKey,
	}
	if _, err := repository.Create(ctx, avatar); err != nil {
		t.Fatalf("Create() Worker component avatar error = %v", err)
	}
	if err := storage.Put(ctx, originalKey, bytes.NewReader(original), int64(len(original)), "image/png"); err != nil {
		t.Fatalf("Put() Worker component original error = %v", err)
	}
	if err := repository.UpdateUploadStatus(ctx, workerTestAvatarID, model.UploadStatusCompleted); err != nil {
		t.Fatalf("UpdateUploadStatus() Worker component avatar error = %v", err)
	}
}

func testPNG(t *testing.T) []byte {
	t.Helper()

	imageData := image.NewRGBA(image.Rect(0, 0, 6, 4))
	for y := 0; y < imageData.Bounds().Dy(); y++ {
		for x := 0; x < imageData.Bounds().Dx(); x++ {
			imageData.Set(x, y, color.RGBA{
				R: uint8(20 + x*20),
				G: uint8(40 + y*30),
				B: 160,
				A: 255,
			})
		}
	}

	var buffer bytes.Buffer
	if err := png.Encode(&buffer, imageData); err != nil {
		t.Fatalf("encode Worker component PNG: %v", err)
	}

	return buffer.Bytes()
}

func openWorkerTestPublisher(t *testing.T, url string) (*amqp.Connection, *amqp.Channel) {
	t.Helper()

	connection, err := amqp.Dial(url)
	if err != nil {
		t.Fatalf("dial RabbitMQ test publisher: %v", err)
	}

	channel, err := connection.Channel()
	if err != nil {
		_ = connection.Close()
		t.Fatalf("open RabbitMQ test publisher channel: %v", err)
	}
	if err := channel.Confirm(false); err != nil {
		_ = channel.Close()
		_ = connection.Close()
		t.Fatalf("enable RabbitMQ test publisher confirms: %v", err)
	}

	return connection, channel
}

func publishWorkerEvent(
	t *testing.T,
	ctx context.Context,
	channel *amqp.Channel,
	exchange string,
	message event.AvatarUploaded,
) {
	t.Helper()

	body, err := json.Marshal(message)
	if err != nil {
		t.Fatalf("marshal Worker component event: %v", err)
	}

	confirmation, err := channel.PublishWithDeferredConfirmWithContext(
		ctx,
		exchange,
		event.AvatarUploadedRoutingKey,
		false,
		false,
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			MessageId:    message.MessageID,
			Timestamp:    message.CreatedAt,
			Type:         event.AvatarUploadedRoutingKey,
			Body:         body,
		},
	)
	if err != nil {
		t.Fatalf("publish Worker component event: %v", err)
	}
	if confirmation == nil {
		t.Fatal("publish Worker component event: confirmation is nil")
	}

	acknowledged, err := confirmation.WaitContext(ctx)
	if err != nil {
		t.Fatalf("wait for Worker component publisher confirm: %v", err)
	}
	if !acknowledged {
		t.Fatal("Worker component event was negatively acknowledged")
	}
}

func receiveAck(t *testing.T, ctx context.Context, acks <-chan string) string {
	t.Helper()

	select {
	case messageID := <-acks:
		return messageID
	case <-ctx.Done():
		t.Fatalf("wait for Worker component ack: %v", ctx.Err())

		return ""
	}
}

func waitForCompletedAvatar(
	t *testing.T,
	ctx context.Context,
	repository *postgres.AvatarRepository,
	avatarID string,
) model.Avatar {
	t.Helper()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		avatar, err := repository.GetByID(ctx, avatarID)
		if err != nil {
			t.Fatalf("GetByID() while waiting for Worker: %v", err)
		}
		if avatar.ProcessingStatus == model.ProcessingStatusCompleted {
			return avatar
		}

		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatalf("wait for completed Worker avatar: %v", ctx.Err())

			return model.Avatar{}
		}
	}
}

func assertThumbnail(
	t *testing.T,
	ctx context.Context,
	storage *s3.Storage,
	key string,
	wantPixels int,
) {
	t.Helper()

	if key == "" {
		t.Fatalf("thumbnail key for %dx%d is empty", wantPixels, wantPixels)
	}

	reader, err := storage.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get() thumbnail %dx%d error = %v", wantPixels, wantPixels, err)
	}
	defer func() {
		if err := reader.Close(); err != nil {
			t.Errorf("close thumbnail %dx%d: %v", wantPixels, wantPixels, err)
		}
	}()

	config, err := jpeg.DecodeConfig(reader)
	if err != nil {
		t.Fatalf("decode thumbnail %dx%d as JPEG: %v", wantPixels, wantPixels, err)
	}
	if config.Width != wantPixels || config.Height != wantPixels {
		t.Errorf(
			"thumbnail dimensions = %dx%d, want %dx%d",
			config.Width,
			config.Height,
			wantPixels,
			wantPixels,
		)
	}
}

func workerTopologyNames() (string, string) {
	base := "gophprofile.worker.component." + strconv.FormatInt(time.Now().UnixNano(), 10)

	return base + ".exchange", base + ".queue"
}

func cleanupWorkerTopology(t *testing.T, url, exchange, queue string) {
	t.Helper()

	connection, err := amqp.Dial(url)
	if err != nil {
		t.Errorf("dial RabbitMQ for Worker topology cleanup: %v", err)
		return
	}
	defer func() {
		if err := connection.Close(); err != nil {
			t.Errorf("close RabbitMQ Worker cleanup connection: %v", err)
		}
	}()

	channel, err := connection.Channel()
	if err != nil {
		t.Errorf("open RabbitMQ Worker cleanup channel: %v", err)
		return
	}
	defer func() {
		if err := channel.Close(); err != nil {
			t.Errorf("close RabbitMQ Worker cleanup channel: %v", err)
		}
	}()

	if _, err := channel.QueueDelete(queue, false, false, false); err != nil {
		t.Errorf("delete RabbitMQ Worker test queue %q: %v", queue, err)
	}
	if _, err := channel.QueueDelete(queue+".dlq", false, false, false); err != nil {
		t.Errorf("delete RabbitMQ Worker test DLQ %q: %v", queue+".dlq", err)
	}
	if err := channel.ExchangeDelete(exchange, false, false); err != nil {
		t.Errorf("delete RabbitMQ Worker test exchange %q: %v", exchange, err)
	}
	if err := channel.ExchangeDelete(exchange+".dlx", false, false); err != nil {
		t.Errorf("delete RabbitMQ Worker test DLX %q: %v", exchange+".dlx", err)
	}
}

func requireWorkerEnv(t *testing.T, name string) string {
	t.Helper()

	value := os.Getenv(name)
	if value == "" {
		t.Fatalf("%s is not set", name)
	}

	return value
}
