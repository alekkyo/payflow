package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/alexkua/payflow/internal/domain/order"
	"github.com/alexkua/payflow/internal/domain/payment"
	"github.com/alexkua/payflow/internal/queue"
	redisstore "github.com/alexkua/payflow/internal/store/redis"
)

const (
	paymentWorkerGroup  = "payment-workers"
	paymentLockTTL      = 30 * time.Second
	paymentStatusKeyTTL = time.Hour
)

// PaymentWorker reads from stream:payments.ready, creates a Stripe PaymentIntent,
// and records the attempt. The saga advances to payment_captured via Stripe webhook.
type PaymentWorker struct {
	consumer     *queue.Consumer
	producer     *queue.Producer
	orderStore   order.Store
	paymentStore payment.Store
	provider     payment.PaymentProvider
	locker       *redisstore.Locker
	rdb          *redis.Client
	logger       *slog.Logger
	workerID     string
	delay        time.Duration
}

// NewPaymentWorker creates a PaymentWorker and registers its consumer group.
func NewPaymentWorker(
	ctx context.Context,
	rdb *redis.Client,
	orderStore order.Store,
	paymentStore payment.Store,
	provider payment.PaymentProvider,
	locker *redisstore.Locker,
	workerID string,
	delay time.Duration,
	logger *slog.Logger,
) (*PaymentWorker, error) {
	consumer, err := queue.NewConsumer(
		ctx, rdb,
		queue.StreamPaymentsReady,
		paymentWorkerGroup,
		workerID,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("payment_worker.New: %w", err)
	}

	return &PaymentWorker{
		consumer:     consumer,
		producer:     queue.NewProducer(rdb),
		orderStore:   orderStore,
		paymentStore: paymentStore,
		provider:     provider,
		locker:       locker,
		rdb:          rdb,
		logger:       logger,
		workerID:     workerID,
		delay:        delay,
	}, nil
}

// Run starts the worker loop. Blocks until ctx is cancelled.
func (w *PaymentWorker) Run(ctx context.Context) {
	w.consumer.Run(ctx, w.handle)
}

