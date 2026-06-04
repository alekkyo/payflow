package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/alexkua/payflow/internal/domain/order"
)

// OrderStore implements order.Store against PostgreSQL.
type OrderStore struct {
	pool *pgxpool.Pool
}

// NewOrderStore creates an OrderStore backed by the given connection pool.
func NewOrderStore(pool *pgxpool.Pool) *OrderStore {
	return &OrderStore{pool: pool}
}

// Create inserts an order, its items, and the initial "created" event in one transaction.
func (s *OrderStore) Create(ctx context.Context, req order.CreateOrderRequest, totalCents int, items []order.OrderItem) (*order.Order, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("order_store.Create begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	const insertOrder = `
		INSERT INTO orders (user_id, total_cents, idempotency_key)
		VALUES ($1, $2, $3)
		RETURNING id, user_id, status, total_cents, currency, idempotency_key, created_at, updated_at`

	o := &order.Order{}
	err = tx.QueryRow(ctx, insertOrder, req.UserID, totalCents, req.IdempotencyKey).Scan(
		&o.ID, &o.UserID, &o.Status, &o.TotalCents, &o.Currency, &o.IdempotencyKey, &o.CreatedAt, &o.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("order_store.Create insert order: %w", err)
	}

	const insertItem = `
		INSERT INTO order_items (order_id, product_id, quantity, price_cents)
		VALUES ($1, $2, $3, $4)
		RETURNING id, order_id, product_id, quantity, price_cents, created_at`

	for i, item := range items {
		if err := tx.QueryRow(ctx, insertItem, o.ID, item.ProductID, item.Quantity, item.PriceCents).Scan(
			&items[i].ID, &items[i].OrderID, &items[i].ProductID,
			&items[i].Quantity, &items[i].PriceCents, &items[i].CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("order_store.Create insert item: %w", err)
		}
	}
	o.Items = items

	const insertEvent = `
		INSERT INTO order_events (order_id, event_type, created_by)
		VALUES ($1, $2, $3)`

	if _, err := tx.Exec(ctx, insertEvent, o.ID, order.EventCreated, "api"); err != nil {
		return nil, fmt.Errorf("order_store.Create insert event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("order_store.Create commit: %w", err)
	}

	return o, nil
}

// GetByID returns an order with all its items and event history.
func (s *OrderStore) GetByID(ctx context.Context, id uuid.UUID) (*order.Order, error) {
	const q = `
		SELECT id, user_id, status, total_cents, currency, idempotency_key, created_at, updated_at
		FROM orders WHERE id = $1`

	o := &order.Order{}
	err := s.pool.QueryRow(ctx, q, id).Scan(
		&o.ID, &o.UserID, &o.Status, &o.TotalCents, &o.Currency, &o.IdempotencyKey, &o.CreatedAt, &o.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("order_store.GetByID %s: %w", id, order.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("order_store.GetByID: %w", err)
	}

	if err := s.loadItems(ctx, o); err != nil {
		return nil, err
	}
	if err := s.loadEvents(ctx, o); err != nil {
		return nil, err
	}

	return o, nil
}

// GetByIdempotencyKey fetches an existing order matching the given key.
func (s *OrderStore) GetByIdempotencyKey(ctx context.Context, key string) (*order.Order, error) {
	const q = `
		SELECT id, user_id, status, total_cents, currency, idempotency_key, created_at, updated_at
		FROM orders WHERE idempotency_key = $1`

	o := &order.Order{}
	err := s.pool.QueryRow(ctx, q, key).Scan(
		&o.ID, &o.UserID, &o.Status, &o.TotalCents, &o.Currency, &o.IdempotencyKey, &o.CreatedAt, &o.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("order_store.GetByIdempotencyKey: %w", order.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("order_store.GetByIdempotencyKey: %w", err)
	}

	if err := s.loadItems(ctx, o); err != nil {
		return nil, err
	}

	return o, nil
}

// ListByUserID returns all orders for a user, newest first.
func (s *OrderStore) ListByUserID(ctx context.Context, userID uuid.UUID) ([]*order.Order, error) {
	const q = `
		SELECT id, user_id, status, total_cents, currency, idempotency_key, created_at, updated_at
		FROM orders WHERE user_id = $1
		ORDER BY created_at DESC`

	rows, err := s.pool.Query(ctx, q, userID)
	if err != nil {
		return nil, fmt.Errorf("order_store.ListByUserID: %w", err)
	}
	defer rows.Close()

	var orders []*order.Order
	for rows.Next() {
		o := &order.Order{}
		if err := rows.Scan(
			&o.ID, &o.UserID, &o.Status, &o.TotalCents, &o.Currency, &o.IdempotencyKey, &o.CreatedAt, &o.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("order_store.ListByUserID scan: %w", err)
		}
		orders = append(orders, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("order_store.ListByUserID rows: %w", err)
	}

	return orders, nil
}

// UpdateStatus sets order.status and appends an event atomically.
func (s *OrderStore) UpdateStatus(ctx context.Context, orderID uuid.UUID, status, eventType, createdBy string, payload []byte) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("order_store.UpdateStatus begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	const updateOrder = `UPDATE orders SET status = $1, updated_at = NOW() WHERE id = $2`
	if _, err := tx.Exec(ctx, updateOrder, status, orderID); err != nil {
		return fmt.Errorf("order_store.UpdateStatus update: %w", err)
	}

	const insertEvent = `
		INSERT INTO order_events (order_id, event_type, payload, created_by)
		VALUES ($1, $2, $3, $4)`

	if _, err := tx.Exec(ctx, insertEvent, orderID, eventType, payload, createdBy); err != nil {
		return fmt.Errorf("order_store.UpdateStatus insert event: %w", err)
	}

	return tx.Commit(ctx)
}

// Cancel transitions an order to cancelled if it is in a cancellable state.
func (s *OrderStore) Cancel(ctx context.Context, orderID uuid.UUID, createdBy string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("order_store.Cancel begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	const selectStatus = `SELECT status FROM orders WHERE id = $1 FOR UPDATE`
	var status string
	if err := tx.QueryRow(ctx, selectStatus, orderID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("order_store.Cancel: %w", order.ErrNotFound)
		}
		return fmt.Errorf("order_store.Cancel select: %w", err)
	}

	if !order.CancellableStatuses[status] {
		return fmt.Errorf("order_store.Cancel status=%s: %w", status, order.ErrNotCancellable)
	}

	const updateOrder = `UPDATE orders SET status = $1, updated_at = NOW() WHERE id = $2`
	if _, err := tx.Exec(ctx, updateOrder, order.StatusCancelled, orderID); err != nil {
		return fmt.Errorf("order_store.Cancel update: %w", err)
	}

	const insertEvent = `
		INSERT INTO order_events (order_id, event_type, created_by)
		VALUES ($1, $2, $3)`

	if _, err := tx.Exec(ctx, insertEvent, orderID, order.EventCancelled, createdBy); err != nil {
		return fmt.Errorf("order_store.Cancel insert event: %w", err)
	}

	return tx.Commit(ctx)
}

func (s *OrderStore) loadItems(ctx context.Context, o *order.Order) error {
	const q = `
		SELECT id, order_id, product_id, quantity, price_cents, created_at
		FROM order_items WHERE order_id = $1`

	rows, err := s.pool.Query(ctx, q, o.ID)
	if err != nil {
		return fmt.Errorf("order_store.loadItems: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		item := order.OrderItem{}
		if err := rows.Scan(&item.ID, &item.OrderID, &item.ProductID, &item.Quantity, &item.PriceCents, &item.CreatedAt); err != nil {
			return fmt.Errorf("order_store.loadItems scan: %w", err)
		}
		o.Items = append(o.Items, item)
	}
	return rows.Err()
}

func (s *OrderStore) loadEvents(ctx context.Context, o *order.Order) error {
	const q = `
		SELECT id, order_id, event_type, payload, created_by, created_at
		FROM order_events WHERE order_id = $1
		ORDER BY created_at ASC`

	rows, err := s.pool.Query(ctx, q, o.ID)
	if err != nil {
		return fmt.Errorf("order_store.loadEvents: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		ev := order.OrderEvent{}
		if err := rows.Scan(&ev.ID, &ev.OrderID, &ev.EventType, &ev.Payload, &ev.CreatedBy, &ev.CreatedAt); err != nil {
			return fmt.Errorf("order_store.loadEvents scan: %w", err)
		}
		o.Events = append(o.Events, ev)
	}
	return rows.Err()
}
