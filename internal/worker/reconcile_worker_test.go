package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/alexkua/payflow/internal/domain/payment"
	"github.com/alexkua/payflow/internal/domain/reconciliation"
)

// ── mock payment store ──────────────────────────────────────────────────────

type mockReconcilePaymentStore struct {
	payments []*payment.Payment
	err      error
}

func (m *mockReconcilePaymentStore) ListPaymentsByDateRange(_ context.Context, _, _ time.Time) ([]*payment.Payment, error) {
	return m.payments, m.err
}

// Unused Store methods — satisfy the payment.Store interface.
func (m *mockReconcilePaymentStore) Create(_ context.Context, _ uuid.UUID, _ int, _, _ string) (*payment.Payment, error) {
	return nil, nil
}
func (m *mockReconcilePaymentStore) GetByID(_ context.Context, _ uuid.UUID) (*payment.Payment, error) {
	return nil, nil
}
func (m *mockReconcilePaymentStore) GetByOrderID(_ context.Context, _ uuid.UUID) (*payment.Payment, error) {
	return nil, nil
}
func (m *mockReconcilePaymentStore) GetByIdempotencyKey(_ context.Context, _ string) (*payment.Payment, error) {
	return nil, nil
}
func (m *mockReconcilePaymentStore) GetByStripePaymentID(_ context.Context, _ string) (*payment.Payment, error) {
	return nil, nil
}
func (m *mockReconcilePaymentStore) UpdateStatus(_ context.Context, _ uuid.UUID, _, _, _, _ string, _ []byte) error {
	return nil
}
func (m *mockReconcilePaymentStore) IsWebhookProcessed(_ context.Context, _ string) (bool, error) {
	return false, nil
}
func (m *mockReconcilePaymentStore) MarkWebhookProcessed(_ context.Context, _, _ string) error {
	return nil
}
func (m *mockReconcilePaymentStore) CreateRefund(_ context.Context, _ uuid.UUID, _ int, _, _ string) (*payment.Refund, error) {
	return nil, nil
}
func (m *mockReconcilePaymentStore) UpdateRefund(_ context.Context, _ uuid.UUID, _, _ string) error {
	return nil
}
func (m *mockReconcilePaymentStore) GetRefundsByPaymentID(_ context.Context, _ uuid.UUID) ([]*payment.Refund, error) {
	return nil, nil
}
func (m *mockReconcilePaymentStore) GetRefundByID(_ context.Context, _ uuid.UUID) (*payment.Refund, error) {
	return nil, nil
}
func (m *mockReconcilePaymentStore) GetRefundByIdempotencyKey(_ context.Context, _ string) (*payment.Refund, error) {
	return nil, nil
}

// ── mock payment provider ───────────────────────────────────────────────────

type mockReconcileProvider struct {
	intents []payment.PaymentIntentSummary
	err     error
}

func (m *mockReconcileProvider) ListPaymentIntents(_ context.Context, _, _ time.Time) ([]payment.PaymentIntentSummary, error) {
	return m.intents, m.err
}

func (m *mockReconcileProvider) CreatePaymentIntent(_ context.Context, _ payment.PaymentIntentRequest) (*payment.PaymentIntentResult, error) {
	return nil, nil
}
func (m *mockReconcileProvider) CreateRefund(_ context.Context, _ payment.RefundRequest) (*payment.RefundResult, error) {
	return nil, nil
}
func (m *mockReconcileProvider) ConstructWebhookEvent(_ []byte, _ string) (*payment.WebhookEvent, error) {
	return nil, nil
}

// ── mock reconcile store ────────────────────────────────────────────────────

type mockReconcileStore struct {
	run          *reconciliation.Run
	discrepancies []reconciliation.Discrepancy
	completeErr  error
}

func (m *mockReconcileStore) CreateRun(_ context.Context, runDate time.Time) (*reconciliation.Run, error) {
	if m.run == nil {
		m.run = &reconciliation.Run{ID: uuid.New(), RunDate: runDate}
	}
	return m.run, nil
}

func (m *mockReconcileStore) CompleteRun(_ context.Context, _ uuid.UUID, _, _, _, _ int) error {
	return m.completeErr
}

func (m *mockReconcileStore) FailRun(_ context.Context, _ uuid.UUID) error {
	return nil
}

