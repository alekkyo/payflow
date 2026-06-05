package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/alexkua/payflow/internal/domain/order"
	"github.com/alexkua/payflow/internal/domain/payment"
	"github.com/alexkua/payflow/internal/domain/product"
	"github.com/alexkua/payflow/internal/queue"
)

const refundWorkerGroup = "refund-workers"

// RefundWorker reads from stream:refunds.requested, calls the Stripe Refunds API,
// releases the reserved inventory, and updates the order status.
type RefundWorker struct {
	consumer     *queue.Consumer
	orderStore   order.Store
	paymentStore payment.Store
	invSvc       *product.InventoryService
	provider     payment.PaymentProvider
	rdb          *redis.Client
	logger       *slog.Logger
	workerID     string
}

// NewRefundWorker creates a RefundWorker and registers its consumer group.
func NewRefundWorker(
	ctx context.Context,
	rdb *redis.Client,
	orderStore order.Store,
	paymentStore payment.Store,
	invSvc *product.InventoryService,
	provider payment.PaymentProvider,
	workerID string,
	logger *slog.Logger,
) (*RefundWorker, error) {
	consumer, err := queue.NewConsumer(
		ctx, rdb,
		queue.StreamRefundsRequested,
		refundWorkerGroup,
		workerID,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("refund_worker.New: %w", err)
	}

	return &RefundWorker{
		consumer:     consumer,
		orderStore:   orderStore,
		paymentStore: paymentStore,
		invSvc:       invSvc,
		provider:     provider,
		rdb:          rdb,
		logger:       logger,
		workerID:     workerID,
	}, nil
}

// Run starts the worker loop. Blocks until ctx is cancelled.
func (w *RefundWorker) Run(ctx context.Context) {
	w.consumer.Run(ctx, w.handle)
}

// handle processes one refund request.
func (w *RefundWorker) handle(ctx context.Context, msg redis.XMessage) error {
	refundID, err := uuidFromMessage(msg, "refund_id")
	if err != nil {
		return fmt.Errorf("refund_worker.handle parse refund_id: %w", err)
	}

	r, err := w.paymentStore.GetRefundByID(ctx, refundID)
	if err != nil {
		return fmt.Errorf("refund_worker.handle get refund %s: %w", refundID, err)
	}

	// Idempotency: if the refund already succeeded, nothing to do.
	if r.Status == payment.RefundStatusSucceeded {
		w.logger.Info("refund_worker skipping — already succeeded", "refund_id", refundID)
		return nil
	}

	p, err := w.paymentStore.GetByID(ctx, r.PaymentID)
	if err != nil {
		return fmt.Errorf("refund_worker.handle get payment %s: %w", r.PaymentID, err)
	}

	// Call Stripe. The idempotency key on the refund ensures Stripe deduplicates.
	result, err := w.provider.CreateRefund(ctx, payment.RefundRequest{
		StripePaymentID: p.StripePaymentID,
		AmountCents:     r.AmountCents,
		IdempotencyKey:  r.IdempotencyKey,
	})
	if err != nil {
		_ = w.paymentStore.UpdateRefund(ctx, r.ID, "", payment.RefundStatusFailed)
		return fmt.Errorf("refund_worker.handle stripe refund %s: %w", refundID, err)
	}

	if err := w.paymentStore.UpdateRefund(ctx, r.ID, result.StripeRefundID, payment.RefundStatusSucceeded); err != nil {
		return fmt.Errorf("refund_worker.handle update refund %s: %w", refundID, err)
	}

	// Release the reserved inventory so the stock becomes available again.
	o, err := w.orderStore.GetByID(ctx, p.OrderID)
	if err == nil {
		for _, item := range o.Items {
			if err := w.invSvc.Release(ctx, item.ProductID, item.Quantity); err != nil {
				w.logger.Error("refund_worker release inventory",
					"product_id", item.ProductID,
					"error", err,
				)
			}
		}
	}

	// Update order status to refunded.
	payload, _ := json.Marshal(map[string]string{
		"refund_id":        r.ID.String(),
		"stripe_refund_id": result.StripeRefundID,
	})
	if err := w.orderStore.UpdateStatus(ctx,
		p.OrderID,
		order.StatusRefunded,
		order.EventRefunded,
		w.workerID,
		payload,
	); err != nil {
		w.logger.Error("refund_worker update order status", "order_id", p.OrderID, "error", err)
	}

	// Update payment status to refunded.
	refundPayload, _ := json.Marshal(map[string]string{"stripe_refund_id": result.StripeRefundID})
	_ = w.paymentStore.UpdateStatus(ctx, p.ID, payment.StatusRefunded, "", "", payment.EventRefunded, refundPayload)

	w.setOrderStatusCache(ctx, p.OrderID, order.StatusRefunded)

	w.logger.Info("refund_worker refund succeeded",
		"refund_id", refundID,
		"order_id", p.OrderID,
		"stripe_refund_id", result.StripeRefundID,
	)
	return nil
}

func (w *RefundWorker) setOrderStatusCache(ctx context.Context, orderID uuid.UUID, status string) {
	key := fmt.Sprintf("order:%s:status", orderID)
	if err := w.rdb.Set(ctx, key, status, time.Hour).Err(); err != nil {
		w.logger.Error("refund_worker set status cache", "order_id", orderID, "error", err)
	}
	channel := fmt.Sprintf("order:%s:events", orderID)
	if err := w.rdb.Publish(ctx, channel, status).Err(); err != nil {
		w.logger.Error("refund_worker publish status event", "order_id", orderID, "error", err)
	}
}
