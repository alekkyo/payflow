package worker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/alexkua/payflow/internal/domain/payment"
	"github.com/alexkua/payflow/internal/domain/reconciliation"
	"github.com/alexkua/payflow/internal/observability"
	"github.com/alexkua/payflow/internal/queue"
)

const reconcileWorkerGroup = "reconcile-workers"

// ReconcileWorker compares local payment records against Stripe's records for a given day.
// It reads trigger messages from stream:reconciliation.trigger. Each message contains an
// optional "date" field (YYYY-MM-DD); if absent, yesterday's data is reconciled.
//
// The reconciliation process is:
//  1. List all local payments created on that date.
//  2. List all Stripe PaymentIntents created on that date.
//  3. Join by stripe_payment_id and compare amounts + statuses.
//  4. Report discrepancies to the reconciliation_discrepancies table and Prometheus.
type ReconcileWorker struct {
	consumer        *queue.Consumer
	paymentStore    payment.Store
	provider        payment.PaymentProvider
	reconcileStore  reconciliation.Store
	logger          *slog.Logger
	workerID        string
}

// NewReconcileWorker creates a ReconcileWorker and registers its consumer group.
func NewReconcileWorker(
	ctx context.Context,
	rdb *redis.Client,
	paymentStore payment.Store,
	provider payment.PaymentProvider,
	reconcileStore reconciliation.Store,
	workerID string,
	logger *slog.Logger,
) (*ReconcileWorker, error) {
	consumer, err := queue.NewConsumer(
		ctx, rdb,
		queue.StreamReconcileTrigger,
		reconcileWorkerGroup,
		workerID,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("reconcile_worker.New: %w", err)
	}

	return &ReconcileWorker{
		consumer:       consumer,
		paymentStore:   paymentStore,
		provider:       provider,
		reconcileStore: reconcileStore,
		logger:         logger,
		workerID:       workerID,
	}, nil
}

// Run starts the worker loop. Blocks until ctx is cancelled.
func (w *ReconcileWorker) Run(ctx context.Context) {
	w.consumer.Run(ctx, w.handle)
}

// handle processes one reconciliation trigger message.
func (w *ReconcileWorker) handle(ctx context.Context, msg redis.XMessage) error {
	start := time.Now()
	defer func() {
		observability.QueueProcessingDuration.WithLabelValues(
			queue.StreamReconcileTrigger, "reconcile-worker",
		).Observe(time.Since(start).Seconds())
	}()

	runDate := w.parseDateFromMessage(msg)

	w.logger.Info("reconcile_worker starting", "date", runDate.Format("2006-01-02"), "worker_id", w.workerID)

	run, err := w.reconcileStore.CreateRun(ctx, runDate)
	if err != nil {
		return fmt.Errorf("reconcile_worker.handle create run: %w", err)
	}

	if err := w.runReconciliation(ctx, run, runDate); err != nil {
		_ = w.reconcileStore.FailRun(ctx, run.ID)
		w.logger.Error("reconcile_worker failed", "run_id", run.ID, "error", err)
		return fmt.Errorf("reconcile_worker.handle: %w", err)
	}
	return nil
}

