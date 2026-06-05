package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/alexkua/payflow/internal/domain/payment"
)

// PaymentStore implements payment.Store using PostgreSQL.
type PaymentStore struct {
	pool *pgxpool.Pool
}

// NewPaymentStore creates a PaymentStore backed by the given connection pool.
func NewPaymentStore(pool *pgxpool.Pool) *PaymentStore {
	return &PaymentStore{pool: pool}
}

// scanner is satisfied by both pgx.Row (QueryRow) and pgx.Rows (Query loop),
// letting the two scan helpers work for single-row and multi-row queries.
type scanner interface {
	Scan(dest ...any) error
}

// scanPayment scans a payment row. stripe_payment_id and failure_reason are
// nullable in the DB, so we scan into *string and convert after.
func scanPayment(row scanner, p *payment.Payment) error {
	var stripePaymentID, failureReason *string
	err := row.Scan(
		&p.ID, &p.OrderID, &stripePaymentID, &p.AmountCents, &p.Currency,
		&p.Status, &p.IdempotencyKey, &failureReason, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return err
	}
	if stripePaymentID != nil {
		p.StripePaymentID = *stripePaymentID
	}
	if failureReason != nil {
		p.FailureReason = *failureReason
	}
	return nil
}

// scanRefund scans a refund row. reason and stripe_refund_id are nullable.
func scanRefund(row scanner, r *payment.Refund) error {
	var reason, stripeRefundID *string
	err := row.Scan(
		&r.ID, &r.PaymentID, &r.AmountCents, &reason,
		&stripeRefundID, &r.Status, &r.IdempotencyKey, &r.CreatedAt,
	)
	if err != nil {
		return err
	}
	if reason != nil {
		r.Reason = *reason
	}
	if stripeRefundID != nil {
		r.StripeRefundID = *stripeRefundID
	}
	return nil
}

func (s *PaymentStore) Create(ctx context.Context, orderID uuid.UUID, amountCents int, currency, idempotencyKey string) (*payment.Payment, error) {
	const q = `
		INSERT INTO payments (order_id, amount_cents, currency, status, idempotency_key)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, order_id, stripe_payment_id, amount_cents, currency, status,
		          idempotency_key, failure_reason, created_at, updated_at`

	p := &payment.Payment{}
	if err := scanPayment(s.pool.QueryRow(ctx, q, orderID, amountCents, currency, payment.StatusPending, idempotencyKey), p); err != nil {
		return nil, fmt.Errorf("payment_store.Create: %w", err)
	}
	return p, nil
}

func (s *PaymentStore) GetByID(ctx context.Context, id uuid.UUID) (*payment.Payment, error) {
	const q = `
		SELECT id, order_id, stripe_payment_id, amount_cents, currency, status,
		       idempotency_key, failure_reason, created_at, updated_at
		FROM payments WHERE id = $1`

	p := &payment.Payment{}
	if err := scanPayment(s.pool.QueryRow(ctx, q, id), p); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, payment.ErrNotFound
		}
		return nil, fmt.Errorf("payment_store.GetByID: %w", err)
	}

	events, err := s.loadEvents(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	p.Events = events
	return p, nil
}

func (s *PaymentStore) GetByOrderID(ctx context.Context, orderID uuid.UUID) (*payment.Payment, error) {
	const q = `
		SELECT id, order_id, stripe_payment_id, amount_cents, currency, status,
		       idempotency_key, failure_reason, created_at, updated_at
		FROM payments WHERE order_id = $1`

	p := &payment.Payment{}
	if err := scanPayment(s.pool.QueryRow(ctx, q, orderID), p); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, payment.ErrNotFound
		}
		return nil, fmt.Errorf("payment_store.GetByOrderID: %w", err)
	}
	return p, nil
}

func (s *PaymentStore) GetByStripePaymentID(ctx context.Context, stripePaymentID string) (*payment.Payment, error) {
	const q = `
		SELECT id, order_id, stripe_payment_id, amount_cents, currency, status,
		       idempotency_key, failure_reason, created_at, updated_at
		FROM payments WHERE stripe_payment_id = $1`

	p := &payment.Payment{}
	if err := scanPayment(s.pool.QueryRow(ctx, q, stripePaymentID), p); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, payment.ErrNotFound
		}
		return nil, fmt.Errorf("payment_store.GetByStripePaymentID: %w", err)
	}
	return p, nil
}

func (s *PaymentStore) GetByIdempotencyKey(ctx context.Context, key string) (*payment.Payment, error) {
	const q = `
		SELECT id, order_id, stripe_payment_id, amount_cents, currency, status,
		       idempotency_key, failure_reason, created_at, updated_at
		FROM payments WHERE idempotency_key = $1`

	p := &payment.Payment{}
	if err := scanPayment(s.pool.QueryRow(ctx, q, key), p); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, payment.ErrNotFound
		}
		return nil, fmt.Errorf("payment_store.GetByIdempotencyKey: %w", err)
	}
	return p, nil
}

