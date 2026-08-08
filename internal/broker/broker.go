// Package broker содержит общие контракты для работы с брокером сообщений.
package broker

import "context"

// Delivery представляет одно сообщение, полученное из брокера.
type Delivery interface {
	// Body возвращает тело сообщения.
	Body() []byte

	// Headers возвращает строковые заголовки сообщения.
	// Заголовки могут содержать метаданные для сквозной трассировки.
	Headers() map[string]string

	// MessageID возвращает идентификатор сообщения.
	MessageID() string

	// RoutingKey возвращает routing key сообщения.
	RoutingKey() string

	// Redelivered сообщает, было ли сообщение доставлено повторно.
	Redelivered() bool

	// Ack подтверждает успешную обработку сообщения.
	Ack() error

	// Nack отклоняет сообщение и при requeue возвращает его в очередь.
	Nack(requeue bool) error
}

// Consumer получает сообщения из брокера с ручным подтверждением обработки.
type Consumer interface {
	// Consume начинает получение сообщений до отмены ctx.
	Consume(ctx context.Context) (<-chan Delivery, error)
}
