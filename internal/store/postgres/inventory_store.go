package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/alexkua/payflow/internal/domain/product"
)

// InventoryStore implements product.InventoryStore against PostgreSQL.
type InventoryStore struct {
	pool *pgxpool.Pool
}

// NewInventoryStore creates an InventoryStore backed by the given connection pool.
func NewInventoryStore(pool *pgxpool.Pool) *InventoryStore {
	return &InventoryStore{pool: pool}
}

// Get returns the current inventory row for a product.
func (s *InventoryStore) Get(ctx context.Context, productID uuid.UUID) (*product.Inventory, error) {
	const q = `
		SELECT product_id, quantity, reserved, version, updated_at
		FROM inventory
		WHERE product_id = $1`

	inv := &product.Inventory{}
	err := s.pool.QueryRow(ctx, q, productID).Scan(
		&inv.ProductID, &inv.Quantity, &inv.Reserved, &inv.Version, &inv.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("inventory_store.Get %s: %w", productID, product.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("inventory_store.Get: %w", err)
	}
	return inv, nil
}

// SetQuantity sets the total stock level for a product.
func (s *InventoryStore) SetQuantity(ctx context.Context, productID uuid.UUID, quantity int) error {
	const q = `
		UPDATE inventory
		SET quantity = $1, version = version + 1, updated_at = NOW()
		WHERE product_id = $2`

	tag, err := s.pool.Exec(ctx, q, quantity, productID)
	if err != nil {
		return fmt.Errorf("inventory_store.SetQuantity: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("inventory_store.SetQuantity %s: %w", productID, product.ErrNotFound)
	}
	return nil
}

// Reserve increments reserved by quantity only if the current version matches expectedVersion.
// Returns true if the row was updated, false if the version was stale (retry signal).
func (s *InventoryStore) Reserve(ctx context.Context, productID uuid.UUID, quantity, expectedVersion int) (bool, error) {
	// The WHERE clause checks both the version (optimistic lock) and that enough stock is
	// available. If either condition fails, 0 rows are affected and the caller retries or
	// surfaces ErrInsufficientStock after re-reading current inventory.
	const q = `
		UPDATE inventory
		SET reserved = reserved + $1, version = version + 1, updated_at = NOW()
		WHERE product_id = $2
		  AND version = $3
		  AND (quantity - reserved) >= $1`

	tag, err := s.pool.Exec(ctx, q, quantity, productID, expectedVersion)
	if err != nil {
		return false, fmt.Errorf("inventory_store.Reserve: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// Release decrements reserved by quantity only if the current version matches expectedVersion.
// Returns true if the row was updated, false if the version was stale.
func (s *InventoryStore) Release(ctx context.Context, productID uuid.UUID, quantity, expectedVersion int) (bool, error) {
	const q = `
		UPDATE inventory
		SET reserved = reserved - $1, version = version + 1, updated_at = NOW()
		WHERE product_id = $2
		  AND version = $3
		  AND reserved >= $1`

	tag, err := s.pool.Exec(ctx, q, quantity, productID, expectedVersion)
	if err != nil {
		return false, fmt.Errorf("inventory_store.Release: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}