func (s *PaymentStore) UpdateStatus(ctx context.Context, id uuid.UUID, status, stripePaymentID, failureReason, eventType string, rawPayload []byte) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("payment_store.UpdateStatus begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	const updateQ = `
		UPDATE payments
		SET status            = $1,
		    stripe_payment_id = CASE WHEN $2 != '' THEN $2 ELSE stripe_payment_id END,
		    failure_reason    = CASE WHEN $3 != '' THEN $3 ELSE failure_reason END,
		    updated_at        = NOW()
		WHERE id = $4`

	if _, err := tx.Exec(ctx, updateQ, status, stripePaymentID, failureReason, id); err != nil {
		return fmt.Errorf("payment_store.UpdateStatus update: %w", err)
	}

	const eventQ = `
		INSERT INTO payment_events (payment_id, event_type, provider, raw_payload)
		VALUES ($1, $2, 'stripe', $3)`

	if _, err := tx.Exec(ctx, eventQ, id, eventType, rawPayload); err != nil {
		return fmt.Errorf("payment_store.UpdateStatus event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("payment_store.UpdateStatus commit: %w", err)
	}
	return nil
}

func (s *PaymentStore) IsWebhookProcessed(ctx context.Context, eventID string) (bool, error) {
	const q = `SELECT 1 FROM processed_webhook_events WHERE event_id = $1`
	var dummy int
	err := s.pool.QueryRow(ctx, q, eventID).Scan(&dummy)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("payment_store.IsWebhookProcessed: %w", err)
	}
	return true, nil
}

func (s *PaymentStore) MarkWebhookProcessed(ctx context.Context, eventID, eventType string) error {
	const q = `
		INSERT INTO processed_webhook_events (event_id, event_type)
		VALUES ($1, $2)
		ON CONFLICT (event_id) DO NOTHING`

	if _, err := s.pool.Exec(ctx, q, eventID, eventType); err != nil {
		return fmt.Errorf("payment_store.MarkWebhookProcessed: %w", err)
	}
	return nil
}

func (s *PaymentStore) CreateRefund(ctx context.Context, paymentID uuid.UUID, amountCents int, reason, idempotencyKey string) (*payment.Refund, error) {
	const q = `
		INSERT INTO refunds (payment_id, amount_cents, reason, status, idempotency_key)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, payment_id, amount_cents, reason, stripe_refund_id, status, idempotency_key, created_at`

	r := &payment.Refund{}
	if err := scanRefund(s.pool.QueryRow(ctx, q, paymentID, amountCents, reason, payment.RefundStatusPending, idempotencyKey), r); err != nil {
		return nil, fmt.Errorf("payment_store.CreateRefund: %w", err)
	}
	return r, nil
}

func (s *PaymentStore) UpdateRefund(ctx context.Context, refundID uuid.UUID, stripeRefundID, status string) error {
	const q = `UPDATE refunds SET stripe_refund_id = $1, status = $2 WHERE id = $3`
	if _, err := s.pool.Exec(ctx, q, stripeRefundID, status, refundID); err != nil {
		return fmt.Errorf("payment_store.UpdateRefund: %w", err)
	}
	return nil
}

func (s *PaymentStore) GetRefundsByPaymentID(ctx context.Context, paymentID uuid.UUID) ([]*payment.Refund, error) {
	const q = `
		SELECT id, payment_id, amount_cents, reason, stripe_refund_id, status, idempotency_key, created_at
		FROM refunds WHERE payment_id = $1 ORDER BY created_at ASC`

	rows, err := s.pool.Query(ctx, q, paymentID)
	if err != nil {
		return nil, fmt.Errorf("payment_store.GetRefundsByPaymentID: %w", err)
	}
	defer rows.Close()

	var refunds []*payment.Refund
	for rows.Next() {
		r := &payment.Refund{}
		if err := scanRefund(rows, r); err != nil {
			return nil, fmt.Errorf("payment_store.GetRefundsByPaymentID scan: %w", err)
		}
		refunds = append(refunds, r)
	}
	return refunds, rows.Err()
}

func (s *PaymentStore) GetRefundByID(ctx context.Context, id uuid.UUID) (*payment.Refund, error) {
	const q = `
		SELECT id, payment_id, amount_cents, reason, stripe_refund_id, status, idempotency_key, created_at
		FROM refunds WHERE id = $1`

	r := &payment.Refund{}
	if err := scanRefund(s.pool.QueryRow(ctx, q, id), r); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, payment.ErrNotFound
		}
		return nil, fmt.Errorf("payment_store.GetRefundByID: %w", err)
	}
	return r, nil
}

func (s *PaymentStore) GetRefundByIdempotencyKey(ctx context.Context, key string) (*payment.Refund, error) {
	const q = `
		SELECT id, payment_id, amount_cents, reason, stripe_refund_id, status, idempotency_key, created_at
		FROM refunds WHERE idempotency_key = $1`

	r := &payment.Refund{}
	if err := scanRefund(s.pool.QueryRow(ctx, q, key), r); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, payment.ErrNotFound
		}
		return nil, fmt.Errorf("payment_store.GetRefundByIdempotencyKey: %w", err)
	}
	return r, nil
}

func (s *PaymentStore) loadEvents(ctx context.Context, paymentID uuid.UUID) ([]payment.PaymentEvent, error) {
	const q = `
		SELECT id, payment_id, event_type, provider, raw_payload, created_at
		FROM payment_events WHERE payment_id = $1 ORDER BY created_at ASC`

	rows, err := s.pool.Query(ctx, q, paymentID)
	if err != nil {
		return nil, fmt.Errorf("payment_store.loadEvents: %w", err)
	}
	defer rows.Close()

	var events []payment.PaymentEvent
	for rows.Next() {
		e := payment.PaymentEvent{}
		if err := rows.Scan(&e.ID, &e.PaymentID, &e.EventType, &e.Provider, &e.RawPayload, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("payment_store.loadEvents scan: %w", err)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}
