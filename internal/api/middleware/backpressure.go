package middleware

import (
	"fmt"
	"net/http"

	"github.com/redis/go-redis/v9"
)

// Backpressure returns middleware that rejects new requests with 503 when a
// Redis Stream's length exceeds threshold. It is fundamentally different from
// rate limiting: rate limiting caps *input* regardless of system state, while
// backpressure caps input in response to downstream congestion.
//
// Applied to POST /orders: if the orders.created stream has more than threshold
// unprocessed messages, inventory workers are overwhelmed. Accepting more orders
// would only grow the backlog — better to tell clients to retry in 30 seconds.
//
// Redis errors fail open (let the request through) so a Redis blip doesn't
// take the entire API offline.
func Backpressure(rdb *redis.Client, stream string, threshold int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			depth, err := rdb.XLen(r.Context(), stream).Result()
			if err == nil && depth >= threshold {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "30")
				// Expose the depth so operators can see how far behind workers are.
				w.Header().Set("X-Queue-Depth", fmt.Sprintf("%d", depth))
				w.WriteHeader(http.StatusServiceUnavailable)
				fmt.Fprintf(w, `{"error":"system is overloaded, please retry in 30 seconds"}`) //nolint:errcheck
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
