// Package reconciliation defines types for the daily payment reconciliation process.
// Reconciliation compares our local payment records against the Stripe API to detect
// discrepancies — missed webhooks, amount differences, status drift, or gaps.
package reconciliation

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Run status constants.
const (
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
)

// Discrepancy type constants — each represents a different class of data integrity problem.
const (
	// DiscrepancyMissingLocal — Stripe has a PaymentIntent we have no record of.
	// The most serious type: money may have left a customer's account without our knowledge.
	DiscrepancyMissingLocal = "missing_local"

	// DiscrepancyMissingStripe — we have a payment record but Stripe has no matching intent.
	// Could mean a logic bug created a DB row without ever calling Stripe.
	DiscrepancyMissingStripe = "missing_stripe"

	// DiscrepancyAmountMismatch — same intent exists in both, but the amounts differ.
	DiscrepancyAmountMismatch = "amount_mismatch"

	// DiscrepancyStatusMismatch — a webhook was missed; our status is stale.
	// e.g. Stripe says "succeeded" but our DB still says "processing".
	DiscrepancyStatusMismatch = "status_mismatch"
)

// Run represents one execution of the reconciliation process for a given date.
type Run struct {
	ID            uuid.UUID    `json:"id"`
	RunDate       time.Time    `json:"run_date"`
	Status        string       `json:"status"`
	Matched       int          `json:"matched"`
	Mismatched    int          `json:"mismatched"`
	MissingLocal  int          `json:"missing_local"`
	MissingStripe int          `json:"missing_stripe"`
	StartedAt     time.Time    `json:"started_at"`
	CompletedAt   *time.Time   `json:"completed_at,omitempty"`
	Discrepancies []Discrepancy `json:"discrepancies,omitempty"`
}

// Discrepancy records a single payment that did not match between our DB and Stripe.
type Discrepancy struct {
	ID                uuid.UUID  `json:"id"`
	ReconciliationID  uuid.UUID  `json:"reconciliation_id"`
	PaymentID         *uuid.UUID `json:"payment_id,omitempty"`
	StripePaymentID   string     `json:"stripe_payment_id,omitempty"`
	Type              string     `json:"discrepancy_type"`
	OurAmountCents    int        `json:"our_amount_cents,omitempty"`
	StripeAmountCents int        `json:"stripe_amount_cents,omitempty"`
	OurStatus         string     `json:"our_status,omitempty"`
	StripeStatus      string     `json:"stripe_status,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
}

// Store is the persistence interface for reconciliation runs and their discrepancies.
type Store interface {
	// CreateRun inserts a new reconciliation run in "running" status.
	CreateRun(ctx context.Context, runDate time.Time) (*Run, error)

	// CompleteRun marks a run as completed and records its summary counters.
	CompleteRun(ctx context.Context, id uuid.UUID, matched, mismatched, missingLocal, missingStripe int) error

	// FailRun marks a run as failed (e.g. due to a Stripe API error).
	FailRun(ctx context.Context, id uuid.UUID) error

	// AddDiscrepancy saves one discrepancy found during a reconciliation run.
	AddDiscrepancy(ctx context.Context, runID uuid.UUID, d Discrepancy) error

	// GetLatestRun returns the most recently started run.
	GetLatestRun(ctx context.Context) (*Run, error)

	// ListRuns returns the most recent runs in descending order.
	ListRuns(ctx context.Context, limit int) ([]*Run, error)

	// GetRunWithDiscrepancies returns a run and all its discrepancy records.
	GetRunWithDiscrepancies(ctx context.Context, id uuid.UUID) (*Run, error)
}
