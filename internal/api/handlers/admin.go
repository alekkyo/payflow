package handlers

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/alexkua/payflow/internal/domain/reconciliation"
	"github.com/alexkua/payflow/internal/queue"
)

// AdminHandler exposes internal operations for operators and monitoring tools.
// All routes require the admin role.
type AdminHandler struct {
	reconcileStore reconciliation.Store
	producer       *queue.Producer
	rdb            *redis.Client
	logger         *slog.Logger
}

// NewAdminHandler creates an AdminHandler.
func NewAdminHandler(
	reconcileStore reconciliation.Store,
	producer *queue.Producer,
	rdb *redis.Client,
	logger *slog.Logger,
) *AdminHandler {
	return &AdminHandler{
		reconcileStore: reconcileStore,
		producer:       producer,
		rdb:            rdb,
		logger:         logger,
	}
}

// ListReconciliationRuns handles GET /admin/reconciliation/runs.
// Returns the 20 most recent reconciliation runs.
func (h *AdminHandler) ListReconciliationRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := h.reconcileStore.ListRuns(r.Context(), 20)
	if err != nil {
		h.logger.Error("admin.ListReconciliationRuns", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if runs == nil {
		runs = []*reconciliation.Run{} // return [] not null
	}
	writeJSON(w, http.StatusOK, runs)
}

// GetReconciliationRun handles GET /admin/reconciliation/runs/{id}.
// Returns the run plus all its discrepancy records.
func (h *AdminHandler) GetReconciliationRun(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid run id")
		return
	}

	run, err := h.reconcileStore.GetRunWithDiscrepancies(r.Context(), id)
	if err != nil {
		h.logger.Error("admin.GetReconciliationRun", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if run == nil {
		writeError(w, http.StatusNotFound, "reconciliation run not found")
		return
	}
	writeJSON(w, http.StatusOK, run)
}

// TriggerReconciliation handles POST /admin/reconciliation/trigger.
// Publishes a message to stream:reconciliation.trigger. An optional "date" query
// parameter (YYYY-MM-DD) specifies which day to reconcile; defaults to yesterday.
func (h *AdminHandler) TriggerReconciliation(w http.ResponseWriter, r *http.Request) {
	dateStr := r.URL.Query().Get("date")
	if dateStr != "" {
		if _, err := time.Parse("2006-01-02", dateStr); err != nil {
			writeError(w, http.StatusBadRequest, "invalid date — expected YYYY-MM-DD")
			return
		}
	}

	fields := map[string]any{"triggered_by": "admin"}
	if dateStr != "" {
		fields["date"] = dateStr
	}

	msgID, err := h.producer.Publish(r.Context(), queue.StreamReconcileTrigger, fields)
	if err != nil {
		h.logger.Error("admin.TriggerReconciliation publish", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{
		"message": "reconciliation triggered",
		"msg_id":  msgID,
		"date":    dateStr,
	})
}

// ListDeadLetterMessages handles GET /admin/deadletter.
// Returns up to 100 messages from the dead letter stream for inspection.
// The optional "count" query parameter overrides the default.
func (h *AdminHandler) ListDeadLetterMessages(w http.ResponseWriter, r *http.Request) {
	count := int64(100)
	if raw := r.URL.Query().Get("count"); raw != "" {
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil && n > 0 && n <= 1000 {
			count = n
		}
	}

	msgs, err := h.rdb.XRevRange(r.Context(), queue.StreamDeadLetter, "+", "-").Result()
	if err != nil {
		h.logger.Error("admin.ListDeadLetterMessages XREVRANGE", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Cap to requested count. XRevRange returns newest-first.
	if int64(len(msgs)) > count {
		msgs = msgs[:count]
	}

	type deadLetterMessage struct {
		ID     string            `json:"id"`
		Fields map[string]any    `json:"fields"`
	}

	result := make([]deadLetterMessage, len(msgs))
	for i, m := range msgs {
		result[i] = deadLetterMessage{ID: m.ID, Fields: m.Values}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"count":    len(result),
		"messages": result,
	})
}

// GetQueueDepths handles GET /admin/queues.
// Returns approximate message counts for each stream. Useful for monitoring
// whether workers are keeping up with the message rate.
func (h *AdminHandler) GetQueueDepths(w http.ResponseWriter, r *http.Request) {
	streams := []string{
		queue.StreamOrdersCreated,
		queue.StreamPaymentsReady,
		queue.StreamPaymentsCaptured,
		queue.StreamPaymentsFailed,
		queue.StreamRefundsRequested,
		queue.StreamStripeWebhooks,
		queue.StreamReconcileTrigger,
		queue.StreamDeadLetter,
	}

	depths := make(map[string]int64, len(streams))
	for _, stream := range streams {
		length, err := h.rdb.XLen(r.Context(), stream).Result()
		if err != nil {
			// Stream may not exist yet; treat as 0.
			depths[fmt.Sprintf("stream:%s", stream)] = 0
			continue
		}
		depths[stream] = length
	}

	writeJSON(w, http.StatusOK, depths)
}
