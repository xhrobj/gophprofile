package rabbitmq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/xhrobj/gophprofile/internal/event"
	"github.com/xhrobj/gophprofile/internal/model"
)

const (
	publisherConfirmTimeout       = 5 * time.Second
	rabbitMQDefaultConnectTimeout = 30 * time.Second
	rabbitMQInstrumentationName   = "github.com/xhrobj/gophprofile/internal/broker/rabbitmq"
)

type amqpTableCarrier amqp.Table

type contextMutex struct {
	once  sync.Once
	token chan struct{}
}

func (m *contextMutex) Lock() {
	_ = m.LockContext(context.Background())
}

func (m *contextMutex) LockContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	m.once.Do(func() {
		m.token = make(chan struct{}, 1)
		m.token <- struct{}{}
	})

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-m.token:
		if err := ctx.Err(); err != nil {
			m.token <- struct{}{}

			return err
		}

		return nil
	}
}

func (m *contextMutex) Unlock() {
	m.once.Do(func() {
		m.token = make(chan struct{}, 1)
	})

	select {
	case m.token <- struct{}{}:
	default:
		panic("unlock of unlocked contextMutex")
	}
}

// Publisher публикует события аватаров в RabbitMQ с publisher confirms.
type Publisher struct {
	url        string
	connection *amqp.Connection
	channel    *amqp.Channel
	exchange   string
	queue      string
	mu         contextMutex
	closed     bool
}

// OpenPublisher подключается к RabbitMQ, идемпотентно объявляет topology и включает publisher confirms.
func OpenPublisher(url, exchange, queue string) (*Publisher, error) {
	connection, channel, err := openPublisherConnection(context.Background(), url, exchange, queue)
	if err != nil {
		return nil, err
	}

	return &Publisher{
		url:        url,
		connection: connection,
		channel:    channel,
		exchange:   exchange,
		queue:      queue,
	}, nil
}

// PublishAvatarUploaded публикует событие о готовом оригинале и ждёт подтверждения RabbitMQ.
func (p *Publisher) PublishAvatarUploaded(ctx context.Context, avatar model.Avatar) error {
	createdAt := time.Now().UTC()
	message := event.AvatarUploaded{
		MessageID:     uuid.NewString(),
		AvatarID:      avatar.ID,
		UserID:        avatar.UserID,
		S3Key:         avatar.S3Key,
		SchemaVersion: event.AvatarUploadedSchemaVersion,
		CreatedAt:     createdAt,
	}

	return p.publish(ctx, event.AvatarUploadedRoutingKey, message.MessageID, message.CreatedAt, message)
}

// PublishAvatarDeleted публикует событие об объектах удалённой аватарки и ждёт подтверждения RabbitMQ.
func (p *Publisher) PublishAvatarDeleted(ctx context.Context, avatar model.Avatar) error {
	createdAt := time.Now().UTC()
	message := event.AvatarDeleted{
		MessageID:     uuid.NewString(),
		AvatarID:      avatar.ID,
		S3Keys:        avatarS3Keys(avatar),
		SchemaVersion: event.AvatarDeletedSchemaVersion,
		CreatedAt:     createdAt,
	}

	return p.publish(ctx, event.AvatarDeletedRoutingKey, message.MessageID, message.CreatedAt, message)
}

// Ping проверяет доступность RabbitMQ и восстанавливает publisher connection после разрыва.
func (p *Publisher) Ping(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := p.mu.LockContext(ctx); err != nil {
		return err
	}
	defer p.mu.Unlock()

	if err := p.ensureConnectedLocked(ctx); err != nil {
		return err
	}

	return ctx.Err()
}

// Close закрывает RabbitMQ channel и connection Publisher.
func (p *Publisher) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.closed = true

	return p.closeConnectionLocked()
}

func (c amqpTableCarrier) Get(key string) string {
	value, ok := c[key].(string)
	if !ok {
		return ""
	}

	return value
}

func (c amqpTableCarrier) Set(key, value string) {
	c[key] = value
}

func (c amqpTableCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for key := range c {
		keys = append(keys, key)
	}

	return keys
}

func (p *Publisher) publish(
	ctx context.Context,
	routingKey string,
	messageID string,
	createdAt time.Time,
	message any,
) (resultErr error) {
	body, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("marshal %s event: %w", routingKey, err)
	}

	destination := p.exchange + ":" + routingKey
	ctx, span := otel.Tracer(rabbitMQInstrumentationName).Start(
		ctx,
		"send "+destination,
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("messaging.system", "rabbitmq"),
			attribute.String("messaging.destination.name", destination),
			attribute.String("messaging.operation.name", "send"),
			attribute.String("messaging.operation.type", "send"),
			attribute.String("messaging.rabbitmq.destination.routing_key", routingKey),
			attribute.String("messaging.message.id", messageID),
		),
	)
	defer func() {
		if resultErr != nil {
			span.RecordError(resultErr)
			span.SetStatus(codes.Error, resultErr.Error())
		}

		span.End()
	}()

	headers := amqp.Table{}
	otel.GetTextMapPropagator().Inject(ctx, amqpTableCarrier(headers))

	if err := p.mu.LockContext(ctx); err != nil {
		return err
	}
	defer p.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}
	if err := p.ensureConnectedLocked(ctx); err != nil {
		return fmt.Errorf("connect RabbitMQ before publishing %s event: %w", routingKey, err)
	}

	confirmCtx, cancel := context.WithTimeout(ctx, publisherConfirmTimeout)
	defer cancel()

	confirmation, err := p.channel.PublishWithDeferredConfirmWithContext(
		confirmCtx,
		p.exchange,
		routingKey,
		false,
		false,
		amqp.Publishing{
			Headers:      headers,
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			MessageId:    messageID,
			Timestamp:    createdAt,
			Type:         routingKey,
			Body:         body,
		},
	)
	if err != nil {
		return fmt.Errorf("publish %s event: %w", routingKey, err)
	}
	if confirmation == nil {
		return fmt.Errorf("publish %s event: publisher confirms are not enabled", routingKey)
	}

	acknowledged, err := confirmation.WaitContext(confirmCtx)
	if err != nil {
		return fmt.Errorf("wait for %s publisher confirm: %w", routingKey, err)
	}
	if !acknowledged {
		return fmt.Errorf("wait for %s publisher confirm: message was negatively acknowledged", routingKey)
	}

	return nil
}

