package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/alexkua/payflow/internal/domain/order"
	"github.com/alexkua/payflow/internal/domain/payment"
	"github.com/alexkua/payflow/internal/observability"
	"github.com/alexkua/payflow/internal/queue"
)

const webhookWorkerGroup = "webhook-workers"

// WebhookWorker reads pre-validated Stripe events from stream:stripe.webhooks
// and drives the saga forward based on the event type.
type WebhookWorker struct {
	consumer     *queue.Consumer
	producer     *queue.Producer
	orderStore   order.Store
	paymentStore payment.Store
	rdb          *redis.Client
	logger       *slog.Logger
	workerID     string
}

// NewWebhookWorker creates a WebhookWorker and registers its consumer group.
func NewWebhookWorker(
	ctx context.Context,
	rdb *redis.Client,
	orderStore order.Store,
	paymentStore payment.Store,
	workerID string,
	logger *slog.Logger,
) (*WebhookWorker, error) {
	consumer, err := queue.NewConsumer(
		ctx, rdb,
		queue.StreamStripeWebhooks,
		webhookWorkerGroup,
		workerID,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("webhook_worker.New: %w", err)
	}

	return &WebhookWorker{
		consumer:     consumer,
		producer:     queue.NewProducer(rdb),
		orderStore:   orderStore,
		paymentStore: paymentStore,
		rdb:          rdb,
		logger:       logger,
		workerID:     workerID,
	}, nil
}

// Run starts the worker loop. Blocks until ctx is cancelled.
func (w *WebhookWorker) Run(ctx context.Context) {
	w.consumer.Run(ctx, w.handle)
}

// handle routes a single Stripe event to the appropriate handler.
func (w *WebhookWorker) handle(ctx context.Context, msg redis.XMessage) error {
	start := time.Now()
	defer func() {
		observability.QueueProcessingDuration.WithLabelValues(
			queue.StreamStripeWebhooks, "webhook-worker",
		).Observe(time.Since(start).Seconds())
	}()

	eventType, _ := msg.Values["event_type"].(string)
	stripePaymentID, _ := msg.Values["stripe_payment_id"].(string)
	rawPayload, _ := msg.Values["raw_payload"].(string)

	observability.WebhookEventsTotal.WithLabelValues(eventType).Inc()

	w.logger.Info("webhook_worker processing",
		"event_type", eventType,
		"stripe_payment_id", stripePaymentID,
		"worker_id", w.workerID,
	)

	switch eventType {
	case "payment_intent.succeeded":
		return w.handlePaymentSucceeded(ctx, stripePaymentID, []byte(rawPayload))
	case "payment_intent.payment_failed":
		return w.handlePaymentFailed(ctx, stripePaymentID, []byte(rawPayload))
	default:
		// Unhandled event type — ACK and move on.
		w.logger.Info("webhook_worker ignoring unhandled event", "event_type", eventType)
		return nil
	}
}

func (w *WebhookWorker) handlePaymentSucceeded(ctx context.Context, stripePaymentID string, rawPayload []byte) error {
	p, err := w.findPaymentByStripeID(ctx, stripePaymentID)
	if err != nil {
		return err
	}

	if p.Status == payment.StatusCaptured {
		w.logger.Info("webhook_worker skipping — payment already captured", "payment_id", p.ID)
		return nil
	}

	if err := w.paymentStore.UpdateStatus(ctx,
		p.ID,
		payment.StatusCaptured,
		stripePaymentID,
		"",
		payment.EventCaptured,
		rawPayload,
	); err != nil {
		return fmt.Errorf("webhook_worker.handlePaymentSucceeded update payment %s: %w", p.ID, err)
	}

	if err := w.orderStore.UpdateStatus(ctx,
		p.OrderID,
		order.StatusPaymentCaptured,
		order.EventPaymentCaptured,
		w.workerID,
		rawPayload,
	); err != nil {
		return fmt.Errorf("webhook_worker.handlePaymentSucceeded update order %s: %w", p.OrderID, err)
	}

	w.setOrderStatusCache(ctx, p.OrderID, order.StatusPaymentCaptured)

	// Publish to the payments.captured stream so the email worker can react.
	if _, err := w.producer.Publish(ctx, queue.StreamPaymentsCaptured, map[string]any{
		"order_id":   p.OrderID.String(),
		"payment_id": p.ID.String(),
	}); err != nil {
		w.logger.Error("webhook_worker publish payments.captured", "order_id", p.OrderID, "error", err)
	}

	// Advance the saga: payment_captured → confirmed → fulfilled.
	// For this demo there is no physical fulfillment step, so both transitions
	// happen inline. Each UpdateStatus + setOrderStatusCache pair persists the
	// new state and pushes an SSE event to any connected browser.
	if err := w.orderStore.UpdateStatus(ctx,
		p.OrderID,
		order.StatusConfirmed,
		order.EventConfirmed,
		w.workerID,
		nil,
	); err != nil {
		return fmt.Errorf("webhook_worker.handlePaymentSucceeded confirm order %s: %w", p.OrderID, err)
	}
	w.setOrderStatusCache(ctx, p.OrderID, order.StatusConfirmed)

	if err := w.orderStore.UpdateStatus(ctx,
		p.OrderID,
		order.StatusFulfilled,
		order.EventFulfilled,
		w.workerID,
		nil,
	); err != nil {
		return fmt.Errorf("webhook_worker.handlePaymentSucceeded fulfill order %s: %w", p.OrderID, err)
	}
	w.setOrderStatusCache(ctx, p.OrderID, order.StatusFulfilled)

	observability.PaymentsTotal.WithLabelValues("captured").Inc()

	w.logger.Info("webhook_worker payment captured",
		"order_id", p.OrderID,
		"payment_id", p.ID,
		"stripe_id", stripePaymentID,
	)
	return nil
}

