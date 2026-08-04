//go:build integration

package rabbitmq_test

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/xhrobj/gophprofile/internal/broker"
	"github.com/xhrobj/gophprofile/internal/broker/rabbitmq"
	"github.com/xhrobj/gophprofile/internal/event"
	"github.com/xhrobj/gophprofile/internal/model"
)

const rabbitMQIntegrationTestTimeout = 30 * time.Second

func TestIntegration_RabbitMQPublisherConsumer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), rabbitMQIntegrationTestTimeout)
	t.Cleanup(cancel)

	url := requireEnv(t, "RABBITMQ_URL")
	exchange, queue := uniqueTopologyNames()
	t.Cleanup(func() {
		cleanupTopology(t, url, exchange, queue)
	})

	consumer, err := rabbitmq.OpenConsumer(url, exchange, queue)
	if err != nil {
		t.Fatalf("OpenConsumer() error = %v", err)
	}
	t.Cleanup(func() {
		if err := consumer.Close(); err != nil {
			t.Errorf("Consumer.Close() error = %v", err)
		}
	})

	publisher, err := rabbitmq.OpenPublisher(url, exchange, queue)
	if err != nil {
		t.Fatalf("OpenPublisher() error = %v", err)
	}
	t.Cleanup(func() {
		if err := publisher.Close(); err != nil {
			t.Errorf("Publisher.Close() error = %v", err)
		}
	})

	deliveries, err := consumer.Consume(ctx)
	if err != nil {
		t.Fatalf("Consume() error = %v", err)
	}

	avatar := model.Avatar{
		ID:     "c0decafe-babe-4bed-b042-feeddeadbeef",
		UserID: "Alice",
		S3Key:  "originals/Alice/c0decafe-babe-4bed-b042-feeddeadbeef/avatar.webp",
	}
	if err := publisher.PublishAvatarUploaded(ctx, avatar); err != nil {
		t.Fatalf("PublishAvatarUploaded() error = %v", err)
	}

	item := receiveDelivery(t, ctx, deliveries)
	if item.RoutingKey() != event.AvatarUploadedRoutingKey {
		t.Errorf("RoutingKey() = %q, want %q", item.RoutingKey(), event.AvatarUploadedRoutingKey)
	}
	if err := uuid.Validate(item.MessageID()); err != nil {
		t.Errorf("MessageID() = %q, want valid UUID: %v", item.MessageID(), err)
	}

	var message event.AvatarUploaded
	if err := json.Unmarshal(item.Body(), &message); err != nil {
		t.Fatalf("decode delivery body: %v", err)
	}
	if message.MessageID != item.MessageID() {
		t.Errorf("body message_id = %q, delivery message_id = %q", message.MessageID, item.MessageID())
	}
	if message.AvatarID != avatar.ID {
		t.Errorf("body avatar_id = %q, want %q", message.AvatarID, avatar.ID)
	}
	if message.UserID != avatar.UserID {
		t.Errorf("body user_id = %q, want %q", message.UserID, avatar.UserID)
	}
	if message.S3Key != avatar.S3Key {
		t.Errorf("body s3_key = %q, want %q", message.S3Key, avatar.S3Key)
	}
	if message.SchemaVersion != event.AvatarUploadedSchemaVersion {
		t.Errorf(
			"body schema_version = %d, want %d",
			message.SchemaVersion,
			event.AvatarUploadedSchemaVersion,
		)
	}
	if message.CreatedAt.IsZero() {
		t.Error("body created_at is zero")
	}

	if err := item.Nack(false); err != nil {
		t.Fatalf("Nack(false) error = %v", err)
	}

	deadLetter := receiveDeadLetter(t, ctx, url, queue+".dlq")
	defer func() {
		if err := deadLetter.Ack(false); err != nil {
			t.Errorf("ack dead-letter delivery: %v", err)
		}
	}()

	if deadLetter.MessageId != message.MessageID {
		t.Errorf("dead-letter message_id = %q, want %q", deadLetter.MessageId, message.MessageID)
	}
	if deadLetter.RoutingKey != queue+".dead" {
		t.Errorf("dead-letter routing key = %q, want %q", deadLetter.RoutingKey, queue+".dead")
	}
	if string(deadLetter.Body) != string(item.Body()) {
		t.Error("dead-letter body differs from original delivery")
	}
}

