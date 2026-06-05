package middleware

import (
	"net/http"
	"strings"
)

// CORS returns middleware that adds Cross-Origin Resource Sharing headers.
// allowedOrigins is a list of exact origins (e.g. "http://localhost:5173").
// Pass []string{"*"} to allow all origins (not recommended for authenticated APIs).
//
// Why CORS is needed: browsers enforce the Same-Origin Policy — a script at
// localhost:5173 is blocked from calling localhost:8080 unless the API explicitly
// permits it via these headers. Non-browser clients (curl, Postman, mobile apps)
// are not subject to this restriction and ignore CORS headers entirely.
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	allowed := make(map[string]bool, len(allowedOrigins))
	for _, o := range allowedOrigins {
		allowed[o] = true
	}
	wildcard := allowed["*"]

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			if wildcard || allowed[origin] {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Vary", "Origin")
			}

			// Preflight request: the browser sends OPTIONS before the real request
			// to check whether the actual method/headers are permitted.
			if r.Method == http.MethodOptions {
				w.Header().Set("Access-Control-Allow-Methods", strings.Join([]string{
					http.MethodGet, http.MethodPost, http.MethodPut,
					http.MethodDelete, http.MethodOptions,
				}, ", "))
				w.Header().Set("Access-Control-Allow-Headers",
					"Accept, Authorization, Content-Type, Idempotency-Key")
				w.Header().Set("Access-Control-Max-Age", "300")
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
