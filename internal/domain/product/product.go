// Package product defines the product and inventory domain types and persistence interfaces.
package product

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Product represents a sellable item in the catalog.
type Product struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	PriceCents  int       `json:"price_cents"`
	Currency    string    `json:"currency"`
	Active      bool      `json:"active"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Inventory tracks stock levels for a product.
type Inventory struct {
	ProductID uuid.UUID `json:"product_id"`
	Quantity  int       `json:"quantity"`
	Reserved  int       `json:"reserved"`
	Version   int       `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Available returns the number of units that can still be reserved.
func (i *Inventory) Available() int {
	return i.Quantity - i.Reserved
}

// CreateProductRequest carries the fields required to create a new product.
type CreateProductRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	PriceCents  int    `json:"price_cents"`
	Currency    string `json:"currency"`
}

// UpdateProductRequest carries the fields that can be changed on an existing product.
type UpdateProductRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	PriceCents  int    `json:"price_cents"`
	Active      bool   `json:"active"`
}

// Store is the persistence interface for products.
type Store interface {
	// Create inserts a product and its inventory row (quantity=0) in one transaction.
	Create(ctx context.Context, req CreateProductRequest) (*Product, error)

	// GetByID returns a product by primary key, or an error wrapping ErrNotFound.
	GetByID(ctx context.Context, id uuid.UUID) (*Product, error)

	// List returns a paginated slice of active products and the total active count.
	List(ctx context.Context, page, pageSize int) ([]*Product, int, error)

	// Update modifies a product's fields and returns the updated record.
	Update(ctx context.Context, id uuid.UUID, req UpdateProductRequest) (*Product, error)

	// Deactivate soft-deletes a product by setting active=false.
	Deactivate(ctx context.Context, id uuid.UUID) error
}

// InventoryStore is the persistence interface for inventory operations.
type InventoryStore interface {
	// Get returns the current inventory for a product.
	Get(ctx context.Context, productID uuid.UUID) (*Inventory, error)

	// SetQuantity sets the total stock quantity for a product (admin operation).
	SetQuantity(ctx context.Context, productID uuid.UUID, quantity int) error

	// Reserve attempts to increment reserved by quantity using optimistic locking.
	// Returns true if the update succeeded, false if the version was stale (caller should retry).
	Reserve(ctx context.Context, productID uuid.UUID, quantity, expectedVersion int) (bool, error)

	// Release decrements reserved by quantity using optimistic locking.
	// Returns true if the update succeeded, false if the version was stale.
	Release(ctx context.Context, productID uuid.UUID, quantity, expectedVersion int) (bool, error)
}

// ErrNotFound is returned when a product or inventory record does not exist.
var ErrNotFound = errors.New("record not found")

// ErrInsufficientStock is returned when a reservation exceeds available units.
var ErrInsufficientStock = errors.New("insufficient stock")

// ErrVersionConflict is returned when optimistic locking retries are exhausted.
var ErrVersionConflict = errors.New("inventory version conflict")

// InventoryService wraps InventoryStore and implements optimistic locking retry logic.
type InventoryService struct {
	store InventoryStore
}

// NewInventoryService creates an InventoryService backed by the given store.
func NewInventoryService(store InventoryStore) *InventoryService {
	return &InventoryService{store: store}
}

// Reserve reserves quantity units for productID, retrying on version conflicts.
func (s *InventoryService) Reserve(ctx context.Context, productID uuid.UUID, quantity int) error {
	const maxAttempts = 3

	for attempt := 0; attempt < maxAttempts; attempt++ {
		inv, err := s.store.Get(ctx, productID)
		if err != nil {
			return fmt.Errorf("inventory_service.Reserve get: %w", err)
		}

		if inv.Available() < quantity {
			return ErrInsufficientStock
		}

		ok, err := s.store.Reserve(ctx, productID, quantity, inv.Version)
		if err != nil {
			return fmt.Errorf("inventory_service.Reserve update: %w", err)
		}
		if ok {
			return nil
		}
		// Version was stale — another process updated first, retry.
	}

	return fmt.Errorf("inventory_service.Reserve: %w after %d attempts", ErrVersionConflict, maxAttempts)
}

// Release returns quantity reserved units back to available, retrying on version conflicts.
func (s *InventoryService) Release(ctx context.Context, productID uuid.UUID, quantity int) error {
	const maxAttempts = 3

	for attempt := 0; attempt < maxAttempts; attempt++ {
		inv, err := s.store.Get(ctx, productID)
		if err != nil {
			return fmt.Errorf("inventory_service.Release get: %w", err)
		}

		ok, err := s.store.Release(ctx, productID, quantity, inv.Version)
		if err != nil {
			return fmt.Errorf("inventory_service.Release update: %w", err)
		}
		if ok {
			return nil
		}
	}

	return fmt.Errorf("inventory_service.Release: %w after %d attempts", ErrVersionConflict, maxAttempts)
}
