package middleware

import (
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// slidingWindowScript implements a sliding window rate limiter using a Redis sorted set.
// Each request is recorded as a member with its timestamp as the score. Members older
// than the window are pruned on every request, so the set always represents the current window.
// The script is atomic — no race condition between the check and the increment.
const slidingWindowScript = `
local key        = KEYS[1]
local now        = tonumber(ARGV[1])
local window_ms  = tonumber(ARGV[2])
local max        = tonumber(ARGV[3])
local request_id = ARGV[4]

redis.call('ZREMRANGEBYSCORE', key, 0, now - window_ms)
redis.call('ZADD', key, now, request_id)
redis.call('PEXPIRE', key, window_ms)

local count = redis.call('ZCARD', key)
if count > max then
    return 0
end
return 1
`

// RateLimit returns middleware that enforces a sliding window rate limit.
// keyFn derives the Redis key from the request — typically the user ID or IP.
// max is the maximum number of requests allowed within window.
func RateLimit(rdb *redis.Client, keyPrefix string, max int, window time.Duration, keyFn func(r *http.Request) string) func(http.Handler) http.Handler {
	script := redis.NewScript(slidingWindowScript)
	windowMS := window.Milliseconds()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			identifier := keyFn(r)
			if identifier == "" {
				// No identifier — can't rate limit, let through.
				next.ServeHTTP(w, r)
				return
			}

			key := fmt.Sprintf("ratelimit:%s:%s", keyPrefix, identifier)
			nowMS := time.Now().UnixMilli()
			requestID := uuid.New().String()

			result, err := script.Run(r.Context(), rdb, []string{key},
				nowMS, windowMS, max, requestID,
			).Int()

			if err != nil {
				// Redis error — fail open (let the request through) rather than
				// blocking all traffic when Redis is temporarily unavailable.
				next.ServeHTTP(w, r)
				return
			}

			if result == 0 {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", max))
				w.Header().Set("Retry-After", fmt.Sprintf("%d", int(window.Seconds())))
				w.WriteHeader(http.StatusTooManyRequests)
				fmt.Fprintf(w, `{"error":"rate limit exceeded"}`) //nolint:errcheck
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// UserIDFromClaims extracts the user ID from the auth claims for rate limiting.
// Used for authenticated endpoints like POST /orders.
func UserIDFromClaims(r *http.Request) string {
	claims, ok := ClaimsFromContext(r.Context())
	if !ok {
		return ""
	}
	return claims.ID.String()
}

// IPAddress extracts the client IP for rate limiting unauthenticated endpoints like webhooks.
func IPAddress(r *http.Request) string {
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return ip
	}
	return r.RemoteAddr
}
