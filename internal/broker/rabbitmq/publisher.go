package rabbitmq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/xhrobj/gophprofile/internal/event"
	"github.com/xhrobj/gophprofile/internal/model"
)

const publisherConfirmTimeout = 5 * time.Second

// Publisher публикует события аватаров в RabbitMQ с publisher confirms.
type Publisher struct {
	connection *amqp.Connection
	channel    *amqp.Channel
	exchange   string
	mu         sync.Mutex
}

// OpenPublisher подключается к RabbitMQ, идемпотентно объявляет topology и включает publisher confirms.
func OpenPublisher(url, exchange, queue string) (*Publisher, error) {
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

	if err := channel.Confirm(false); err != nil {
		_ = channel.Close()
		_ = connection.Close()

		return nil, fmt.Errorf("enable RabbitMQ publisher confirms: %w", err)
	}

	return &Publisher{
		connection: connection,
		channel:    channel,
		exchange:   exchange,
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

// Close закрывает RabbitMQ channel и connection Publisher.
func (p *Publisher) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	var resultErr error
	if p.channel != nil {
		if err := p.channel.Close(); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("close RabbitMQ channel: %w", err))
		}
	}
	if p.connection != nil {
		if err := p.connection.Close(); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("close RabbitMQ connection: %w", err))
		}
	}

	return resultErr
}

func (p *Publisher) publish(
	ctx context.Context,
	routingKey string,
	messageID string,
	createdAt time.Time,
	message any,
) error {
	body, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("marshal %s event: %w", routingKey, err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	confirmCtx, cancel := context.WithTimeout(ctx, publisherConfirmTimeout)
	defer cancel()

	confirmation, err := p.channel.PublishWithDeferredConfirmWithContext(
		confirmCtx,
		p.exchange,
		routingKey,
		false,
		false,
		amqp.Publishing{
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