func (m *mockReconcileStore) AddDiscrepancy(_ context.Context, _ uuid.UUID, d reconciliation.Discrepancy) error {
	m.discrepancies = append(m.discrepancies, d)
	return nil
}

func (m *mockReconcileStore) GetLatestRun(_ context.Context) (*reconciliation.Run, error) {
	return nil, nil
}

func (m *mockReconcileStore) ListRuns(_ context.Context, _ int) ([]*reconciliation.Run, error) {
	return nil, nil
}

func (m *mockReconcileStore) GetRunWithDiscrepancies(_ context.Context, _ uuid.UUID) (*reconciliation.Run, error) {
	return nil, nil
}

// ── localStatusMatchesStripe ─────────────────────────────────────────────────

func TestLocalStatusMatchesStripe(t *testing.T) {
	tests := []struct {
		local  string
		stripe string
		want   bool
	}{
		// captured ↔ succeeded
		{payment.StatusCaptured, "succeeded", true},
		{payment.StatusCaptured, "processing", false},
		{payment.StatusCaptured, "canceled", false},

		// failed ↔ canceled | requires_payment_method | requires_action
		{payment.StatusFailed, "canceled", true},
		{payment.StatusFailed, "requires_payment_method", true},
		{payment.StatusFailed, "requires_action", true},
		{payment.StatusFailed, "succeeded", false},
		{payment.StatusFailed, "processing", false},

		// processing ↔ processing | requires_capture | succeeded (already captured)
		{payment.StatusProcessing, "processing", true},
		{payment.StatusProcessing, "requires_capture", true},
		{payment.StatusProcessing, "succeeded", true},
		{payment.StatusProcessing, "canceled", false},

		// refunded ↔ succeeded (refund is a separate Stripe object)
		{payment.StatusRefunded, "succeeded", true},
		{payment.StatusRefunded, "canceled", false},

		// pending: no stripe match
		{payment.StatusPending, "succeeded", false},
		{payment.StatusPending, "requires_confirmation", false},
		{"unknown", "succeeded", false},
	}

	for _, tt := range tests {
		t.Run(tt.local+"/"+tt.stripe, func(t *testing.T) {
			got := localStatusMatchesStripe(tt.local, tt.stripe)
			if got != tt.want {
				t.Errorf("localStatusMatchesStripe(%q, %q) = %v, want %v", tt.local, tt.stripe, got, tt.want)
			}
		})
	}
}

// ── parseDateFromMessage ────────────────────────────────────────────────────

func TestParseDateFromMessage(t *testing.T) {
	w := &ReconcileWorker{logger: newDiscardLogger()}
	yesterday := time.Now().UTC().AddDate(0, 0, -1)

	tests := []struct {
		name     string
		values   map[string]any
		wantDate string // YYYY-MM-DD
	}{
		{
			name:     "valid date in message",
			values:   map[string]any{"date": "2024-03-15"},
			wantDate: "2024-03-15",
		},
		{
			name:     "missing date defaults to yesterday",
			values:   map[string]any{},
			wantDate: yesterday.Format("2006-01-02"),
		},
		{
			name:     "invalid date defaults to yesterday",
			values:   map[string]any{"date": "not-a-date"},
			wantDate: yesterday.Format("2006-01-02"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := redis.XMessage{Values: tt.values}
			got := w.parseDateFromMessage(msg)
			if got.Format("2006-01-02") != tt.wantDate {
				t.Errorf("parseDateFromMessage() = %s, want %s", got.Format("2006-01-02"), tt.wantDate)
			}
		})
	}
}

// ── runReconciliation ───────────────────────────────────────────────────────

