package rabbitmq

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

const publisherReconnectCancellationWait = time.Second

func TestPublisher_Ping_CancelsReconnectHandshake(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen fake RabbitMQ: %v", err)
	}
	t.Cleanup(func() {
		_ = listener.Close()
	})

	accepted := make(chan net.Conn, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr == nil {
			accepted <- connection
		}
	}()

	publisher := &Publisher{
		url:      "amqp://guest:guest@" + listener.Addr().String() + "/",
		exchange: "test.exchange",
		queue:    "test.queue",
	}

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- publisher.Ping(ctx)
	}()

	var connection net.Conn
	select {
	case connection = <-accepted:
	case <-time.After(publisherReconnectCancellationWait):
		cancel()
		t.Fatal("fake RabbitMQ did not accept publisher connection")
	}
	t.Cleanup(func() {
		_ = connection.Close()
	})

	cancel()

	select {
	case err = <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Publisher.Ping() error = %v, want context canceled", err)
		}
	case <-time.After(publisherReconnectCancellationWait):
		t.Fatal("Publisher.Ping() did not stop after context cancellation")
	}
}

func TestContextMutex_LockContextStopsOnCancellation(t *testing.T) {
	var mutex contextMutex
	mutex.Lock()
	defer mutex.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- mutex.LockContext(ctx)
	}()

	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("contextMutex.LockContext() error = %v, want context canceled", err)
		}
	case <-time.After(publisherReconnectCancellationWait):
		t.Fatal("contextMutex.LockContext() did not stop after context cancellation")
	}
}

func TestInterruptRabbitMQSetup_CancelsAfterConnectionHandshake(t *testing.T) {
	client, server := net.Pipe()
	t.Cleanup(func() {
		_ = client.Close()
		_ = server.Close()
	})

	ctx, cancel := context.WithCancel(context.Background())
	setupDone := make(chan struct{})
	go interruptRabbitMQSetup(ctx, setupDone, client)
	t.Cleanup(func() {
		close(setupDone)
	})

	readResult := make(chan error, 1)
	go func() {
		buffer := make([]byte, 1)
		_, readErr := client.Read(buffer)
		readResult <- readErr
	}()

	cancel()

	select {
	case err := <-readResult:
		if err == nil {
			t.Fatal("net.Conn.Read() error = nil after context cancellation")
		}
		netErr, ok := err.(net.Error)
		if !ok || !netErr.Timeout() {
			t.Fatalf("net.Conn.Read() error = %v, want timeout after setup cancellation", err)
		}
	case <-time.After(publisherReconnectCancellationWait):
		t.Fatal("RabbitMQ setup I/O did not stop after context cancellation")
	}
}
