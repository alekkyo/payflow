// Package handlers contains HTTP handler functions for the PayFlow API.
package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Health handles GET /health — simple liveness check.
func Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Ready handles GET /ready — readiness check that verifies DB and Redis.
func Ready(pool *pgxpool.Pool, rdb *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		type check struct {
			name string
			fn   func() error
		}

		checks := []check{
			{"postgres", func() error { return pool.Ping(ctx) }},
			{"redis", func() error { return rdb.Ping(ctx).Err() }},
		}

		result := make(map[string]string, len(checks))
		healthy := true

		for _, c := range checks {
			if err := c.fn(); err != nil {
				result[c.name] = "unavailable"
				healthy = false
			} else {
				result[c.name] = "ok"
			}
		}

		status := http.StatusOK
		if !healthy {
			status = http.StatusServiceUnavailable
		}
		writeJSON(w, status, result)
	}
}

// writeJSON encodes v as JSON and writes it with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
