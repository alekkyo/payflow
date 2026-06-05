package product_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/alexkua/payflow/internal/domain/product"
)

// mockInventoryStore is an in-memory InventoryStore for testing.
type mockInventoryStore struct {
	inv       *product.Inventory
	reserveOK bool // controls whether Reserve returns true (success) or false (version conflict)
	releaseOK bool
	err       error
}

func (m *mockInventoryStore) Get(_ context.Context, _ uuid.UUID) (*product.Inventory, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.inv, nil
}

func (m *mockInventoryStore) SetQuantity(_ context.Context, _ uuid.UUID, _ int) error {
	return m.err
}

func (m *mockInventoryStore) Reserve(_ context.Context, _ uuid.UUID, _ int, _ int) (bool, error) {
	if m.err != nil {
		return false, m.err
	}
	return m.reserveOK, nil
}

func (m *mockInventoryStore) Release(_ context.Context, _ uuid.UUID, _ int, _ int) (bool, error) {
	if m.err != nil {
		return false, m.err
	}
	return m.releaseOK, nil
}

func (m *mockInventoryStore) ListAvailable(_ context.Context) (map[uuid.UUID]int, error) {
	return nil, m.err
}

func TestInventory_Available(t *testing.T) {
	tests := []struct {
		name     string
		inv      product.Inventory
		expected int
	}{
		{
			name:     "fully available",
			inv:      product.Inventory{Quantity: 10, Reserved: 0},
			expected: 10,
		},
		{
			name:     "partially reserved",
			inv:      product.Inventory{Quantity: 10, Reserved: 3},
			expected: 7,
		},
		{
			name:     "fully reserved",
			inv:      product.Inventory{Quantity: 5, Reserved: 5},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.inv.Available()
			if got != tt.expected {
				t.Errorf("Available() = %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestInventoryService_Reserve(t *testing.T) {
	productID := uuid.New()
	ctx := context.Background()

	tests := []struct {
		name      string
		store     *mockInventoryStore
		quantity  int
		wantErr   error
	}{
		{
			name: "success",
			store: &mockInventoryStore{
				inv:       &product.Inventory{ProductID: productID, Quantity: 10, Reserved: 2, Version: 1},
				reserveOK: true,
			},
			quantity: 5,
			wantErr:  nil,
		},
		{
			name: "insufficient stock",
			store: &mockInventoryStore{
				inv: &product.Inventory{ProductID: productID, Quantity: 3, Reserved: 0, Version: 1},
			},
			quantity: 5,
			wantErr:  product.ErrInsufficientStock,
		},
		{
			name: "exactly available quantity succeeds",
			store: &mockInventoryStore{
				inv:       &product.Inventory{ProductID: productID, Quantity: 5, Reserved: 0, Version: 1},
				reserveOK: true,
			},
			quantity: 5,
			wantErr:  nil,
		},
		{
			name: "version conflict exhausts retries",
			store: &mockInventoryStore{
				inv:       &product.Inventory{ProductID: productID, Quantity: 10, Reserved: 0, Version: 1},
				reserveOK: false, // always returns false — simulates perpetual version conflict
			},
			quantity: 1,
			wantErr:  product.ErrVersionConflict,
		},
		{
			name: "store error on get propagates",
			store: &mockInventoryStore{
				err: errors.New("postgres unavailable"),
			},
			quantity: 1,
			wantErr:  nil, // checked via wantErrStr below
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := product.NewInventoryService(tt.store)
			err := svc.Reserve(ctx, productID, tt.quantity)

			if tt.name == "store error on get propagates" {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("Reserve() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Errorf("Reserve() unexpected error: %v", err)
			}
		})
	}
}

func TestInventoryService_Release(t *testing.T) {
	productID := uuid.New()
	ctx := context.Background()

	tests := []struct {
		name     string
		store    *mockInventoryStore
		quantity int
		wantErr  error
	}{
		{
			name: "success",
			store: &mockInventoryStore{
				inv:       &product.Inventory{ProductID: productID, Quantity: 10, Reserved: 5, Version: 2},
				releaseOK: true,
			},
			quantity: 3,
			wantErr:  nil,
		},
		{
			name: "version conflict exhausts retries",
			store: &mockInventoryStore{
				inv:       &product.Inventory{ProductID: productID, Quantity: 10, Reserved: 5, Version: 2},
				releaseOK: false,
			},
			quantity: 3,
			wantErr:  product.ErrVersionConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := product.NewInventoryService(tt.store)
			err := svc.Release(ctx, productID, tt.quantity)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("Release() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Errorf("Release() unexpected error: %v", err)
			}
		})
	}
}
