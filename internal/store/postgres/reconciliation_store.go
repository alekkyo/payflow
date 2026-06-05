package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/alexkua/payflow/internal/domain/reconciliation"
)

// ReconciliationStore implements reconciliation.Store using PostgreSQL.
type ReconciliationStore struct {
	pool *pgxpool.Pool
}

// NewReconciliationStore creates a ReconciliationStore backed by the given connection pool.
func NewReconciliationStore(pool *pgxpool.Pool) *ReconciliationStore {
	return &ReconciliationStore{pool: pool}
}

func scanRun(row scanner, run *reconciliation.Run) error {
	return row.Scan(
		&run.ID, &run.RunDate, &run.Status,
		&run.Matched, &run.Mismatched, &run.MissingLocal, &run.MissingStripe,
		&run.StartedAt, &run.CompletedAt,
	)
}

func (s *ReconciliationStore) CreateRun(ctx context.Context, runDate time.Time) (*reconciliation.Run, error) {
	const q = `
		INSERT INTO reconciliation_runs (run_date, status)
		VALUES ($1, $2)
		ON CONFLICT (run_date) DO UPDATE
		    SET status = EXCLUDED.status, started_at = NOW(), completed_at = NULL
		RETURNING id, run_date, status, matched, mismatched, missing_local, missing_stripe, started_at, completed_at`

	run := &reconciliation.Run{}
	if err := scanRun(s.pool.QueryRow(ctx, q, runDate.Format("2006-01-02"), reconciliation.StatusRunning), run); err != nil {
		return nil, fmt.Errorf("reconciliation_store.CreateRun: %w", err)
	}
	return run, nil
}

func (s *ReconciliationStore) CompleteRun(ctx context.Context, id uuid.UUID, matched, mismatched, missingLocal, missingStripe int) error {
	const q = `
		UPDATE reconciliation_runs
		SET status = $1, matched = $2, mismatched = $3,
		    missing_local = $4, missing_stripe = $5, completed_at = NOW()
		WHERE id = $6`

	if _, err := s.pool.Exec(ctx, q, reconciliation.StatusCompleted, matched, mismatched, missingLocal, missingStripe, id); err != nil {
		return fmt.Errorf("reconciliation_store.CompleteRun: %w", err)
	}
	return nil
}

func (s *ReconciliationStore) FailRun(ctx context.Context, id uuid.UUID) error {
	const q = `UPDATE reconciliation_runs SET status = $1, completed_at = NOW() WHERE id = $2`
	if _, err := s.pool.Exec(ctx, q, reconciliation.StatusFailed, id); err != nil {
		return fmt.Errorf("reconciliation_store.FailRun: %w", err)
	}
	return nil
}

func (s *ReconciliationStore) AddDiscrepancy(ctx context.Context, runID uuid.UUID, d reconciliation.Discrepancy) error {
	const q = `
		INSERT INTO reconciliation_discrepancies
		    (reconciliation_id, payment_id, stripe_payment_id, discrepancy_type,
		     our_amount_cents, stripe_amount_cents, our_status, stripe_status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

	var paymentID interface{} = nil
	if d.PaymentID != nil {
		paymentID = *d.PaymentID
	}

	if _, err := s.pool.Exec(ctx, q,
		runID, paymentID, d.StripePaymentID, d.Type,
		d.OurAmountCents, d.StripeAmountCents, d.OurStatus, d.StripeStatus,
	); err != nil {
		return fmt.Errorf("reconciliation_store.AddDiscrepancy: %w", err)
	}
	return nil
}

func (s *ReconciliationStore) GetLatestRun(ctx context.Context) (*reconciliation.Run, error) {
	const q = `
		SELECT id, run_date, status, matched, mismatched, missing_local, missing_stripe, started_at, completed_at
		FROM reconciliation_runs
		ORDER BY started_at DESC LIMIT 1`

	run := &reconciliation.Run{}
	if err := scanRun(s.pool.QueryRow(ctx, q), run); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("reconciliation_store.GetLatestRun: %w", err)
	}
	return run, nil
}

func (s *ReconciliationStore) ListRuns(ctx context.Context, limit int) ([]*reconciliation.Run, error) {
	const q = `
		SELECT id, run_date, status, matched, mismatched, missing_local, missing_stripe, started_at, completed_at
		FROM reconciliation_runs
		ORDER BY started_at DESC
		LIMIT $1`

	rows, err := s.pool.Query(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("reconciliation_store.ListRuns: %w", err)
	}
	defer rows.Close()

	var runs []*reconciliation.Run
	for rows.Next() {
		run := &reconciliation.Run{}
		if err := scanRun(rows, run); err != nil {
			return nil, fmt.Errorf("reconciliation_store.ListRuns scan: %w", err)
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func (s *ReconciliationStore) GetRunWithDiscrepancies(ctx context.Context, id uuid.UUID) (*reconciliation.Run, error) {
	const runQ = `
		SELECT id, run_date, status, matched, mismatched, missing_local, missing_stripe, started_at, completed_at
		FROM reconciliation_runs WHERE id = $1`

	run := &reconciliation.Run{}
	if err := scanRun(s.pool.QueryRow(ctx, runQ, id), run); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("reconciliation_store.GetRunWithDiscrepancies: %w", err)
	}

	const discQ = `
		SELECT id, reconciliation_id, payment_id, stripe_payment_id, discrepancy_type,
		       our_amount_cents, stripe_amount_cents, our_status, stripe_status, created_at
		FROM reconciliation_discrepancies
		WHERE reconciliation_id = $1
		ORDER BY created_at ASC`

	rows, err := s.pool.Query(ctx, discQ, id)
	if err != nil {
		return nil, fmt.Errorf("reconciliation_store.GetRunWithDiscrepancies discrepancies: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		d := reconciliation.Discrepancy{}
		var paymentID *uuid.UUID
		if err := rows.Scan(
			&d.ID, &d.ReconciliationID, &paymentID, &d.StripePaymentID, &d.Type,
			&d.OurAmountCents, &d.StripeAmountCents, &d.OurStatus, &d.StripeStatus, &d.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("reconciliation_store.GetRunWithDiscrepancies scan: %w", err)
		}
		d.PaymentID = paymentID
		run.Discrepancies = append(run.Discrepancies, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reconciliation_store.GetRunWithDiscrepancies rows: %w", err)
	}
	return run, nil
}
