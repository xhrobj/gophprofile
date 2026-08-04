package rabbitmq

import (
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/xhrobj/gophprofile/internal/event"
)

const exchangeTypeDirect = "direct"

type topology struct {
	exchange             string
	queue                string
	deadLetterExchange   string
	deadLetterQueue      string
	deadLetterRoutingKey string
}

func declareTopology(channel *amqp.Channel, exchange, queue string) error {
	topology := newTopology(exchange, queue)

	if err := channel.ExchangeDeclare(
		topology.exchange,
		exchangeTypeDirect,
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return fmt.Errorf("declare RabbitMQ exchange %q: %w", topology.exchange, err)
	}

	if err := channel.ExchangeDeclare(
		topology.deadLetterExchange,
		exchangeTypeDirect,
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return fmt.Errorf("declare RabbitMQ dead-letter exchange %q: %w", topology.deadLetterExchange, err)
	}

	if _, err := channel.QueueDeclare(
		topology.deadLetterQueue,
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return fmt.Errorf("declare RabbitMQ dead-letter queue %q: %w", topology.deadLetterQueue, err)
	}

	if err := channel.QueueBind(
		topology.deadLetterQueue,
		topology.deadLetterRoutingKey,
		topology.deadLetterExchange,
		false,
		nil,
	); err != nil {
		return fmt.Errorf("bind RabbitMQ dead-letter queue %q: %w", topology.deadLetterQueue, err)
	}

	if _, err := channel.QueueDeclare(
		topology.queue,
		true,
		false,
		false,
		false,
		amqp.Table{
			"x-dead-letter-exchange":    topology.deadLetterExchange,
			"x-dead-letter-routing-key": topology.deadLetterRoutingKey,
		},
	); err != nil {
		return fmt.Errorf("declare RabbitMQ queue %q: %w", topology.queue, err)
	}

	for _, routingKey := range []string{
		event.AvatarUploadedRoutingKey,
		event.AvatarDeletedRoutingKey,
	} {
		if err := channel.QueueBind(
			topology.queue,
			routingKey,
			topology.exchange,
			false,
			nil,
		); err != nil {
			return fmt.Errorf("bind RabbitMQ queue %q with routing key %q: %w", topology.queue, routingKey, err)
		}
	}

	return nil
}

func newTopology(exchange, queue string) topology {
	return topology{
		exchange:             exchange,
		queue:                queue,
		deadLetterExchange:   exchange + ".dlx",
		deadLetterQueue:      queue + ".dlq",
		deadLetterRoutingKey: queue + ".dead",
	}
}