func (w *WebhookWorker) handlePaymentFailed(ctx context.Context, stripePaymentID string, rawPayload []byte) error {
	p, err := w.findPaymentByStripeID(ctx, stripePaymentID)
	if err != nil {
		return err
	}

	if p.Status == payment.StatusFailed {
		w.logger.Info("webhook_worker skipping — payment already failed", "payment_id", p.ID)
		return nil
	}

	// Extract failure reason from the raw Stripe event if present.
	var failureReason string
	var rawEvent map[string]any
	if json.Unmarshal(rawPayload, &rawEvent) == nil {
		if data, ok := rawEvent["data"].(map[string]any); ok {
			if obj, ok := data["object"].(map[string]any); ok {
				if lastErr, ok := obj["last_payment_error"].(map[string]any); ok {
					failureReason, _ = lastErr["message"].(string)
				}
			}
		}
	}

	if err := w.paymentStore.UpdateStatus(ctx,
		p.ID,
		payment.StatusFailed,
		stripePaymentID,
		failureReason,
		payment.EventFailed,
		rawPayload,
	); err != nil {
		return fmt.Errorf("webhook_worker.handlePaymentFailed update payment %s: %w", p.ID, err)
	}

	// Publish to payments.failed so the inventory worker releases reserved stock.
	if _, err := w.producer.Publish(ctx, queue.StreamPaymentsFailed, map[string]any{
		"order_id": p.OrderID.String(),
		"reason":   failureReason,
	}); err != nil {
		w.logger.Error("webhook_worker publish payments.failed", "order_id", p.OrderID, "error", err)
	}

	observability.PaymentsTotal.WithLabelValues("failed").Inc()

	w.logger.Info("webhook_worker payment failed",
		"order_id", p.OrderID,
		"payment_id", p.ID,
		"reason", failureReason,
	)
	return nil
}

// findPaymentByStripeID looks up a payment by its Stripe PaymentIntent ID.
// This is the correct join point between a Stripe webhook event and our local record.
func (w *WebhookWorker) findPaymentByStripeID(ctx context.Context, stripePaymentID string) (*payment.Payment, error) {
	p, err := w.paymentStore.GetByStripePaymentID(ctx, stripePaymentID)
	if err != nil {
		return nil, fmt.Errorf("webhook_worker.findPaymentByStripeID %s: %w", stripePaymentID, err)
	}
	return p, nil
}

func (w *WebhookWorker) setOrderStatusCache(ctx context.Context, orderID interface{ String() string }, status string) {
	key := fmt.Sprintf("order:%s:status", orderID.String())
	if err := w.rdb.Set(ctx, key, status, time.Hour).Err(); err != nil {
		w.logger.Error("webhook_worker set status cache", "order_id", orderID.String(), "error", err)
	}
	channel := fmt.Sprintf("order:%s:events", orderID.String())
	if err := w.rdb.Publish(ctx, channel, status).Err(); err != nil {
		w.logger.Error("webhook_worker publish status event", "order_id", orderID.String(), "error", err)
	}
}
