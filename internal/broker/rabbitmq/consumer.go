package rabbitmq

import (
	"context"
	"errors"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/xhrobj/gophprofile/internal/broker"
)

const consumerPrefetchCount = 1

// Consumer получает сообщения из RabbitMQ с manual ack.
type Consumer struct {
	connection *amqp.Connection
	channel    *amqp.Channel
	queue      string
}

type delivery struct {
	value amqp.Delivery
}

// OpenConsumer подключается к RabbitMQ, идемпотентно объявляет topology и настраивает prefetch.
func OpenConsumer(url, exchange, queue string) (*Consumer, error) {
	connection, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("dial RabbitMQ: %w", err)
	}

	channel, err := connection.Channel()
	if err != nil {
		_ = connection.Close()

		return nil, fmt.Errorf("open RabbitMQ channel: %w", err)
	}

	if err := declareTopology(channel, exchange, queue); err != nil {
		_ = channel.Close()
		_ = connection.Close()

		return nil, err
	}

	if err := channel.Qos(consumerPrefetchCount, 0, false); err != nil {
		_ = channel.Close()
		_ = connection.Close()

		return nil, fmt.Errorf("configure RabbitMQ consumer QoS: %w", err)
	}

	return &Consumer{
		connection: connection,
		channel:    channel,
		queue:      queue,
	}, nil
}

// Consume начинает получать сообщения до отмены контекста или закрытия RabbitMQ channel.
func (c *Consumer) Consume(ctx context.Context) (<-chan broker.Delivery, error) {
	deliveries, err := c.channel.ConsumeWithContext(
		ctx,
		c.queue,
		"",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("consume RabbitMQ queue %q: %w", c.queue, err)
	}

	result := make(chan broker.Delivery)
	go forwardDeliveries(ctx, deliveries, result)

	return result, nil
}

// Close закрывает RabbitMQ channel и connection Consumer.
func (c *Consumer) Close() error {
	var resultErr error
	if c.channel != nil {
		if err := c.channel.Close(); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("close RabbitMQ channel: %w", err))
		}
	}
	if c.connection != nil {
		if err := c.connection.Close(); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("close RabbitMQ connection: %w", err))
		}
	}

	return resultErr
}

func (d delivery) Body() []byte {
	return d.value.Body
}

func (d delivery) Headers() map[string]string {
	if len(d.value.Headers) == 0 {
		return nil
	}

	headers := make(map[string]string, len(d.value.Headers))
	for key, value := range d.value.Headers {
		if text, ok := value.(string); ok {
			headers[key] = text
		}
	}

	return headers
}

func (d delivery) MessageID() string {
	return d.value.MessageId
}

func (d delivery) RoutingKey() string {
	return d.value.RoutingKey
}

func (d delivery) Redelivered() bool {
	return d.value.Redelivered
}

func (d delivery) Ack() error {
	return d.value.Ack(false)
}

func (d delivery) Nack(requeue bool) error {
	return d.value.Nack(false, requeue)
}

func forwardDeliveries(ctx context.Context, source <-chan amqp.Delivery, target chan<- broker.Delivery) {
	defer close(target)

	for item := range source {
		if ctx.Err() != nil {
			return
		}

		select {
		case target <- delivery{value: item}:
		case <-ctx.Done():
			return
		}
	}
}