func receiveDelivery(t *testing.T, ctx context.Context, deliveries <-chan broker.Delivery) broker.Delivery {
	t.Helper()

	select {
	case item, ok := <-deliveries:
		if !ok {
			t.Fatal("RabbitMQ deliveries channel closed before message arrived")
		}

		return item
	case <-ctx.Done():
		t.Fatalf("wait for RabbitMQ delivery: %v", ctx.Err())

		return nil
	}
}

func receiveDeadLetter(
	t *testing.T,
	ctx context.Context,
	url string,
	queue string,
) amqp.Delivery {
	t.Helper()

	connection, err := amqp.Dial(url)
	if err != nil {
		t.Fatalf("dial RabbitMQ for DLQ assertion: %v", err)
	}
	t.Cleanup(func() {
		if err := connection.Close(); err != nil {
			t.Errorf("close RabbitMQ DLQ connection: %v", err)
		}
	})

	channel, err := connection.Channel()
	if err != nil {
		t.Fatalf("open RabbitMQ DLQ channel: %v", err)
	}
	t.Cleanup(func() {
		if err := channel.Close(); err != nil {
			t.Errorf("close RabbitMQ DLQ channel: %v", err)
		}
	})

	deliveries, err := channel.ConsumeWithContext(
		ctx,
		queue,
		"",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		t.Fatalf("consume RabbitMQ DLQ %q: %v", queue, err)
	}

	select {
	case item, ok := <-deliveries:
		if !ok {
			t.Fatal("RabbitMQ DLQ deliveries channel closed before message arrived")
		}

		return item
	case <-ctx.Done():
		t.Fatalf("wait for RabbitMQ DLQ delivery: %v", ctx.Err())

		return amqp.Delivery{}
	}
}

func uniqueTopologyNames() (string, string) {
	base := "gophprofile.integration." + strconv.FormatInt(time.Now().UnixNano(), 10)

	return base + ".exchange", base + ".queue"
}

func cleanupTopology(t *testing.T, url, exchange, queue string) {
	t.Helper()

	connection, err := amqp.Dial(url)
	if err != nil {
		t.Errorf("dial RabbitMQ for topology cleanup: %v", err)
		return
	}
	defer func() {
		if err := connection.Close(); err != nil {
			t.Errorf("close RabbitMQ cleanup connection: %v", err)
		}
	}()

	channel, err := connection.Channel()
	if err != nil {
		t.Errorf("open RabbitMQ cleanup channel: %v", err)
		return
	}
	defer func() {
		if err := channel.Close(); err != nil {
			t.Errorf("close RabbitMQ cleanup channel: %v", err)
		}
	}()

	if _, err := channel.QueueDelete(queue, false, false, false); err != nil {
		t.Errorf("delete RabbitMQ test queue %q: %v", queue, err)
	}
	if _, err := channel.QueueDelete(queue+".dlq", false, false, false); err != nil {
		t.Errorf("delete RabbitMQ test DLQ %q: %v", queue+".dlq", err)
	}
	if err := channel.ExchangeDelete(exchange, false, false); err != nil {
		t.Errorf("delete RabbitMQ test exchange %q: %v", exchange, err)
	}
	if err := channel.ExchangeDelete(exchange+".dlx", false, false); err != nil {
		t.Errorf("delete RabbitMQ test DLX %q: %v", exchange+".dlx", err)
	}
}

func requireEnv(t *testing.T, name string) string {
	t.Helper()

	value := os.Getenv(name)
	if value == "" {
		t.Fatalf("%s is not set", name)
	}

	return value
}
