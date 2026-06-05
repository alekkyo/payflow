// Package payment defines the payment domain types, state machine, and persistence interface.
package payment

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Status constants for the payments table.
const (
	StatusPending    = "pending"
	StatusProcessing = "processing"
	StatusCaptured   = "captured"
	StatusFailed     = "failed"
	StatusRefunded   = "refunded"
)

// EventType constants for the payment_events audit log.
const (
	EventInitiated       = "initiated"
	EventProcessing      = "processing"
	EventCaptured        = "captured"
	EventFailed          = "failed"
	EventRefundRequested = "refund_requested"
	EventRefunded        = "refunded"
)

// RefundStatus constants for the refunds table.
const (
	RefundStatusPending   = "pending"
	RefundStatusSucceeded = "succeeded"
	RefundStatusFailed    = "failed"
)

// Payment represents a Stripe charge attempt for an order.
type Payment struct {
	ID              uuid.UUID      `json:"id"`
	OrderID         uuid.UUID      `json:"order_id"`
	StripePaymentID string         `json:"stripe_payment_id,omitempty"`
	AmountCents     int            `json:"amount_cents"`
	Currency        string         `json:"currency"`
	Status          string         `json:"status"`
	IdempotencyKey  string         `json:"idempotency_key"`
	FailureReason   string         `json:"failure_reason,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	Events          []PaymentEvent `json:"events,omitempty"`
}

// PaymentEvent is an immutable entry in the append-only payment audit log.
type PaymentEvent struct {
	ID         uuid.UUID `json:"id"`
	PaymentID  uuid.UUID `json:"payment_id"`
	EventType  string    `json:"event_type"`
	Provider   string    `json:"provider"`
	RawPayload []byte    `json:"raw_payload,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// Refund represents a Stripe refund for a payment.
type Refund struct {
	ID             uuid.UUID `json:"id"`
	PaymentID      uuid.UUID `json:"payment_id"`
	AmountCents    int       `json:"amount_cents"`
	Reason         string    `json:"reason,omitempty"`
	StripeRefundID string    `json:"stripe_refund_id,omitempty"`
	Status         string    `json:"status"`
	IdempotencyKey string    `json:"idempotency_key"`
	CreatedAt      time.Time `json:"created_at"`
}

// Store is the persistence interface for payments.
type Store interface {
	// Create inserts a new payment row in "pending" status.
	Create(ctx context.Context, orderID uuid.UUID, amountCents int, currency, idempotencyKey string) (*Payment, error)

	// GetByID returns a payment with its full event history.
	GetByID(ctx context.Context, id uuid.UUID) (*Payment, error)

	// GetByOrderID returns the payment for an order, or ErrNotFound.
	GetByOrderID(ctx context.Context, orderID uuid.UUID) (*Payment, error)

	// GetByIdempotencyKey returns an existing payment by its idempotency key.
	GetByIdempotencyKey(ctx context.Context, key string) (*Payment, error)

	// GetByStripePaymentID returns the payment matching the Stripe PaymentIntent ID.
	GetByStripePaymentID(ctx context.Context, stripePaymentID string) (*Payment, error)

	// UpdateStatus sets the payment status, optionally updating the Stripe ID and failure reason,
	// and appends an event to the payment audit log.
	UpdateStatus(ctx context.Context, id uuid.UUID, status, stripePaymentID, failureReason, eventType string, rawPayload []byte) error

	// IsWebhookProcessed returns true if the Stripe event ID has already been handled.
	IsWebhookProcessed(ctx context.Context, eventID string) (bool, error)

	// MarkWebhookProcessed inserts the Stripe event ID into processed_webhook_events.
	MarkWebhookProcessed(ctx context.Context, eventID, eventType string) error

	// CreateRefund inserts a new refund row in "pending" status.
	CreateRefund(ctx context.Context, paymentID uuid.UUID, amountCents int, reason, idempotencyKey string) (*Refund, error)

	// UpdateRefund sets the refund's Stripe refund ID and status.
	UpdateRefund(ctx context.Context, refundID uuid.UUID, stripeRefundID, status string) error

	// GetRefundsByPaymentID returns all refunds for a payment.
	GetRefundsByPaymentID(ctx context.Context, paymentID uuid.UUID) ([]*Refund, error)

	// GetRefundByID returns a refund by its primary key, or ErrNotFound.
	GetRefundByID(ctx context.Context, id uuid.UUID) (*Refund, error)

	// GetRefundByIdempotencyKey returns a refund by its idempotency key, or ErrNotFound.
	GetRefundByIdempotencyKey(ctx context.Context, key string) (*Refund, error)

	// ListPaymentsByDateRange returns all payments created within [from, to].
	// Used by the reconciliation worker to compare against Stripe's records.
	ListPaymentsByDateRange(ctx context.Context, from, to time.Time) ([]*Payment, error)
}

// PaymentProvider is the interface for payment processing providers.
// Stripe implements this; a mock can implement it for tests.
type PaymentProvider interface {
	CreatePaymentIntent(ctx context.Context, req PaymentIntentRequest) (*PaymentIntentResult, error)
	CreateRefund(ctx context.Context, req RefundRequest) (*RefundResult, error)
	ConstructWebhookEvent(payload []byte, signature string) (*WebhookEvent, error)

	// ListPaymentIntents fetches all payment intents created within [from, to].
	// Used by the reconciliation worker to compare against our local records.
	ListPaymentIntents(ctx context.Context, from, to time.Time) ([]PaymentIntentSummary, error)
}

// PaymentIntentSummary holds the fields we compare during reconciliation.
type PaymentIntentSummary struct {
	StripeID    string
	Status      string
	AmountCents int
	Currency    string
	OrderID     string // extracted from Stripe metadata["order_id"]
}

// PaymentIntentRequest carries the data needed to create a Stripe PaymentIntent.
type PaymentIntentRequest struct {
	OrderID        uuid.UUID
	AmountCents    int
	Currency       string
	IdempotencyKey string
}

// PaymentIntentResult holds the fields returned from a successful PaymentIntent creation.
type PaymentIntentResult struct {
	StripeID     string
	Status       string
	ClientSecret string
}

// RefundRequest carries the data needed to issue a Stripe refund.
type RefundRequest struct {
	StripePaymentID string
	AmountCents     int
	IdempotencyKey  string
}

// RefundResult holds the fields returned from a successful refund.
type RefundResult struct {
	StripeRefundID string
	Status         string
}

// WebhookEvent is a validated, parsed Stripe webhook event.
type WebhookEvent struct {
	ID      string
	Type    string
	Payload []byte // raw JSON of the event, stored for audit
}

// ErrNotFound is returned when a payment or refund does not exist.
var ErrNotFound = errors.New("payment not found")

// ErrAlreadyProcessed is returned when a webhook event has already been handled.
var ErrAlreadyProcessed = errors.New("webhook event already processed")
