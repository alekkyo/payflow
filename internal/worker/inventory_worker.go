// Package worker contains background job processors that consume Redis Streams.
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
	"github.com/alexkua/payflow/internal/domain/product"
	"github.com/alexkua/payflow/internal/queue"
	redisstore "github.com/alexkua/payflow/internal/store/redis"
)

const (
	inventoryWorkerGroup    = "inventory-workers"
	orderStatusKeyTTL       = time.Hour
	inventoryLockTTL        = 5 * time.Second
)

// InventoryWorker reads from stream:orders.created, reserves stock, and
// advances the saga to stream:payments.ready or stream:orders.cancelled.
type InventoryWorker struct {
	consumer             *queue.Consumer
	compensationConsumer *queue.Consumer
	producer             *queue.Producer
	orderStore           order.Store
	invSvc               *product.InventoryService
	locker               *redisstore.Locker
	rdb                  *redis.Client
	logger               *slog.Logger
	workerID             string
	delay                time.Duration
}

// NewInventoryWorker creates an InventoryWorker and registers its consumer group.
func NewInventoryWorker(
	ctx context.Context,
	rdb *redis.Client,
	orderStore order.Store,
	invSvc *product.InventoryService,
	locker *redisstore.Locker,
	workerID string,
	delay time.Duration,
	logger *slog.Logger,
) (*InventoryWorker, error) {
	consumer, err := queue.NewConsumer(
		ctx, rdb,
		queue.StreamOrdersCreated,
		inventoryWorkerGroup,
		workerID,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("inventory_worker.New: %w", err)
	}

	// A second consumer on payments.failed handles saga compensation —
	// releasing reserved inventory when a payment charge fails.
	compensationConsumer, err := queue.NewConsumer(
		ctx, rdb,
		queue.StreamPaymentsFailed,
		inventoryWorkerGroup+"-compensation",
		workerID,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("inventory_worker.New compensation: %w", err)
	}

	return &InventoryWorker{
		consumer:             consumer,
		compensationConsumer: compensationConsumer,
		producer:             queue.NewProducer(rdb),
		orderStore:           orderStore,
		invSvc:               invSvc,
		locker:               locker,
		rdb:                  rdb,
		logger:               logger,
		workerID:             workerID,
		delay:                delay,
	}, nil
}

// Run starts the worker loop. Blocks until ctx is cancelled.
func (w *InventoryWorker) Run(ctx context.Context) {
	w.consumer.Run(ctx, w.handle)
}

// RunCompensation starts a second loop that reads stream:payments.failed and
// releases reserved inventory when a payment charge fails.
func (w *InventoryWorker) RunCompensation(ctx context.Context) {
	w.compensationConsumer.Run(ctx, w.handleCompensation)
}

// handleCompensation releases inventory reserved for an order whose payment failed.
func (w *InventoryWorker) handleCompensation(ctx context.Context, msg redis.XMessage) error {
	orderID, err := uuidFromMessage(msg, "order_id")
	if err != nil {
		return fmt.Errorf("inventory_worker.handleCompensation parse order_id: %w", err)
	}

	w.logger.Info("inventory_worker compensation releasing stock", "order_id", orderID)

	o, err := w.orderStore.GetByID(ctx, orderID)
	if err != nil {
		return fmt.Errorf("inventory_worker.handleCompensation get order %s: %w", orderID, err)
	}

	// Release all reserved inventory for the order.
	for _, item := range o.Items {
		if err := w.invSvc.Release(ctx, item.ProductID, item.Quantity); err != nil {
			w.logger.Error("inventory_worker.handleCompensation release",
				"product_id", item.ProductID,
				"error", err,
			)
		}
	}

	// Cancel the order and record the reason.
	payload, _ := json.Marshal(map[string]string{"reason": "payment failed"})
	if err := w.orderStore.UpdateStatus(ctx,
		orderID,
		order.StatusCancelled,
		order.EventPaymentFailed,
		w.workerID,
		payload,
	); err != nil {
		w.logger.Error("inventory_worker.handleCompensation cancel order", "order_id", orderID, "error", err)
	}

	w.setStatusCache(ctx, orderID, order.StatusCancelled)

	w.logger.Info("inventory_worker compensation complete", "order_id", orderID)
	return nil
}

