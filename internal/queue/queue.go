// Package queue provides Redis Streams producer and consumer primitives.
package queue

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// Stream name constants — single source of truth for all stream keys.
const (
	StreamOrdersCreated       = "stream:orders.created"
	StreamPaymentsReady       = "stream:payments.ready"
	StreamPaymentsCaptured    = "stream:payments.captured"
	StreamPaymentsFailed      = "stream:payments.failed"
	StreamRefundsRequested    = "stream:refunds.requested"
	StreamStripeWebhooks      = "stream:stripe.webhooks"
	StreamReconcileTrigger    = "stream:reconciliation.trigger"
	StreamDeadLetter          = "stream:deadletter"
)

const (
	maxRetries     = 3
	claimMinIdle   = 30 * time.Second // reclaim messages idle this long
)

// Producer publishes messages to Redis Streams.
type Producer struct {
	client *redis.Client
}

// NewProducer creates a Producer using the given Redis client.
func NewProducer(client *redis.Client) *Producer {
	return &Producer{client: client}
}

// Publish sends a message to the given stream and returns the message ID.
func (p *Producer) Publish(ctx context.Context, stream string, fields map[string]any) (string, error) {
	id, err := p.client.XAdd(ctx, &redis.XAddArgs{
		Stream: stream,
		Values: fields,
	}).Result()
	if err != nil {
		return "", fmt.Errorf("producer.Publish %s: %w", stream, err)
	}
	return id, nil
}

// Consumer reads messages from a Redis Stream using a consumer group.
// Consumer groups provide at-least-once delivery — each message is processed
// by exactly one consumer in the group, and must be explicitly acknowledged.
type Consumer struct {
	client    *redis.Client
	stream    string
	group     string
	consumer  string
	batchSize int64
	logger    *slog.Logger
}

// NewConsumer creates a Consumer and ensures the consumer group exists.
func NewConsumer(ctx context.Context, client *redis.Client, stream, group, consumer string, logger *slog.Logger) (*Consumer, error) {
	// XGROUP CREATE ... $ MKSTREAM creates the group starting from the latest
	// message. "MKSTREAM" creates the stream itself if it doesn't exist yet.
	err := client.XGroupCreateMkStream(ctx, stream, group, "$").Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		return nil, fmt.Errorf("consumer.NewConsumer create group %s/%s: %w", stream, group, err)
	}

	return &Consumer{
		client:    client,
		stream:    stream,
		group:     group,
		consumer:  consumer,
		batchSize: 10,
		logger:    logger,
	}, nil
}

// HandlerFunc is the function signature for processing a single stream message.
// Return nil to acknowledge the message; return an error to trigger retry logic.
type HandlerFunc func(ctx context.Context, msg redis.XMessage) error

// Run starts the consume loop, blocking until ctx is cancelled.
// On each iteration it reads new messages, then checks for stale pending messages
// to reclaim from crashed workers.
func (c *Consumer) Run(ctx context.Context, handler HandlerFunc) {
	c.logger.Info("consumer starting", "stream", c.stream, "group", c.group, "consumer", c.consumer)

	for {
		select {
		case <-ctx.Done():
			c.logger.Info("consumer stopping", "stream", c.stream)
			return
		default:
		}

		// Read new messages assigned to this consumer.
		msgs, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    c.group,
			Consumer: c.consumer,
			Streams:  []string{c.stream, ">"},
			Count:    c.batchSize,
			Block:    2 * time.Second, // block up to 2s waiting for new messages
		}).Result()

		if err != nil && err != redis.Nil {
			if ctx.Err() != nil {
				return
			}
			c.logger.Error("consumer XReadGroup", "stream", c.stream, "error", err)
			continue
		}

		for _, stream := range msgs {
			for _, msg := range stream.Messages {
				c.process(ctx, msg, handler)
			}
		}

		// Reclaim messages that have been pending too long (from crashed workers).
		c.reclaimPending(ctx, handler)
	}
}

// process handles one message: calls the handler, acknowledges on success,
// or dead-letters after maxRetries failures.
func (c *Consumer) process(ctx context.Context, msg redis.XMessage, handler HandlerFunc) {
	err := handler(ctx, msg)
	if err == nil {
		c.ack(ctx, msg.ID)
		return
	}

	c.logger.Error("consumer handler error",
		"stream", c.stream,
		"message_id", msg.ID,
		"error", err,
	)

	// Check delivery count from the pending entry list.
	pending, pelErr := c.client.XPendingExt(ctx, &redis.XPendingExtArgs{
		Stream: c.stream,
		Group:  c.group,
		Start:  msg.ID,
		End:    msg.ID,
		Count:  1,
	}).Result()

	if pelErr != nil || len(pending) == 0 {
		return
	}

	if pending[0].RetryCount >= maxRetries {
		c.deadLetter(ctx, msg, err)
		c.ack(ctx, msg.ID)
	}
}

// reclaimPending uses XAUTOCLAIM to take ownership of messages idle longer than claimMinIdle.
func (c *Consumer) reclaimPending(ctx context.Context, handler HandlerFunc) {
	msgs, _, err := c.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{
		Stream:   c.stream,
		Group:    c.group,
		Consumer: c.consumer,
		MinIdle:  claimMinIdle,
		Start:    "0-0",
		Count:    c.batchSize,
	}).Result()

	if err != nil {
		if err != redis.Nil && ctx.Err() == nil {
			c.logger.Error("consumer XAutoClaim", "stream", c.stream, "error", err)
		}
		return
	}

	for _, msg := range msgs {
		c.process(ctx, msg, handler)
	}
}

func (c *Consumer) ack(ctx context.Context, msgID string) {
	if err := c.client.XAck(ctx, c.stream, c.group, msgID).Err(); err != nil {
		c.logger.Error("consumer XAck", "stream", c.stream, "message_id", msgID, "error", err)
	}
}

func (c *Consumer) deadLetter(ctx context.Context, msg redis.XMessage, handlerErr error) {
	fields := map[string]any{
		"source_stream": c.stream,
		"source_group":  c.group,
		"message_id":    msg.ID,
		"error":         handlerErr.Error(),
	}
	for k, v := range msg.Values {
		fields["payload_"+k] = v
	}

	if err := c.client.XAdd(ctx, &redis.XAddArgs{
		Stream: StreamDeadLetter,
		Values: fields,
	}).Err(); err != nil {
		c.logger.Error("consumer dead letter publish", "message_id", msg.ID, "error", err)
		return
	}

	c.logger.Warn("message dead-lettered",
		"stream", c.stream,
		"message_id", msg.ID,
		"original_error", handlerErr.Error(),
	)
}
