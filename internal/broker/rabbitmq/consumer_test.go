package rabbitmq

import (
	"context"
	"errors"
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/xhrobj/gophprofile/internal/broker"
)

func TestConsumer_Ping(t *testing.T) {
	t.Run("canceled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := (&Consumer{}).Ping(ctx)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Ping() error = %v, want %v", err, context.Canceled)
		}
	})

	t.Run("missing connection", func(t *testing.T) {
		err := (&Consumer{}).Ping(context.Background())
		if err == nil {
			t.Fatal("Ping() error = nil, want closed connection error")
		}
	})
}

func TestForwardDeliveries_StopsWhenContextAlreadyCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	source := make(chan amqp.Delivery, 1)
	target := make(chan broker.Delivery)
	source <- amqp.Delivery{}
	close(source)

	forwardDeliveries(ctx, source, target)

	if _, ok := <-target; ok {
		t.Error("target channel is open, want closed")
	}
}

func TestForwardDeliveries_StopsWhenCanceledWhileForwarding(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	source := make(chan amqp.Delivery)
	target := make(chan broker.Delivery)
	done := make(chan struct{})

	go func() {
		forwardDeliveries(ctx, source, target)
		close(done)
	}()

	source <- amqp.Delivery{}
	cancel()
	<-done

	if _, ok := <-target; ok {
		t.Error("target channel is open, want closed")
	}
}