// handle processes one payments.ready event.
func (w *PaymentWorker) handle(ctx context.Context, msg redis.XMessage) error {
	if w.delay > 0 {
		select {
		case <-time.After(w.delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	orderID, err := uuidFromMessage(msg, "order_id")
	if err != nil {
		return fmt.Errorf("payment_worker.handle parse order_id: %w", err)
	}

	w.logger.Info("payment_worker processing", "order_id", orderID, "worker_id", w.workerID)

	// Acquire a per-order lock to prevent two workers from charging the same order.
	lockKey := fmt.Sprintf("lock:payment:%s", orderID)
	token, ok, err := w.locker.Acquire(ctx, lockKey, paymentLockTTL)
	if err != nil {
		return fmt.Errorf("payment_worker.handle acquire lock %s: %w", orderID, err)
	}
	if !ok {
		// Another worker holds the lock — this order is already being charged.
		return nil
	}
	defer w.locker.Release(ctx, lockKey, token) //nolint:errcheck

	// Idempotency check: has a payment already been attempted for this order?
	existing, err := w.paymentStore.GetByOrderID(ctx, orderID)
	if err != nil && !errors.Is(err, payment.ErrNotFound) {
		return fmt.Errorf("payment_worker.handle get payment %s: %w", orderID, err)
	}

	if existing != nil {
		return w.handleExisting(ctx, existing)
	}

	// No payment yet — load the order and create the payment record.
	o, err := w.orderStore.GetByID(ctx, orderID)
	if err != nil {
		return fmt.Errorf("payment_worker.handle get order %s: %w", orderID, err)
	}

	// Idempotency key is deterministic: same order always maps to same Stripe request.
	idempotencyKey := "payment:" + orderID.String()

	// INSERT the payment row BEFORE calling Stripe. If we crash after creating the
	// Stripe intent but before saving the ID, the retry will use the same idempotency
	// key and Stripe will return the original intent — no double charge.
	p, err := w.paymentStore.Create(ctx, orderID, o.TotalCents, o.Currency, idempotencyKey)
	if err != nil {
		return fmt.Errorf("payment_worker.handle create payment %s: %w", orderID, err)
	}

	// Advance the order status so SSE clients see the transition.
	if err := w.orderStore.UpdateStatus(ctx,
		orderID,
		order.StatusPaymentProcessing,
		order.EventPaymentProcessing,
		w.workerID,
		nil,
	); err != nil {
		return fmt.Errorf("payment_worker.handle update order status %s: %w", orderID, err)
	}
	w.setStatusCache(ctx, orderID, order.StatusPaymentProcessing)

	// Call Stripe.
	result, err := w.provider.CreatePaymentIntent(ctx, payment.PaymentIntentRequest{
		OrderID:        orderID,
		AmountCents:    o.TotalCents,
		Currency:       o.Currency,
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		// Stripe call failed — record the failure and trigger compensation.
		payload, _ := json.Marshal(map[string]string{"error": err.Error()})
		_ = w.paymentStore.UpdateStatus(ctx, p.ID, payment.StatusFailed, "", err.Error(), payment.EventFailed, payload)
		w.publishPaymentFailed(ctx, orderID, err.Error())
		return nil // handled — ACK the message
	}

	// Stripe accepted the intent. Record the Stripe ID and mark as processing.
	payload, _ := json.Marshal(map[string]string{"stripe_payment_id": result.StripeID, "status": result.Status})
	if err := w.paymentStore.UpdateStatus(ctx,
		p.ID,
		payment.StatusProcessing,
		result.StripeID,
		"",
		payment.EventProcessing,
		payload,
	); err != nil {
		return fmt.Errorf("payment_worker.handle update payment %s: %w", orderID, err)
	}

	w.logger.Info("payment_worker intent created",
		"order_id", orderID,
		"payment_id", p.ID,
		"stripe_id", result.StripeID,
		"worker_id", w.workerID,
	)

	// The saga now waits for the Stripe webhook (payment_intent.succeeded or
	// payment_intent.payment_failed) to advance to the next state.
	return nil
}

// handleExisting fast-forwards a retry based on the existing payment's status.
func (w *PaymentWorker) handleExisting(ctx context.Context, p *payment.Payment) error {
	switch p.Status {
	case payment.StatusProcessing:
		// Stripe accepted the intent, waiting for webhook. Nothing to do.
		w.logger.Info("payment_worker skipping — already processing", "payment_id", p.ID)
		return nil

	case payment.StatusCaptured:
		// Webhook already arrived before this retry. Re-publish to downstream.
		w.logger.Info("payment_worker skipping — already captured", "payment_id", p.ID)
		payload, _ := json.Marshal(map[string]string{"payment_id": p.ID.String()})
		_, _ = w.producer.Publish(ctx, queue.StreamPaymentsCaptured, map[string]any{
			"order_id":   p.OrderID.String(),
			"payment_id": p.ID.String(),
		})
		_ = payload
		return nil

	case payment.StatusFailed:
		// Already failed and compensated. Re-publish failure so downstream workers
		// can compensate if they haven't already.
		w.publishPaymentFailed(ctx, p.OrderID, "retry: payment already failed")
		return nil

	default:
		// StatusPending: payment row was created but Stripe was never called
		// (crashed between the INSERT and the API call). Return an error so the
		// message is NOT acknowledged — the worker will retry and call Stripe
		// with the same idempotency key, which is safe.
		return fmt.Errorf("payment_worker: payment %s is pending, retrying stripe call", p.ID)
	}
}

func (w *PaymentWorker) publishPaymentFailed(ctx context.Context, orderID uuid.UUID, reason string) {
	if _, err := w.producer.Publish(ctx, queue.StreamPaymentsFailed, map[string]any{
		"order_id": orderID.String(),
		"reason":   reason,
	}); err != nil {
		w.logger.Error("payment_worker publish payments.failed", "order_id", orderID, "error", err)
	}
}

func (w *PaymentWorker) setStatusCache(ctx context.Context, orderID uuid.UUID, status string) {
	key := fmt.Sprintf("order:%s:status", orderID)
	if err := w.rdb.Set(ctx, key, status, paymentStatusKeyTTL).Err(); err != nil {
		w.logger.Error("payment_worker set status cache", "order_id", orderID, "error", err)
	}
	channel := fmt.Sprintf("order:%s:events", orderID)
	if err := w.rdb.Publish(ctx, channel, status).Err(); err != nil {
		w.logger.Error("payment_worker publish status event", "order_id", orderID, "error", err)
	}
}
