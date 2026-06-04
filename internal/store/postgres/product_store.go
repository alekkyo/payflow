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

// ProductStore implements product.Store against PostgreSQL.
type ProductStore struct {
	pool *pgxpool.Pool
}

// NewProductStore creates a ProductStore backed by the given connection pool.
func NewProductStore(pool *pgxpool.Pool) *ProductStore {
	return &ProductStore{pool: pool}
}

// Create inserts a product and its inventory row in a single transaction.
func (s *ProductStore) Create(ctx context.Context, req product.CreateProductRequest) (*product.Product, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("product_store.Create begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	const insertProduct = `
		INSERT INTO products (name, description, price_cents, currency)
		VALUES ($1, $2, $3, $4)
		RETURNING id, name, description, price_cents, currency, active, created_at, updated_at`

	currency := req.Currency
	if currency == "" {
		currency = "usd"
	}

	p := &product.Product{}
	err = tx.QueryRow(ctx, insertProduct, req.Name, req.Description, req.PriceCents, currency).Scan(
		&p.ID, &p.Name, &p.Description, &p.PriceCents, &p.Currency, &p.Active, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("product_store.Create insert product: %w", err)
	}

	const insertInventory = `INSERT INTO inventory (product_id) VALUES ($1)`
	if _, err = tx.Exec(ctx, insertInventory, p.ID); err != nil {
		return nil, fmt.Errorf("product_store.Create insert inventory: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("product_store.Create commit: %w", err)
	}

	return p, nil
}

// GetByID returns a product by primary key.
func (s *ProductStore) GetByID(ctx context.Context, id uuid.UUID) (*product.Product, error) {
	const q = `
		SELECT id, name, description, price_cents, currency, active, created_at, updated_at
		FROM products
		WHERE id = $1`

	p := &product.Product{}
	err := s.pool.QueryRow(ctx, q, id).Scan(
		&p.ID, &p.Name, &p.Description, &p.PriceCents, &p.Currency, &p.Active, &p.CreatedAt, &p.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("product_store.GetByID %s: %w", id, product.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("product_store.GetByID: %w", err)
	}
	return p, nil
}

// List returns a page of active products and the total active count.
func (s *ProductStore) List(ctx context.Context, page, pageSize int) ([]*product.Product, int, error) {
	const countQ = `SELECT COUNT(*) FROM products WHERE active = true`
	var total int
	if err := s.pool.QueryRow(ctx, countQ).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("product_store.List count: %w", err)
	}

	const q = `
		SELECT id, name, description, price_cents, currency, active, created_at, updated_at
		FROM products
		WHERE active = true
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2`

	offset := (page - 1) * pageSize
	rows, err := s.pool.Query(ctx, q, pageSize, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("product_store.List query: %w", err)
	}
	defer rows.Close()

	var products []*product.Product
	for rows.Next() {
		p := &product.Product{}
		if err := rows.Scan(
			&p.ID, &p.Name, &p.Description, &p.PriceCents, &p.Currency, &p.Active, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("product_store.List scan: %w", err)
		}
		products = append(products, p)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("product_store.List rows: %w", err)
	}

	return products, total, nil
}

// Update modifies a product's mutable fields.
func (s *ProductStore) Update(ctx context.Context, id uuid.UUID, req product.UpdateProductRequest) (*product.Product, error) {
	const q = `
		UPDATE products
		SET name = $1, description = $2, price_cents = $3, active = $4, updated_at = NOW()
		WHERE id = $5
		RETURNING id, name, description, price_cents, currency, active, created_at, updated_at`

	p := &product.Product{}
	err := s.pool.QueryRow(ctx, q, req.Name, req.Description, req.PriceCents, req.Active, id).Scan(
		&p.ID, &p.Name, &p.Description, &p.PriceCents, &p.Currency, &p.Active, &p.CreatedAt, &p.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("product_store.Update %s: %w", id, product.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("product_store.Update: %w", err)
	}
	return p, nil
}

// Deactivate soft-deletes a product by setting active=false.
func (s *ProductStore) Deactivate(ctx context.Context, id uuid.UUID) error {
	const q = `UPDATE products SET active = false, updated_at = NOW() WHERE id = $1`
	tag, err := s.pool.Exec(ctx, q, id)
	if err != nil {
		return fmt.Errorf("product_store.Deactivate: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("product_store.Deactivate %s: %w", id, product.ErrNotFound)
	}
	return nil
}
