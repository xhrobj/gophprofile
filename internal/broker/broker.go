// Package broker содержит broker-neutral контракты доставки сообщений.
package broker

import "context"

// Delivery представляет одно сообщение, полученное из брокера.
type Delivery interface {
	Body() []byte
	MessageID() string
	RoutingKey() string
	Ack() error
	Nack(requeue bool) error
}

// Consumer получает сообщения из брокера с ручным подтверждением обработки.
type Consumer interface {
	Consume(ctx context.Context) (<-chan Delivery, error)
}
