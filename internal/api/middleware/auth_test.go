package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/alexkua/payflow/internal/api/middleware"
	"github.com/alexkua/payflow/internal/domain/user"
)

// mockUserStore implements user.Store for middleware tests.
type mockUserStore struct {
	session *user.Session
	u       *user.User
	sessErr error
	userErr error
}

func (m *mockUserStore) Create(_ context.Context, _, _ string) (*user.User, error) {
	return nil, nil
}

func (m *mockUserStore) GetByEmail(_ context.Context, _ string) (*user.User, error) {
	return nil, nil
}

func (m *mockUserStore) GetByID(_ context.Context, _ uuid.UUID) (*user.User, error) {
	return m.u, m.userErr
}

func (m *mockUserStore) CreateSession(_ context.Context, _ uuid.UUID, _ string, _ time.Time) (*user.Session, error) {
	return nil, nil
}

func (m *mockUserStore) GetSessionByTokenHash(_ context.Context, _ string) (*user.Session, error) {
	return m.session, m.sessErr
}

func (m *mockUserStore) DeleteSession(_ context.Context, _ string) error {
	return nil
}

// ── BearerToken ─────────────────────────────────────────────────────────────

func TestBearerToken(t *testing.T) {
	tests := []struct {
		name  string
		setup func(r *http.Request)
		want  string
	}{
		{
			name: "Authorization header",
			setup: func(r *http.Request) {
				r.Header.Set("Authorization", "Bearer mytoken123")
			},
			want: "mytoken123",
		},
		{
			name: "token query param fallback (for SSE)",
			setup: func(r *http.Request) {
				r.URL.RawQuery = "token=querytoken"
			},
			want: "querytoken",
		},
		{
			name: "header takes precedence over query param",
			setup: func(r *http.Request) {
				r.Header.Set("Authorization", "Bearer headertoken")
				r.URL.RawQuery = "token=querytoken"
			},
			want: "headertoken",
		},
		{
			name:  "no token returns empty string",
			setup: func(_ *http.Request) {},
			want:  "",
		},
		{
			name: "malformed Authorization header (no Bearer prefix)",
			setup: func(r *http.Request) {
				r.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			tt.setup(r)
			got := middleware.BearerToken(r)
			if got != tt.want {
				t.Errorf("BearerToken() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ── Auth middleware ──────────────────────────────────────────────────────────

func TestAuth(t *testing.T) {
	userID := uuid.New()
	validUser := &user.User{ID: userID, Email: "test@example.com", Role: "customer"}
	future := time.Now().Add(24 * time.Hour)
	past := time.Now().Add(-24 * time.Hour)
	validSession := &user.Session{ID: uuid.New(), UserID: userID, ExpiresAt: future}

	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := middleware.ClaimsFromContext(r.Context())
		if !ok || claims == nil {
			http.Error(w, "no claims", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	tests := []struct {
		name       string
		token      string
		store      *mockUserStore
		wantStatus int
	}{
		{
			name:  "valid token grants access and sets claims",
			token: "Bearer validtoken",
			store: &mockUserStore{
				session: validSession,
				u:       validUser,
			},
			wantStatus: http.StatusOK,
		},
		{
			name:       "missing token returns 401",
			token:      "",
			store:      &mockUserStore{},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:  "expired session returns 401",
			token: "Bearer expiredtoken",
			store: &mockUserStore{
				session: &user.Session{ID: uuid.New(), UserID: userID, ExpiresAt: past},
				u:       validUser,
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:  "unknown token returns 401",
			token: "Bearer unknowntoken",
			store: &mockUserStore{
				sessErr: user.ErrNotFound,
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:  "store error returns 500",
			token: "Bearer sometoken",
			store: &mockUserStore{
				sessErr: &storeError{"db down"},
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := middleware.Auth(tt.store)(okHandler)
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.token != "" {
				r.Header.Set("Authorization", tt.token)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

// storeError is a non-ErrNotFound error for testing store failure paths.
type storeError struct{ msg string }

func (e *storeError) Error() string { return e.msg }

// ── ClaimsFromContext ────────────────────────────────────────────────────────

func TestClaimsFromContext(t *testing.T) {
	t.Run("claims present in context", func(t *testing.T) {
		userID := uuid.New()
		validUser := &user.User{ID: userID, Role: "admin"}
		validSession := &user.Session{ID: uuid.New(), UserID: userID, ExpiresAt: time.Now().Add(time.Hour)}

		var capturedClaims *user.Claims
		inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			capturedClaims, _ = middleware.ClaimsFromContext(r.Context())
		})

		handler := middleware.Auth(&mockUserStore{
			session: validSession,
			u:       validUser,
		})(inner)

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Authorization", "Bearer sometoken")
		handler.ServeHTTP(httptest.NewRecorder(), r)

		if capturedClaims == nil {
			t.Fatal("expected claims in context, got nil")
		}
		if capturedClaims.ID != userID {
			t.Errorf("claims.ID = %v, want %v", capturedClaims.ID, userID)
		}
		if capturedClaims.Role != "admin" {
			t.Errorf("claims.Role = %q, want %q", capturedClaims.Role, "admin")
		}
	})

	t.Run("no claims in empty context", func(t *testing.T) {
		claims, ok := middleware.ClaimsFromContext(context.Background())
		if ok || claims != nil {
			t.Error("expected no claims in empty context")
		}
	})
}