func TestRunReconciliation(t *testing.T) {
	runDate := time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)

	// Shared test data
	stripeID1 := "pi_matched"
	stripeID2 := "pi_amount_mismatch"
	stripeID3 := "pi_status_mismatch"
	stripeID4 := "pi_missing_local"
	stripeID5 := "pi_missing_stripe"

	paymentID1 := uuid.New()
	paymentID2 := uuid.New()
	paymentID3 := uuid.New()
	paymentID5 := uuid.New()

	tests := []struct {
		name                    string
		localPayments           []*payment.Payment
		stripeIntents           []payment.PaymentIntentSummary
		wantDiscrepancyTypes    []string
		wantDiscrepancyCount    int
		paymentStoreErr         error
		providerErr             error
	}{
		{
			name: "all matched — no discrepancies",
			localPayments: []*payment.Payment{
				{ID: paymentID1, StripePaymentID: stripeID1, AmountCents: 1999, Status: payment.StatusCaptured},
			},
			stripeIntents: []payment.PaymentIntentSummary{
				{StripeID: stripeID1, AmountCents: 1999, Status: "succeeded"},
			},
			wantDiscrepancyCount: 0,
		},
		{
			name: "amount mismatch detected",
			localPayments: []*payment.Payment{
				{ID: paymentID2, StripePaymentID: stripeID2, AmountCents: 1999, Status: payment.StatusCaptured},
			},
			stripeIntents: []payment.PaymentIntentSummary{
				{StripeID: stripeID2, AmountCents: 2500, Status: "succeeded"}, // different amount
			},
			wantDiscrepancyCount: 1,
			wantDiscrepancyTypes: []string{reconciliation.DiscrepancyAmountMismatch},
		},
		{
			name: "status mismatch detected",
			localPayments: []*payment.Payment{
				// Stripe says captured+refunded (canceled), but we still show it as captured.
				{ID: paymentID3, StripePaymentID: stripeID3, AmountCents: 1999, Status: payment.StatusCaptured},
			},
			stripeIntents: []payment.PaymentIntentSummary{
				{StripeID: stripeID3, AmountCents: 1999, Status: "canceled"},
			},
			wantDiscrepancyCount: 1,
			wantDiscrepancyTypes: []string{reconciliation.DiscrepancyStatusMismatch},
		},
		{
			name:          "charge in Stripe with no local record",
			localPayments: []*payment.Payment{},
			stripeIntents: []payment.PaymentIntentSummary{
				{StripeID: stripeID4, AmountCents: 5000, Status: "succeeded"},
			},
			wantDiscrepancyCount: 1,
			wantDiscrepancyTypes: []string{reconciliation.DiscrepancyMissingLocal},
		},
		{
			name: "local record with no Stripe counterpart",
			localPayments: []*payment.Payment{
				{ID: paymentID5, StripePaymentID: stripeID5, AmountCents: 3000, Status: payment.StatusProcessing},
			},
			stripeIntents:        []payment.PaymentIntentSummary{},
			wantDiscrepancyCount: 1,
			wantDiscrepancyTypes: []string{reconciliation.DiscrepancyMissingStripe},
		},
		{
			name: "local payment without stripe ID skipped",
			localPayments: []*payment.Payment{
				{ID: uuid.New(), StripePaymentID: "", AmountCents: 1000, Status: payment.StatusPending},
			},
			stripeIntents:        []payment.PaymentIntentSummary{},
			wantDiscrepancyCount: 0,
		},
		{
			name:            "local store error propagates",
			paymentStoreErr: errors.New("db unavailable"),
			wantDiscrepancyCount: 0,
		},
		{
			name:            "stripe provider error propagates",
			localPayments:   []*payment.Payment{},
			providerErr:     errors.New("stripe unavailable"),
			wantDiscrepancyCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rs := &mockReconcileStore{}
			run := &reconciliation.Run{ID: uuid.New(), RunDate: runDate}

			w := &ReconcileWorker{
				paymentStore: &mockReconcilePaymentStore{
					payments: tt.localPayments,
					err:      tt.paymentStoreErr,
				},
				provider: &mockReconcileProvider{
					intents: tt.stripeIntents,
					err:     tt.providerErr,
				},
				reconcileStore: rs,
				logger:         newDiscardLogger(),
				workerID:       "test",
			}

			err := w.runReconciliation(context.Background(), run, runDate)

			if tt.paymentStoreErr != nil || tt.providerErr != nil {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("runReconciliation() unexpected error: %v", err)
			}

			if len(rs.discrepancies) != tt.wantDiscrepancyCount {
				t.Errorf("discrepancy count = %d, want %d", len(rs.discrepancies), tt.wantDiscrepancyCount)
			}

			for i, wantType := range tt.wantDiscrepancyTypes {
				if i >= len(rs.discrepancies) {
					t.Errorf("missing discrepancy[%d]: want type %q", i, wantType)
					continue
				}
				if rs.discrepancies[i].Type != wantType {
					t.Errorf("discrepancy[%d].Type = %q, want %q", i, rs.discrepancies[i].Type, wantType)
				}
			}
		})
	}
}
