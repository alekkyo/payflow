package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/alexkua/payflow/internal/api/middleware"
	"github.com/alexkua/payflow/internal/domain/order"
	"github.com/alexkua/payflow/internal/domain/product"
	"github.com/alexkua/payflow/internal/observability"
	"github.com/alexkua/payflow/internal/queue"
)

// OrderHandler handles HTTP requests for orders.
type OrderHandler struct {
	orderStore     order.Store
	productStore   product.Store
	inventoryStore product.InventoryStore
	producer       *queue.Producer
	rdb            *redis.Client
	logger         *slog.Logger
}

// NewOrderHandler creates an OrderHandler.
func NewOrderHandler(
	orderStore order.Store,
	productStore product.Store,
	inventoryStore product.InventoryStore,
	producer *queue.Producer,
	rdb *redis.Client,
	logger *slog.Logger,
) *OrderHandler {
	return &OrderHandler{
		orderStore:     orderStore,
		productStore:   productStore,
		inventoryStore: inventoryStore,
		producer:       producer,
		rdb:            rdb,
		logger:         logger,
	}
}

type createOrderRequest struct {
	Items []order.CreateOrderItem `json:"items"`
}

// Create handles POST /orders.
// Requires an Idempotency-Key header. Duplicate requests return the original response.
func (h *OrderHandler) Create(w http.ResponseWriter, r *http.Request) {
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		writeError(w, http.StatusBadRequest, "Idempotency-Key header is required")
		return
	}

	claims, _ := middleware.ClaimsFromContext(r.Context())

	// Idempotency check — return the existing order if this key was already processed.
	existing, err := h.orderStore.GetByIdempotencyKey(r.Context(), idempotencyKey)
	if err == nil {
		writeJSON(w, http.StatusOK, existing)
		return
	}
	if !errors.Is(err, order.ErrNotFound) {
		h.logger.Error("order.Create idempotency check", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	var req createOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Items) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "order must contain at least one item")
		return
	}

	// Resolve current prices from the product catalog — snapshot at order time.
	items, totalCents, err := h.resolveItems(r.Context(), req.Items)
	if err != nil {
		if errors.Is(err, product.ErrNotFound) || errors.Is(err, product.ErrInsufficientStock) {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		h.logger.Error("order.Create resolve items", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	createReq := order.CreateOrderRequest{
		IdempotencyKey: idempotencyKey,
		UserID:         claims.ID,
		Items:          req.Items,
	}

	o, err := h.orderStore.Create(r.Context(), createReq, totalCents, items)
	if err != nil {
		h.logger.Error("order.Create store", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Seed the status cache so SSE has something to read immediately.
	h.setStatusCache(r.Context(), o.ID, order.StatusCreated)

	// Publish to the saga's first stream.
	if _, err := h.producer.Publish(r.Context(), queue.StreamOrdersCreated, map[string]any{
		"order_id": o.ID.String(),
		"user_id":  o.UserID.String(),
	}); err != nil {
		h.logger.Error("order.Create publish", "order_id", o.ID, "error", err)
		// Non-fatal: order is persisted; a reconciliation job can re-trigger the saga.
	}

	observability.OrdersTotal.WithLabelValues("created").Inc()

	h.logger.Info("order created",
		"order_id", o.ID,
		"user_id", o.UserID,
		"total_cents", totalCents,
		"items", len(items),
	)

	writeJSON(w, http.StatusCreated, o)
}

// GetByID handles GET /orders/:id.
func (h *OrderHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "id")
	if !ok {
		return
	}

	claims, _ := middleware.ClaimsFromContext(r.Context())

	o, err := h.orderStore.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, order.ErrNotFound) {
			writeError(w, http.StatusNotFound, "order not found")
			return
		}
		h.logger.Error("order.GetByID", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Customers can only view their own orders; admins can view any.
	if claims.Role != "admin" && o.UserID != claims.ID {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	writeJSON(w, http.StatusOK, o)
}

// List handles GET /orders — returns the authenticated user's orders.
func (h *OrderHandler) List(w http.ResponseWriter, r *http.Request) {
	claims, _ := middleware.ClaimsFromContext(r.Context())

	orders, err := h.orderStore.ListByUserID(r.Context(), claims.ID)
	if err != nil {
		h.logger.Error("order.List", "error", err, "user_id", claims.ID)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if orders == nil {
		orders = []*order.Order{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"orders": orders})
}

// Cancel handles POST /orders/:id/cancel.
func (h *OrderHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "id")
	if !ok {
		return
	}

	claims, _ := middleware.ClaimsFromContext(r.Context())

	// Verify ownership before cancelling.
	o, err := h.orderStore.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, order.ErrNotFound) {
			writeError(w, http.StatusNotFound, "order not found")
			return
		}
		h.logger.Error("order.Cancel get", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if claims.Role != "admin" && o.UserID != claims.ID {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	if err := h.orderStore.Cancel(r.Context(), id, "api:"+claims.ID.String()); err != nil {
		if errors.Is(err, order.ErrNotCancellable) {
			writeError(w, http.StatusConflict, "order cannot be cancelled in its current state")
			return
		}
		h.logger.Error("order.Cancel store", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	h.setStatusCache(r.Context(), id, order.StatusCancelled)
	h.logger.Info("order cancelled by customer", "order_id", id, "user_id", claims.ID)
	w.WriteHeader(http.StatusNoContent)
}

// StreamEvents handles GET /orders/:id/events/stream using Server-Sent Events.
// The client receives a status update whenever the order transitions state.
func (h *OrderHandler) StreamEvents(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "id")
	if !ok {
		return
	}

	claims, _ := middleware.ClaimsFromContext(r.Context())

	o, err := h.orderStore.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, order.ErrNotFound) {
			writeError(w, http.StatusNotFound, "order not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if claims.Role != "admin" && o.UserID != claims.ID {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	// SSE headers — tell the browser this is a long-lived event stream.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering if present
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		return
	}

	// Subscribe to the Pub/Sub channel for this order.
	channel := fmt.Sprintf("order:%s:events", id)
	sub := h.rdb.Subscribe(r.Context(), channel)
	defer sub.Close()

	// Send current status immediately so the client doesn't wait for the first event.
	sendSSE(w, flusher, "status", map[string]string{
		"order_id": id.String(),
		"status":   o.Status,
	})

	// Stream events until the client disconnects or the order reaches a terminal state.
	for {
		select {
		case <-r.Context().Done():
			return

		case msg, ok := <-sub.Channel():
			if !ok {
				return
			}
			sendSSE(w, flusher, "status", map[string]string{
				"order_id": id.String(),
				"status":   msg.Payload,
			})

			// Stop streaming once the order reaches a terminal state.
			if isTerminal(msg.Payload) {
				sendSSE(w, flusher, "done", map[string]string{"order_id": id.String()})
				return
			}
		}
	}
}

func sendSSE(w http.ResponseWriter, flusher http.Flusher, event string, data any) {
	b, _ := json.Marshal(data)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
	flusher.Flush()
}

func isTerminal(status string) bool {
	switch status {
	case order.StatusFulfilled, order.StatusCancelled, order.StatusRefunded:
		return true
	}
	return false
}

// resolveItems looks up current product prices and builds the order items slice.
// Prices are snapshotted at order creation time — future price changes don't affect existing orders.
func (h *OrderHandler) resolveItems(ctx context.Context, reqItems []order.CreateOrderItem) ([]order.OrderItem, int, error) {
	items := make([]order.OrderItem, 0, len(reqItems))
	totalCents := 0

	for _, ri := range reqItems {
		if ri.Quantity <= 0 {
			return nil, 0, fmt.Errorf("quantity must be positive for product %s", ri.ProductID)
		}

		p, err := h.productStore.GetByID(ctx, ri.ProductID)
		if err != nil {
			if errors.Is(err, product.ErrNotFound) {
				return nil, 0, fmt.Errorf("product %s not found: %w", ri.ProductID, product.ErrNotFound)
			}
			return nil, 0, fmt.Errorf("resolveItems get product %s: %w", ri.ProductID, err)
		}
		if !p.Active {
			return nil, 0, fmt.Errorf("product %s is not available: %w", p.Name, product.ErrNotFound)
		}

		inv, err := h.inventoryStore.Get(ctx, ri.ProductID)
		if err != nil {
			return nil, 0, fmt.Errorf("resolveItems get inventory %s: %w", ri.ProductID, err)
		}
		if inv.Available() < ri.Quantity {
			return nil, 0, fmt.Errorf("product %q has insufficient stock: %w", p.Name, product.ErrInsufficientStock)
		}

		items = append(items, order.OrderItem{
			ProductID:   ri.ProductID,
			ProductName: p.Name,
			Quantity:    ri.Quantity,
			PriceCents:  p.PriceCents,
		})
		totalCents += p.PriceCents * ri.Quantity
	}

	return items, totalCents, nil
}

func (h *OrderHandler) setStatusCache(ctx context.Context, orderID uuid.UUID, status string) {
	key := fmt.Sprintf("order:%s:status", orderID)
	if err := h.rdb.Set(ctx, key, status, time.Hour).Err(); err != nil {
		h.logger.Error("order.setStatusCache", "order_id", orderID, "error", err)
	}
}