// handle processes one order-created event.
func (w *InventoryWorker) handle(ctx context.Context, msg redis.XMessage) error {
	if w.delay > 0 {
		select {
		case <-time.After(w.delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	start := time.Now()

	orderID, err := uuidFromMessage(msg, "order_id")
	if err != nil {
		return fmt.Errorf("inventory_worker.handle parse order_id: %w", err)
	}

	w.logger.Info("inventory_worker processing",
		"order_id", orderID,
		"message_id", msg.ID,
		"worker_id", w.workerID,
	)

	o, err := w.orderStore.GetByID(ctx, orderID)
	if err != nil {
		return fmt.Errorf("inventory_worker.handle get order %s: %w", orderID, err)
	}

	// Reserve stock for every line item, acquiring a per-product distributed lock.
	reserved := make([]reservedItem, 0, len(o.Items))
	for _, item := range o.Items {
		if err := w.reserveItem(ctx, o.ID, item, &reserved); err != nil {
			// Compensation: release any items already reserved before this failure.
			w.releaseAll(ctx, reserved)
			w.cancelOrder(ctx, o.ID, err.Error())
			return nil // return nil so the message is acknowledged — we handled the failure
		}
	}

	// All items reserved — advance the saga.
	payload, _ := json.Marshal(map[string]any{"reserved_items": len(reserved)})
	if err := w.orderStore.UpdateStatus(ctx,
		o.ID,
		order.StatusInventoryReserved,
		order.EventInventoryReserved,
		w.workerID,
		payload,
	); err != nil {
		w.releaseAll(ctx, reserved)
		return fmt.Errorf("inventory_worker.handle update status %s: %w", orderID, err)
	}

	// Update order status cache for SSE.
	w.setStatusCache(ctx, o.ID, order.StatusInventoryReserved)

	// Publish to the next stage in the saga.
	if _, err := w.producer.Publish(ctx, queue.StreamPaymentsReady, map[string]any{
		"order_id":    orderID.String(),
		"total_cents": o.TotalCents,
		"currency":    o.Currency,
	}); err != nil {
		// Non-fatal: the order status is already updated. The payment worker
		// can be re-triggered by a reconciliation job if needed.
		w.logger.Error("inventory_worker.handle publish payments.ready",
			"order_id", orderID,
			"error", err,
		)
	}

	w.logger.Info("inventory_worker reserved",
		"order_id", orderID,
		"items", len(reserved),
		"duration_ms", time.Since(start).Milliseconds(),
		"worker_id", w.workerID,
	)

	return nil
}

type reservedItem struct {
	productID uuid.UUID
	quantity  int
}

func (w *InventoryWorker) reserveItem(ctx context.Context, orderID uuid.UUID, item order.OrderItem, reserved *[]reservedItem) error {
	lockKey := fmt.Sprintf("lock:inventory:%s", item.ProductID)

	token, ok, err := w.locker.Acquire(ctx, lockKey, inventoryLockTTL)
	if err != nil {
		return fmt.Errorf("acquire lock product %s: %w", item.ProductID, err)
	}
	if !ok {
		return fmt.Errorf("lock contention on product %s", item.ProductID)
	}
	defer w.locker.Release(ctx, lockKey, token) //nolint:errcheck

	if err := w.invSvc.Reserve(ctx, item.ProductID, item.Quantity); err != nil {
		return fmt.Errorf("reserve product %s qty %d: %w", item.ProductID, item.Quantity, err)
	}

	*reserved = append(*reserved, reservedItem{productID: item.ProductID, quantity: item.Quantity})
	return nil
}

func (w *InventoryWorker) releaseAll(ctx context.Context, items []reservedItem) {
	for _, item := range items {
		if err := w.invSvc.Release(ctx, item.productID, item.quantity); err != nil {
			w.logger.Error("inventory_worker.releaseAll",
				"product_id", item.productID,
				"error", err,
			)
		}
	}
}

func (w *InventoryWorker) cancelOrder(ctx context.Context, orderID uuid.UUID, reason string) {
	payload, _ := json.Marshal(map[string]string{"reason": reason})
	if err := w.orderStore.UpdateStatus(ctx,
		orderID,
		order.StatusCancelled,
		order.EventInventoryFailed,
		w.workerID,
		payload,
	); err != nil {
		w.logger.Error("inventory_worker.cancelOrder update status",
			"order_id", orderID,
			"error", err,
		)
		return
	}
	w.setStatusCache(ctx, orderID, order.StatusCancelled)

	w.logger.Info("order cancelled — inventory unavailable",
		"order_id", orderID,
		"reason", reason,
	)
}

func (w *InventoryWorker) setStatusCache(ctx context.Context, orderID uuid.UUID, status string) {
	key := fmt.Sprintf("order:%s:status", orderID)
	if err := w.rdb.Set(ctx, key, status, orderStatusKeyTTL).Err(); err != nil {
		w.logger.Error("inventory_worker set status cache", "order_id", orderID, "error", err)
	}

	// Publish to Pub/Sub channel for SSE subscribers.
	channel := fmt.Sprintf("order:%s:events", orderID)
	if err := w.rdb.Publish(ctx, channel, status).Err(); err != nil {
		w.logger.Error("inventory_worker publish status event", "order_id", orderID, "error", err)
	}
}

func uuidFromMessage(msg redis.XMessage, key string) (uuid.UUID, error) {
	raw, ok := msg.Values[key]
	if !ok {
		return uuid.Nil, fmt.Errorf("missing field %q", key)
	}
	s, ok := raw.(string)
	if !ok {
		return uuid.Nil, fmt.Errorf("field %q is not a string", key)
	}
	return uuid.Parse(s)
}