func (p *Publisher) ensureConnectedLocked(ctx context.Context) error {
	if p.closed {
		return errors.New("rabbitmq publisher is closed")
	}
	if p.connection != nil && !p.connection.IsClosed() && p.channel != nil && !p.channel.IsClosed() {
		return nil
	}

	_ = p.closeConnectionLocked()

	connection, channel, err := openPublisherConnection(ctx, p.url, p.exchange, p.queue)
	if err != nil {
		return err
	}

	p.connection = connection
	p.channel = channel

	return nil
}

func (p *Publisher) closeConnectionLocked() error {
	var resultErr error
	if p.channel != nil && !p.channel.IsClosed() {
		if err := p.channel.Close(); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("close RabbitMQ channel: %w", err))
		}
	}
	if p.connection != nil && !p.connection.IsClosed() {
		if err := p.connection.Close(); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("close RabbitMQ connection: %w", err))
		}
	}

	p.channel = nil
	p.connection = nil

	return resultErr
}

func openPublisherConnection(
	ctx context.Context,
	url string,
	exchange string,
	queue string,
) (*amqp.Connection, *amqp.Channel, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	uri, err := amqp.ParseURI(url)
	if err != nil {
		return nil, nil, fmt.Errorf("parse RabbitMQ URL: %w", err)
	}

	connectTimeout := rabbitMQDefaultConnectTimeout
	if uri.ConnectionTimeout > 0 {
		connectTimeout = time.Duration(uri.ConnectionTimeout) * time.Millisecond
	}

	connectCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	setupDone := make(chan struct{})
	var setupDoneOnce sync.Once
	finishSetup := func() {
		setupDoneOnce.Do(func() {
			close(setupDone)
		})
	}
	defer finishSetup()

	connection, err := amqp.DialConfig(url, amqp.Config{
		Dial: func(network, addr string) (net.Conn, error) {
			connection, dialErr := (&net.Dialer{}).DialContext(connectCtx, network, addr)
			if dialErr != nil {
				return nil, dialErr
			}

			if deadline, ok := connectCtx.Deadline(); ok {
				if deadlineErr := connection.SetDeadline(deadline); deadlineErr != nil {
					_ = connection.Close()

					return nil, deadlineErr
				}
			}

			go interruptRabbitMQSetup(connectCtx, setupDone, connection)

			return connection, nil
		},
	})
	if err != nil {
		if ctxErr := connectCtx.Err(); ctxErr != nil {
			return nil, nil, fmt.Errorf("dial RabbitMQ: %w", ctxErr)
		}

		return nil, nil, fmt.Errorf("dial RabbitMQ: %w", err)
	}

	channel, err := connection.Channel()
	if err != nil {
		_ = connection.Close()

		if ctxErr := connectCtx.Err(); ctxErr != nil {
			return nil, nil, fmt.Errorf("open RabbitMQ channel: %w", ctxErr)
		}

		return nil, nil, fmt.Errorf("open RabbitMQ channel: %w", err)
	}

	if err := declareTopology(channel, exchange, queue); err != nil {
		_ = channel.Close()
		_ = connection.Close()

		if ctxErr := connectCtx.Err(); ctxErr != nil {
			return nil, nil, fmt.Errorf("declare RabbitMQ topology: %w", ctxErr)
		}

		return nil, nil, err
	}

	if err := channel.Confirm(false); err != nil {
		_ = channel.Close()
		_ = connection.Close()

		if ctxErr := connectCtx.Err(); ctxErr != nil {
			return nil, nil, fmt.Errorf("enable RabbitMQ publisher confirms: %w", ctxErr)
		}

		return nil, nil, fmt.Errorf("enable RabbitMQ publisher confirms: %w", err)
	}

	if err := connectCtx.Err(); err != nil {
		_ = channel.Close()
		_ = connection.Close()

		return nil, nil, err
	}

	finishSetup()

	return connection, channel, nil
}

func interruptRabbitMQSetup(ctx context.Context, setupDone <-chan struct{}, connection net.Conn) {
	select {
	case <-ctx.Done():
		select {
		case <-setupDone:
			return
		default:
			_ = connection.SetDeadline(time.Now())
		}
	case <-setupDone:
	}
}

func avatarS3Keys(avatar model.Avatar) []string {
	keys := make([]string, 0, 3)
	if avatar.S3Key != "" {
		keys = append(keys, avatar.S3Key)
	}

	for _, size := range []model.ThumbnailSize{
		model.ThumbnailSize100x100,
		model.ThumbnailSize300x300,
	} {
		if key := avatar.ThumbnailS3Keys[size]; key != "" {
			keys = append(keys, key)
		}
	}

	return keys
}
