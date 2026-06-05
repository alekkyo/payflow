package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/alexkua/payflow/internal/domain/user"
)

type contextKey string

const contextKeyClaims contextKey = "claims"

// Auth returns middleware that requires a valid Bearer session token.
// On success it stores a *user.Claims (ID + role) in the request context.
func Auth(store user.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := BearerToken(r)
			if token == "" {
				http.Error(w, `{"error":"missing authorization token"}`, http.StatusUnauthorized)
				return
			}

			h := sha256.Sum256([]byte(token))
			tokenHash := hex.EncodeToString(h[:])

			sess, err := store.GetSessionByTokenHash(r.Context(), tokenHash)
			if err != nil {
				if errors.Is(err, user.ErrNotFound) {
					http.Error(w, `{"error":"invalid or expired token"}`, http.StatusUnauthorized)
					return
				}
				http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
				return
			}

			if time.Now().After(sess.ExpiresAt) {
				http.Error(w, `{"error":"invalid or expired token"}`, http.StatusUnauthorized)
				return
			}

			u, err := store.GetByID(r.Context(), sess.UserID)
			if err != nil {
				http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
				return
			}

			claims := &user.Claims{ID: u.ID, Role: u.Role}
			ctx := context.WithValue(r.Context(), contextKeyClaims, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ClaimsFromContext extracts the authenticated user's claims from the context.
func ClaimsFromContext(ctx context.Context) (*user.Claims, bool) {
	c, ok := ctx.Value(contextKeyClaims).(*user.Claims)
	return c, ok
}

// BearerToken extracts the token from an "Authorization: Bearer <token>" header.
// Falls back to the "token" query parameter to support SSE connections —
// EventSource does not support custom headers, so the token must be in the URL.
func BearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	return r.URL.Query().Get("token")
}
