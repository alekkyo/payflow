package handlers

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/alexkua/payflow/internal/domain/payment"
	"github.com/alexkua/payflow/internal/queue"
)

// WebhookHandler handles incoming Stripe webhook events.
type WebhookHandler struct {
	provider     payment.PaymentProvider
	paymentStore payment.Store
	producer     *queue.Producer
	logger       *slog.Logger
}

// NewWebhookHandler creates a WebhookHandler.
func NewWebhookHandler(
	provider payment.PaymentProvider,
	paymentStore payment.Store,
	producer *queue.Producer,
	logger *slog.Logger,
) *WebhookHandler {
	return &WebhookHandler{
		provider:     provider,
		paymentStore: paymentStore,
		producer:     producer,
		logger:       logger,
	}
}

// Handle handles POST /webhooks/stripe.
// Stripe requires a 200 response within 30 seconds; we validate, deduplicate,
// enqueue to a Redis Stream, and return 200 immediately. All processing is async.
func (h *WebhookHandler) Handle(w http.ResponseWriter, r *http.Request) {
	// IMPORTANT: the raw body must be read before any other operation because
	// Stripe's signature verification hashes the exact bytes received over the wire.
	// If you parse or re-encode the body first, the hash won't match.
	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.logger.Error("webhook.Handle read body", "error", err)
		writeError(w, http.StatusBadRequest, "could not read body")
		return
	}

	sig := r.Header.Get("Stripe-Signature")
	event, err := h.provider.ConstructWebhookEvent(body, sig)
	if err != nil {
		// Invalid signature — could be a spoofed request or clock drift.
		h.logger.Warn("webhook.Handle invalid signature", "error", err)
		writeError(w, http.StatusBadRequest, "invalid webhook signature")
		return
	}

	// Deduplication: Stripe sends webhooks at least once.
	// If we've already processed this event ID, return 200 so Stripe stops retrying.
	processed, err := h.paymentStore.IsWebhookProcessed(r.Context(), event.ID)
	if err != nil {
		h.logger.Error("webhook.Handle idempotency check", "event_id", event.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if processed {
		h.logger.Info("webhook.Handle duplicate — ignoring", "event_id", event.ID, "type", event.Type)
		w.WriteHeader(http.StatusOK)
		return
	}

	// Mark the event as processed BEFORE enqueuing. If we crash after marking but
	// before publishing, the event is missed — but the reconciliation job catches
	// those. This order of operations prevents the much worse outcome of
	// processing the same charge twice.
	if err := h.paymentStore.MarkWebhookProcessed(r.Context(), event.ID, event.Type); err != nil {
		h.logger.Error("webhook.Handle mark processed", "event_id", event.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// For payment_intent.* events, extract the PaymentIntent ID from data.object.id
	// and store it as a top-level field so the webhook worker can do a direct lookup
	// without re-parsing the full payload.
	stripePaymentID := extractStripePaymentID(event.Payload)

	if _, err := h.producer.Publish(r.Context(), queue.StreamStripeWebhooks, map[string]any{
		"event_id":          event.ID,
		"event_type":        event.Type,
		"stripe_payment_id": stripePaymentID,
		"raw_payload":       string(event.Payload),
	}); err != nil {
		// Non-fatal: the event is marked processed. The reconciliation job will
		// catch any orders that stall because the stream message was lost.
		h.logger.Error("webhook.Handle publish to stream", "event_id", event.ID, "error", err)
	}

	h.logger.Info("webhook.Handle enqueued", "event_id", event.ID, "type", event.Type)
	w.WriteHeader(http.StatusOK)
}

// extractStripePaymentID parses data.object.id from a Stripe event payload.
// For payment_intent.* events this is the PaymentIntent ID (pi_xxx).
func extractStripePaymentID(payload []byte) string {
	var envelope struct {
		Data struct {
			Object struct {
				ID string `json:"id"`
			} `json:"object"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return ""
	}
	return envelope.Data.Object.ID
}
