// Package middleware provides reusable HTTP middleware for the PayFlow API.
package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/alexkua/payflow/internal/observability"
)

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// Flush implements http.Flusher so SSE handlers can push events to clients immediately.
func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Logger logs each request and records Prometheus API duration metrics.
func Logger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(rw, r)

			duration := time.Since(start)

			// chi stores the matched route pattern (e.g. /orders/{id}) in request context.
			// Using the pattern instead of the actual path prevents unbounded label
			// cardinality in Prometheus — /orders/{id} is one label value, not millions.
			routePattern := r.URL.Path
			if rctx := chi.RouteContext(r.Context()); rctx != nil && rctx.RoutePattern() != "" {
				routePattern = rctx.RoutePattern()
			}

			observability.APIRequestDuration.WithLabelValues(
				r.Method,
				routePattern,
				fmt.Sprintf("%d", rw.status),
			).Observe(duration.Seconds())

			logger.Info("request",
				"method",      r.Method,
				"path",        r.URL.Path,
				"route",       routePattern,
				"status",      rw.status,
				"duration_ms", duration.Milliseconds(),
				"remote_addr", r.RemoteAddr,
			)
		})
	}
}
