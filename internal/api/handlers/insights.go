package handlers

import (
	"log/slog"
	"net/http"

	"github.com/alexkua/payflow/internal/insights"
)

// InsightsHandler serves AI-generated payment insights to admin users.
type InsightsHandler struct {
	service *insights.Service
	logger  *slog.Logger
}

// NewInsightsHandler creates an InsightsHandler.
func NewInsightsHandler(service *insights.Service, logger *slog.Logger) *InsightsHandler {
	return &InsightsHandler{service: service, logger: logger}
}

// GetSummary handles GET /admin/insights.
// Pass ?refresh=true to bypass the 30-minute Redis cache and call Claude immediately.
func (h *InsightsHandler) GetSummary(w http.ResponseWriter, r *http.Request) {
	refresh := r.URL.Query().Get("refresh") == "true"

	summary, err := h.service.GenerateSummary(r.Context(), refresh)
	if err != nil {
		h.logger.Error("insights.GetSummary", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to generate insights")
		return
	}

	writeJSON(w, http.StatusOK, summary)
}
