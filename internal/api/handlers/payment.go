package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/alexkua/payflow/internal/api/middleware"
	"github.com/alexkua/payflow/internal/domain/order"
	"github.com/alexkua/payflow/internal/domain/payment"
	"github.com/alexkua/payflow/internal/queue"
)

// PaymentHandler handles payment and refund endpoints.
type PaymentHandler struct {
	paymentStore payment.Store
	orderStore   order.Store
	producer     *queue.Producer
	logger       *slog.Logger
}

// NewPaymentHandler creates a PaymentHandler.
func NewPaymentHandler(
	paymentStore payment.Store,
	orderStore order.Store,
	producer *queue.Producer,
	logger *slog.Logger,
) *PaymentHandler {
	return &PaymentHandler{
		paymentStore: paymentStore,
		orderStore:   orderStore,
		producer:     producer,
		logger:       logger,
	}
}

// GetByID handles GET /payments/:id.
func (h *PaymentHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid payment id")
		return
	}

	claims, _ := middleware.ClaimsFromContext(r.Context())

	p, err := h.paymentStore.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, payment.ErrNotFound) {
			writeError(w, http.StatusNotFound, "payment not found")
			return
		}
		h.logger.Error("payment.GetByID", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Verify ownership via the order — admins see everything.
	if claims.Role != "admin" {
		o, err := h.orderStore.GetByID(r.Context(), p.OrderID)
		if err != nil || o.UserID != claims.ID {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
	}

	writeJSON(w, http.StatusOK, p)
}

// CreateRefund handles POST /orders/:id/refunds.
// Requires an Idempotency-Key header. Creates a pending refund and enqueues it.
func (h *PaymentHandler) CreateRefund(w http.ResponseWriter, r *http.Request) {
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		writeError(w, http.StatusBadRequest, "Idempotency-Key header is required")
		return
	}

	orderID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid order id")
		return
	}

	claims, _ := middleware.ClaimsFromContext(r.Context())

	// Ownership check.
	o, err := h.orderStore.GetByID(r.Context(), orderID)
	if err != nil {
		if errors.Is(err, order.ErrNotFound) {
			writeError(w, http.StatusNotFound, "order not found")
			return
		}
		h.logger.Error("payment.CreateRefund get order", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if claims.Role != "admin" && o.UserID != claims.ID {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	var req struct {
		AmountCents int    `json:"amount_cents"`
		Reason      string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.AmountCents <= 0 {
		writeError(w, http.StatusUnprocessableEntity, "amount_cents must be positive")
		return
	}

	// Idempotency check.
	existing, err := h.paymentStore.GetRefundByIdempotencyKey(r.Context(), idempotencyKey)
	if err == nil {
		writeJSON(w, http.StatusOK, existing)
		return
	}
	if !errors.Is(err, payment.ErrNotFound) {
		h.logger.Error("payment.CreateRefund idempotency check", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Get the payment for this order.
	p, err := h.paymentStore.GetByOrderID(r.Context(), orderID)
	if err != nil {
		if errors.Is(err, payment.ErrNotFound) {
			writeError(w, http.StatusUnprocessableEntity, "no payment found for this order")
			return
		}
		h.logger.Error("payment.CreateRefund get payment", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if p.Status != payment.StatusCaptured {
		writeError(w, http.StatusConflict, "payment must be captured before it can be refunded")
		return
	}

	// Create a pending refund row. The actual Stripe call happens in the refund worker.
	refund, err := h.paymentStore.CreateRefund(r.Context(), p.ID, req.AmountCents, req.Reason, idempotencyKey)
	if err != nil {
		h.logger.Error("payment.CreateRefund create", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Enqueue for async processing. The refund worker does the Stripe call.
	if _, err := h.producer.Publish(r.Context(), queue.StreamRefundsRequested, map[string]any{
		"refund_id":  refund.ID.String(),
		"order_id":   orderID.String(),
		"payment_id": p.ID.String(),
	}); err != nil {
		h.logger.Error("payment.CreateRefund publish", "refund_id", refund.ID, "error", err)
	}

	h.logger.Info("refund requested", "refund_id", refund.ID, "order_id", orderID)
	writeJSON(w, http.StatusAccepted, refund)
}

// ListRefunds handles GET /orders/:id/refunds.
func (h *PaymentHandler) ListRefunds(w http.ResponseWriter, r *http.Request) {
	orderID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid order id")
		return
	}

	claims, _ := middleware.ClaimsFromContext(r.Context())

	o, err := h.orderStore.GetByID(r.Context(), orderID)
	if err != nil {
		if errors.Is(err, order.ErrNotFound) {
			writeError(w, http.StatusNotFound, "order not found")
			return
		}
		h.logger.Error("payment.ListRefunds get order", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if claims.Role != "admin" && o.UserID != claims.ID {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	p, err := h.paymentStore.GetByOrderID(r.Context(), orderID)
	if err != nil {
		if errors.Is(err, payment.ErrNotFound) {
			writeJSON(w, http.StatusOK, map[string]any{"refunds": []any{}})
			return
		}
		h.logger.Error("payment.ListRefunds get payment", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	refunds, err := h.paymentStore.GetRefundsByPaymentID(r.Context(), p.ID)
	if err != nil {
		h.logger.Error("payment.ListRefunds", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if refunds == nil {
		refunds = []*payment.Refund{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"refunds": refunds})
}
