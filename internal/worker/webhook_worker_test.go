package worker

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/alexkua/payflow/internal/domain/order"
	"github.com/alexkua/payflow/internal/domain/payment"
	"github.com/alexkua/payflow/internal/queue"
)

// ── mock order store ─────────────────────────────────────────────────────────

type mockOrderStore struct {
	statusUpdates []statusUpdate
	updateErr     error
}

type statusUpdate struct {
	status    string
	eventType string
}

func (m *mockOrderStore) Create(_ context.Context, _ order.CreateOrderRequest, _ int, _ []order.OrderItem) (*order.Order, error) {
	return nil, nil
}
func (m *mockOrderStore) GetByID(_ context.Context, _ uuid.UUID) (*order.Order, error) {
	return nil, nil
}
func (m *mockOrderStore) GetByIdempotencyKey(_ context.Context, _ string) (*order.Order, error) {
	return nil, nil
}
func (m *mockOrderStore) ListByUserID(_ context.Context, _ uuid.UUID) ([]*order.Order, error) {
	return nil, nil
}
func (m *mockOrderStore) UpdateStatus(_ context.Context, _ uuid.UUID, status, eventType, _ string, _ []byte) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.statusUpdates = append(m.statusUpdates, statusUpdate{status, eventType})
	return nil
}
func (m *mockOrderStore) Cancel(_ context.Context, _ uuid.UUID, _ string) error {
	return nil
}

// ── mock payment store ───────────────────────────────────────────────────────

type mockWebhookPaymentStore struct {
	byStripeID map[string]*payment.Payment
	statusHistory []string
	updateErr error
}

func newMockWebhookPaymentStore() *mockWebhookPaymentStore {
	return &mockWebhookPaymentStore{byStripeID: make(map[string]*payment.Payment)}
}

func (m *mockWebhookPaymentStore) GetByStripePaymentID(_ context.Context, id string) (*payment.Payment, error) {
	p, ok := m.byStripeID[id]
	if !ok {
		return nil, payment.ErrNotFound
	}
	return p, nil
}

func (m *mockWebhookPaymentStore) UpdateStatus(_ context.Context, _ uuid.UUID, status, _, _, _ string, _ []byte) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.statusHistory = append(m.statusHistory, status)
	return nil
}

func (m *mockWebhookPaymentStore) Create(_ context.Context, _ uuid.UUID, _ int, _, _ string) (*payment.Payment, error) {
	return nil, nil
}
func (m *mockWebhookPaymentStore) GetByID(_ context.Context, _ uuid.UUID) (*payment.Payment, error) {
	return nil, nil
}
func (m *mockWebhookPaymentStore) GetByOrderID(_ context.Context, _ uuid.UUID) (*payment.Payment, error) {
	return nil, nil
}
func (m *mockWebhookPaymentStore) GetByIdempotencyKey(_ context.Context, _ string) (*payment.Payment, error) {
	return nil, nil
}
func (m *mockWebhookPaymentStore) IsWebhookProcessed(_ context.Context, _ string) (bool, error) {
	return false, nil
}
func (m *mockWebhookPaymentStore) MarkWebhookProcessed(_ context.Context, _, _ string) error {
	return nil
}
func (m *mockWebhookPaymentStore) CreateRefund(_ context.Context, _ uuid.UUID, _ int, _, _ string) (*payment.Refund, error) {
	return nil, nil
}
func (m *mockWebhookPaymentStore) UpdateRefund(_ context.Context, _ uuid.UUID, _, _ string) error {
	return nil
}
func (m *mockWebhookPaymentStore) GetRefundsByPaymentID(_ context.Context, _ uuid.UUID) ([]*payment.Refund, error) {
	return nil, nil
}
func (m *mockWebhookPaymentStore) GetRefundByID(_ context.Context, _ uuid.UUID) (*payment.Refund, error) {
	return nil, nil
}
func (m *mockWebhookPaymentStore) GetRefundByIdempotencyKey(_ context.Context, _ string) (*payment.Refund, error) {
	return nil, nil
}
func (m *mockWebhookPaymentStore) ListPaymentsByDateRange(_ context.Context, _, _ time.Time) ([]*payment.Payment, error) {
	return nil, nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

func newTestWebhookWorker(t *testing.T, orderStore order.Store, paymentStore payment.Store) (*WebhookWorker, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })

	return &WebhookWorker{
		consumer:     nil, // not used in unit tests
		producer:     queue.NewProducer(rdb),
		orderStore:   orderStore,
		paymentStore: paymentStore,
		rdb:          rdb,
		logger:       newDiscardLogger(),
		workerID:     "test-worker",
	}, mr
}

// ── handlePaymentSucceeded ────────────────────────────────────────────────────

