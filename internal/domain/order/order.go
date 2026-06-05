// Package order defines the order domain types, state machine, and persistence interface.
package order

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Status constants represent every state an order can be in.
// Transitions are append-only — recorded in order_events, never by mutating orders.status directly.
const (
	StatusCreated            = "created"
	StatusInventoryReserved  = "inventory_reserved"
	StatusPaymentProcessing  = "payment_processing"
	StatusPaymentCaptured    = "payment_captured"
	StatusConfirmed          = "confirmed"
	StatusFulfilled          = "fulfilled"
	StatusCancelled          = "cancelled"
	StatusRefunded           = "refunded"
)

// EventType constants map to the event_type column in order_events.
const (
	EventCreated           = "created"
	EventInventoryReserved = "inventory_reserved"
	EventInventoryFailed   = "inventory_failed"
	EventPaymentProcessing = "payment_processing"
	EventPaymentCaptured   = "payment_captured"
	EventPaymentFailed     = "payment_failed"
	EventConfirmed         = "confirmed"
	EventFulfilled         = "fulfilled"
	EventCancelled         = "cancelled"
	EventRefunded          = "refunded"
)

// Order represents a customer's purchase request.
type Order struct {
	ID             uuid.UUID   `json:"id"`
	UserID         uuid.UUID   `json:"user_id"`
	Status         string      `json:"status"`
	TotalCents     int         `json:"total_cents"`
	Currency       string      `json:"currency"`
	IdempotencyKey string      `json:"idempotency_key"`
	CreatedAt      time.Time   `json:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at"`
	Items          []OrderItem  `json:"items"`
	Events         []OrderEvent `json:"events"`
}

// OrderItem is a single line in an order — a product at the price snapshotted at order time.
type OrderItem struct {
	ID          uuid.UUID `json:"id"`
	OrderID     uuid.UUID `json:"order_id"`
	ProductID   uuid.UUID `json:"product_id"`
	ProductName string    `json:"product_name"`
	Quantity    int       `json:"quantity"`
	PriceCents  int       `json:"price_cents"`
	CreatedAt   time.Time `json:"created_at"`
}

// OrderEvent is an immutable entry in the append-only event log.
type OrderEvent struct {
	ID        uuid.UUID `json:"id"`
	OrderID   uuid.UUID `json:"order_id"`
	EventType string    `json:"event_type"`
	Payload   []byte    `json:"payload"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateOrderRequest carries the fields submitted by the client to create an order.
type CreateOrderRequest struct {
	IdempotencyKey string
	UserID         uuid.UUID
	Items          []CreateOrderItem
}

// CreateOrderItem is one line item in a CreateOrderRequest.
type CreateOrderItem struct {
	ProductID  uuid.UUID `json:"product_id"`
	Quantity   int       `json:"quantity"`
}

// Store is the persistence interface for orders.
type Store interface {
	// Create inserts an order and its items in one transaction and appends a "created" event.
	Create(ctx context.Context, req CreateOrderRequest, totalCents int, items []OrderItem) (*Order, error)

	// GetByID returns an order with its items and full event history.
	GetByID(ctx context.Context, id uuid.UUID) (*Order, error)

	// GetByIdempotencyKey returns an existing order matching the key, or ErrNotFound.
	GetByIdempotencyKey(ctx context.Context, key string) (*Order, error)

	// ListByUserID returns all orders for a user, newest first.
	ListByUserID(ctx context.Context, userID uuid.UUID) ([]*Order, error)

	// UpdateStatus sets the order's status column and appends an event.
	UpdateStatus(ctx context.Context, orderID uuid.UUID, status, eventType, createdBy string, payload []byte) error

	// Cancel transitions an order to cancelled if it is in a cancellable state.
	Cancel(ctx context.Context, orderID uuid.UUID, createdBy string) error
}

// CancellableStatuses lists the states from which an order can be cancelled by the customer.
var CancellableStatuses = map[string]bool{
	StatusCreated:           true,
	StatusInventoryReserved: true,
}

// ErrNotFound is returned when an order does not exist.
var ErrNotFound = errors.New("order not found")

// ErrNotCancellable is returned when cancellation is attempted on an order past the point of no return.
var ErrNotCancellable = errors.New("order cannot be cancelled in its current state")

// ErrDuplicateIdempotencyKey signals that this request was already processed.
var ErrDuplicateIdempotencyKey = errors.New("duplicate idempotency key")
