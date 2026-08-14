//go:build integration

package rabbitmq

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/xhrobj/gophprofile/internal/model"
)

const publisherReconnectIntegrationTestTimeout = 30 * time.Second

func TestIntegration_PublisherReconnects(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), publisherReconnectIntegrationTestTimeout)
	t.Cleanup(cancel)

	url := os.Getenv("RABBITMQ_URL")
	if url == "" {
		t.Fatal("RABBITMQ_URL is not set")
	}

	base := "gophprofile.integration.reconnect." + strconv.FormatInt(time.Now().UnixNano(), 10)
	exchange := base + ".exchange"
	queue := base + ".queue"

	publisher, err := OpenPublisher(url, exchange, queue)
	if err != nil {
		t.Fatalf("OpenPublisher() error = %v", err)
	}
	t.Cleanup(func() {
		cleanupReconnectTopology(t, publisher, exchange, queue)
		if err := publisher.Close(); err != nil {
			t.Errorf("Publisher.Close() error = %v", err)
		}
	})

	firstConnection := publisher.connection
	if err := firstConnection.Close(); err != nil {
		t.Fatalf("close first RabbitMQ connection: %v", err)
	}

	if err := publisher.Ping(ctx); err != nil {
		t.Fatalf("Publisher.Ping() after connection close error = %v", err)
	}
	if publisher.connection == firstConnection {
		t.Fatal("Publisher.Ping() did not replace closed RabbitMQ connection")
	}

	secondConnection := publisher.connection
	if err := secondConnection.Close(); err != nil {
		t.Fatalf("close second RabbitMQ connection: %v", err)
	}

	avatar := model.Avatar{
		ID:     "c0decafe-babe-4bed-b042-feeddeadbeef",
		UserID: "Alice",
		S3Key:  "originals/Alice/c0decafe-babe-4bed-b042-feeddeadbeef/avatar.webp",
	}
	if err := publisher.PublishAvatarUploaded(ctx, avatar); err != nil {
		t.Fatalf("PublishAvatarUploaded() after connection close error = %v", err)
	}
	if publisher.connection == secondConnection {
		t.Fatal("PublishAvatarUploaded() did not replace closed RabbitMQ connection")
	}
}

func cleanupReconnectTopology(t *testing.T, publisher *Publisher, exchange, queue string) {
	t.Helper()

	publisher.mu.Lock()
	defer publisher.mu.Unlock()

	if err := publisher.ensureConnectedLocked(); err != nil {
		t.Errorf("reconnect RabbitMQ for topology cleanup: %v", err)
		return
	}

	if _, err := publisher.channel.QueueDelete(queue, false, false, false); err != nil {
		t.Errorf("delete RabbitMQ test queue %q: %v", queue, err)
	}
	if _, err := publisher.channel.QueueDelete(queue+".dlq", false, false, false); err != nil {
		t.Errorf("delete RabbitMQ test DLQ %q: %v", queue+".dlq", err)
	}
	if err := publisher.channel.ExchangeDelete(exchange, false, false); err != nil {
		t.Errorf("delete RabbitMQ test exchange %q: %v", exchange, err)
	}
	if err := publisher.channel.ExchangeDelete(exchange+".dlx", false, false); err != nil {
		t.Errorf("delete RabbitMQ test DLX %q: %v", exchange+".dlx", err)
	}
}
