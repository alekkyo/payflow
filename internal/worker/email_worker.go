package worker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/alexkua/payflow/internal/domain/order"
	"github.com/alexkua/payflow/internal/observability"
	"github.com/alexkua/payflow/internal/queue"
)

const emailWorkerGroup = "email-workers"

// EmailWorker reads from stream:payments.captured and sends order confirmation emails.
// This is the final step of the happy-path saga: payment captured → customer notified.
//
// In production this would call a transactional email provider (SendGrid, AWS SES, Postmark).
// Here we simulate by writing a structured log entry — the log acts as the audit record
// that an email was dispatched.
type EmailWorker struct {
	consumer   *queue.Consumer
	orderStore order.Store
	logger     *slog.Logger
	workerID   string
}

// NewEmailWorker creates an EmailWorker and registers its consumer group.
func NewEmailWorker(
	ctx context.Context,
	rdb *redis.Client,
	orderStore order.Store,
	workerID string,
	logger *slog.Logger,
) (*EmailWorker, error) {
	consumer, err := queue.NewConsumer(
		ctx, rdb,
		queue.StreamPaymentsCaptured,
		emailWorkerGroup,
		workerID,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("email_worker.New: %w", err)
	}

	return &EmailWorker{
		consumer:   consumer,
		orderStore: orderStore,
		logger:     logger,
		workerID:   workerID,
	}, nil
}

// Run starts the worker loop. Blocks until ctx is cancelled.
func (w *EmailWorker) Run(ctx context.Context) {
	w.consumer.Run(ctx, w.handle)
}

func (w *EmailWorker) handle(ctx context.Context, msg redis.XMessage) error {
	start := time.Now()
	defer func() {
		observability.QueueProcessingDuration.WithLabelValues(
			queue.StreamPaymentsCaptured, "email-worker",
		).Observe(time.Since(start).Seconds())
	}()

	orderID, err := uuidFromMessage(msg, "order_id")
	if err != nil {
		return fmt.Errorf("email_worker.handle parse order_id: %w", err)
	}

	o, err := w.orderStore.GetByID(ctx, orderID)
	if err != nil {
		return fmt.Errorf("email_worker.handle get order %s: %w", orderID, err)
	}

	// Simulate sending a confirmation email.
	// A real implementation would build an HTML template and call an email API here.
	// The structured fields below are what you'd pass to SendGrid / SES.
	w.logger.Info("email_worker confirmation sent",
		"to",         "user:"+o.UserID.String(), // in prod: look up user email
		"subject",    "Your PayFlow order has been confirmed",
		"order_id",   o.ID,
		"total_cents", o.TotalCents,
		"currency",   o.Currency,
		"items",      len(o.Items),
		"worker_id",  w.workerID,
		"simulated",  true,
	)
	return nil
}