// runReconciliation executes the comparison and saves results.
func (w *ReconcileWorker) runReconciliation(ctx context.Context, run *reconciliation.Run, runDate time.Time) error {
	// Date window: midnight-to-midnight UTC for the target day.
	from := time.Date(runDate.Year(), runDate.Month(), runDate.Day(), 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)

	// Load both sides of the comparison in parallel.
	type localResult struct {
		payments []*payment.Payment
		err      error
	}
	type stripeResult struct {
		intents []payment.PaymentIntentSummary
		err     error
	}

	localCh := make(chan localResult, 1)
	stripeCh := make(chan stripeResult, 1)

	go func() {
		payments, err := w.paymentStore.ListPaymentsByDateRange(ctx, from, to)
		localCh <- localResult{payments, err}
	}()
	go func() {
		intents, err := w.provider.ListPaymentIntents(ctx, from, to)
		stripeCh <- stripeResult{intents, err}
	}()

	lr := <-localCh
	if lr.err != nil {
		return fmt.Errorf("reconcile_worker load local payments: %w", lr.err)
	}

	sr := <-stripeCh
	if sr.err != nil {
		return fmt.Errorf("reconcile_worker list stripe intents: %w", sr.err)
	}

	// Index local payments by their Stripe PaymentIntent ID.
	localByStripeID := make(map[string]*payment.Payment, len(lr.payments))
	for _, p := range lr.payments {
		if p.StripePaymentID != "" {
			localByStripeID[p.StripePaymentID] = p
		}
	}

	// Index Stripe intents by their ID.
	stripeByID := make(map[string]payment.PaymentIntentSummary, len(sr.intents))
	for _, pi := range sr.intents {
		stripeByID[pi.StripeID] = pi
	}

	var matched, mismatched, missingLocal, missingStripe int

	// Pass 1: for every Stripe intent, check if we have a matching local record.
	for stripeID, pi := range stripeByID {
		local, ok := localByStripeID[stripeID]
		if !ok {
			// Stripe has a charge we have no record of. Most critical discrepancy type.
			missingLocal++
			observability.ReconciliationDiscrepanciesTotal.WithLabelValues(reconciliation.DiscrepancyMissingLocal).Inc()
			d := reconciliation.Discrepancy{
				StripePaymentID:   stripeID,
				Type:              reconciliation.DiscrepancyMissingLocal,
				StripeAmountCents: pi.AmountCents,
				StripeStatus:      pi.Status,
			}
			if addErr := w.reconcileStore.AddDiscrepancy(ctx, run.ID, d); addErr != nil {
				w.logger.Error("reconcile_worker add discrepancy", "error", addErr)
			}
			continue
		}

		// Both sides exist — compare amounts and statuses.
		amountOK := local.AmountCents == pi.AmountCents
		statusOK := localStatusMatchesStripe(local.Status, pi.Status)

		if amountOK && statusOK {
			matched++
			continue
		}

		mismatched++
		discType := reconciliation.DiscrepancyStatusMismatch
		if !amountOK {
			discType = reconciliation.DiscrepancyAmountMismatch
		}

		observability.ReconciliationDiscrepanciesTotal.WithLabelValues(discType).Inc()
		pid := local.ID
		d := reconciliation.Discrepancy{
			PaymentID:         &pid,
			StripePaymentID:   stripeID,
			Type:              discType,
			OurAmountCents:    local.AmountCents,
			StripeAmountCents: pi.AmountCents,
			OurStatus:         local.Status,
			StripeStatus:      pi.Status,
		}
		if addErr := w.reconcileStore.AddDiscrepancy(ctx, run.ID, d); addErr != nil {
			w.logger.Error("reconcile_worker add discrepancy", "error", addErr)
		}
	}

	// Pass 2: for every local payment with a Stripe ID, check if Stripe has it.
	// This catches payments where the Stripe ID was saved locally but Stripe somehow lacks the record.
	for stripeID, local := range localByStripeID {
		if _, ok := stripeByID[stripeID]; ok {
			continue // already handled in pass 1
		}
		missingStripe++
		observability.ReconciliationDiscrepanciesTotal.WithLabelValues(reconciliation.DiscrepancyMissingStripe).Inc()
		pid := local.ID
		d := reconciliation.Discrepancy{
			PaymentID:       &pid,
			StripePaymentID: stripeID,
			Type:            reconciliation.DiscrepancyMissingStripe,
			OurAmountCents:  local.AmountCents,
			OurStatus:       local.Status,
		}
		if addErr := w.reconcileStore.AddDiscrepancy(ctx, run.ID, d); addErr != nil {
			w.logger.Error("reconcile_worker add discrepancy", "error", addErr)
		}
	}

	if err := w.reconcileStore.CompleteRun(ctx, run.ID, matched, mismatched, missingLocal, missingStripe); err != nil {
		return fmt.Errorf("reconcile_worker complete run: %w", err)
	}

	w.logger.Info("reconcile_worker completed",
		"date", runDate.Format("2006-01-02"),
		"matched", matched,
		"mismatched", mismatched,
		"missing_local", missingLocal,
		"missing_stripe", missingStripe,
	)
	return nil
}

// parseDateFromMessage extracts the "date" field (YYYY-MM-DD) from the trigger message.
// If absent or unparseable, it falls back to yesterday.
func (w *ReconcileWorker) parseDateFromMessage(msg redis.XMessage) time.Time {
	yesterday := time.Now().UTC().AddDate(0, 0, -1)
	dateStr, _ := msg.Values["date"].(string)
	if dateStr == "" {
		return yesterday
	}
	t, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		w.logger.Warn("reconcile_worker invalid date in trigger, using yesterday", "date", dateStr)
		return yesterday
	}
	return t
}

// localStatusMatchesStripe returns true when our local status is consistent with
// the Stripe PaymentIntent status. The mapping is:
//
//	captured  ↔ succeeded
//	failed    ↔ canceled | requires_payment_method | requires_action
//	processing ↔ processing | requires_capture
//	pending   ↔ requires_confirmation | requires_payment_method (before Stripe call)
func localStatusMatchesStripe(local, stripe string) bool {
	switch local {
	case payment.StatusCaptured:
		return stripe == "succeeded"
	case payment.StatusFailed:
		return stripe == "canceled" || stripe == "requires_payment_method" || stripe == "requires_action"
	case payment.StatusProcessing:
		return stripe == "processing" || stripe == "requires_capture" || stripe == "succeeded"
	case payment.StatusRefunded:
		return stripe == "succeeded" // the refund itself is a separate Stripe object
	default:
		return false
	}
}
