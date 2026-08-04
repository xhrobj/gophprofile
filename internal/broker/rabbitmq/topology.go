package rabbitmq

import (
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/xhrobj/gophprofile/internal/event"
)

const exchangeTypeDirect = "direct"

func declareTopology(channel *amqp.Channel, exchange, queue string) error {
	deadLetterExchange := exchange + ".dlx"
	deadLetterQueue := queue + ".dlq"
	deadLetterRoutingKey := queue + ".dead"

	if err := channel.ExchangeDeclare(
		exchange,
		exchangeTypeDirect,
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return fmt.Errorf("declare RabbitMQ exchange %q: %w", exchange, err)
	}

	if err := channel.ExchangeDeclare(
		deadLetterExchange,
		exchangeTypeDirect,
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return fmt.Errorf("declare RabbitMQ dead-letter exchange %q: %w", deadLetterExchange, err)
	}

	if _, err := channel.QueueDeclare(
		deadLetterQueue,
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return fmt.Errorf("declare RabbitMQ dead-letter queue %q: %w", deadLetterQueue, err)
	}

	if err := channel.QueueBind(
		deadLetterQueue,
		deadLetterRoutingKey,
		deadLetterExchange,
		false,
		nil,
	); err != nil {
		return fmt.Errorf("bind RabbitMQ dead-letter queue %q: %w", deadLetterQueue, err)
	}

	if _, err := channel.QueueDeclare(
		queue,
		true,
		false,
		false,
		false,
		amqp.Table{
			"x-dead-letter-exchange":    deadLetterExchange,
			"x-dead-letter-routing-key": deadLetterRoutingKey,
		},
	); err != nil {
		return fmt.Errorf("declare RabbitMQ queue %q: %w", queue, err)
	}

	if err := channel.QueueBind(
		queue,
		event.AvatarUploadedRoutingKey,
		exchange,
		false,
		nil,
	); err != nil {
		return fmt.Errorf(
			"bind RabbitMQ queue %q with routing key %q: %w",
			queue,
			event.AvatarUploadedRoutingKey,
			err,
		)
	}

	return nil
}
