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

const contextKeyUser contextKey = "user"

// Auth returns middleware that requires a valid Bearer session token.
// It hashes the token and looks it up in the user store.
func Auth(store user.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := bearerToken(r)
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

			// Attach the session's user ID to the context for downstream handlers.
			ctx := context.WithValue(r.Context(), contextKeyUser, sess.UserID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UserIDFromContext extracts the authenticated user's UUID from the context.
func UserIDFromContext(ctx context.Context) (interface{}, bool) {
	v := ctx.Value(contextKeyUser)
	return v, v != nil
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(h, "Bearer ")
}