func TestWebhookWorker_HandlePaymentSucceeded(t *testing.T) {
	ctx := context.Background()
	orderID := uuid.New()
	paymentID := uuid.New()
	stripeID := "pi_test_success"

	tests := []struct {
		name               string
		payment            *payment.Payment
		orderUpdateErr     error
		wantErr            bool
		wantPaymentUpdates int
		wantOrderStatuses  []string
	}{
		{
			name: "happy path — advances saga to fulfilled",
			payment: &payment.Payment{
				ID:              paymentID,
				OrderID:         orderID,
				StripePaymentID: stripeID,
				Status:          payment.StatusProcessing,
			},
			wantPaymentUpdates: 1,
			// payment_captured → confirmed → fulfilled
			wantOrderStatuses: []string{
				order.StatusPaymentCaptured,
				order.StatusConfirmed,
				order.StatusFulfilled,
			},
		},
		{
			name: "idempotency — already captured skips processing",
			payment: &payment.Payment{
				ID:              paymentID,
				OrderID:         orderID,
				StripePaymentID: stripeID,
				Status:          payment.StatusCaptured, // already done
			},
			wantPaymentUpdates: 0,
			wantOrderStatuses:  nil,
		},
		{
			name: "order update failure propagates error",
			payment: &payment.Payment{
				ID:              paymentID,
				OrderID:         orderID,
				StripePaymentID: stripeID,
				Status:          payment.StatusProcessing,
			},
			orderUpdateErr: errTest,
			wantErr:        true,
			// Payment is updated to captured before the order update attempt, so 1 update occurs.
			wantPaymentUpdates: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ps := newMockWebhookPaymentStore()
			ps.byStripeID[stripeID] = tt.payment
			os := &mockOrderStore{updateErr: tt.orderUpdateErr}

			w, _ := newTestWebhookWorker(t, os, ps)

			err := w.handlePaymentSucceeded(ctx, stripeID, []byte(`{}`))

			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if got := len(ps.statusHistory); got != tt.wantPaymentUpdates {
				t.Errorf("payment status updates = %d, want %d", got, tt.wantPaymentUpdates)
			}

			if len(tt.wantOrderStatuses) > 0 {
				gotStatuses := make([]string, len(os.statusUpdates))
				for i, u := range os.statusUpdates {
					gotStatuses[i] = u.status
				}
				if len(gotStatuses) != len(tt.wantOrderStatuses) {
					t.Fatalf("order status update count = %d, want %d (got %v)", len(gotStatuses), len(tt.wantOrderStatuses), gotStatuses)
				}
				for i, want := range tt.wantOrderStatuses {
					if gotStatuses[i] != want {
						t.Errorf("order status[%d] = %q, want %q", i, gotStatuses[i], want)
					}
				}
			}
		})
	}
}

// ── handlePaymentFailed ───────────────────────────────────────────────────────

func TestWebhookWorker_HandlePaymentFailed(t *testing.T) {
	ctx := context.Background()
	orderID := uuid.New()
	paymentID := uuid.New()
	stripeID := "pi_test_failed"

	tests := []struct {
		name               string
		payment            *payment.Payment
		rawPayload         []byte
		wantErr            bool
		wantPaymentUpdates int
		wantFailureReason  string
	}{
		{
			name: "happy path — marks payment failed",
			payment: &payment.Payment{
				ID:              paymentID,
				OrderID:         orderID,
				StripePaymentID: stripeID,
				Status:          payment.StatusProcessing,
			},
			rawPayload:         []byte(`{}`),
			wantPaymentUpdates: 1,
		},
		{
			name: "idempotency — already failed skips processing",
			payment: &payment.Payment{
				ID:              paymentID,
				OrderID:         orderID,
				StripePaymentID: stripeID,
				Status:          payment.StatusFailed, // already done
			},
			rawPayload:         []byte(`{}`),
			wantPaymentUpdates: 0,
		},
		{
			name: "extracts failure reason from Stripe payload",
			payment: &payment.Payment{
				ID:              paymentID,
				OrderID:         orderID,
				StripePaymentID: stripeID,
				Status:          payment.StatusProcessing,
			},
			rawPayload:         buildStripeFailedPayload("Your card was declined."),
			wantPaymentUpdates: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ps := newMockWebhookPaymentStore()
			ps.byStripeID[stripeID] = tt.payment
			os := &mockOrderStore{}

			w, _ := newTestWebhookWorker(t, os, ps)

			err := w.handlePaymentFailed(ctx, stripeID, tt.rawPayload)

			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if got := len(ps.statusHistory); got != tt.wantPaymentUpdates {
				t.Errorf("payment status updates = %d, want %d", got, tt.wantPaymentUpdates)
			}
		})
	}
}

func TestWebhookWorker_HandlePaymentFailed_PaymentNotFound(t *testing.T) {
	ps := newMockWebhookPaymentStore()
	// no entry added — GetByStripePaymentID will return ErrNotFound
	os := &mockOrderStore{}

	w, _ := newTestWebhookWorker(t, os, ps)

	err := w.handlePaymentFailed(context.Background(), "pi_unknown", []byte(`{}`))
	if err == nil {
		t.Error("expected error for unknown stripe ID, got nil")
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

var errTest = &testError{"injected test error"}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }

func buildStripeFailedPayload(message string) []byte {
	payload := map[string]any{
		"data": map[string]any{
			"object": map[string]any{
				"last_payment_error": map[string]any{
					"message": message,
				},
			},
		},
	}
	b, _ := json.Marshal(payload)
	return b
}
