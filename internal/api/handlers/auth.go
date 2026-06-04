package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/alexkua/payflow/internal/api/middleware"
	"github.com/alexkua/payflow/internal/domain/user"
)

// AuthHandler holds dependencies for the auth endpoints.
type AuthHandler struct {
	store           user.Store
	sessionDuration time.Duration
	logger          *slog.Logger
}

// NewAuthHandler creates an AuthHandler.
func NewAuthHandler(store user.Store, sessionDuration time.Duration, logger *slog.Logger) *AuthHandler {
	return &AuthHandler{
		store:           store,
		sessionDuration: sessionDuration,
		logger:          logger,
	}
}

type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type authResponse struct {
	Token     string       `json:"token"`
	ExpiresAt time.Time    `json:"expires_at"`
	User      userResponse `json:"user"`
}

type userResponse struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

// Register handles POST /auth/register.
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := validateCredentials(req.Email, req.Password); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	passwordHash, err := hashPassword(req.Password)
	if err != nil {
		h.logger.Error("register: hash password", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	u, err := h.store.Create(r.Context(), req.Email, passwordHash)
	if err != nil {
		// Detect duplicate email via the unique constraint message.
		if strings.Contains(err.Error(), "unique") {
			writeError(w, http.StatusConflict, "email already registered")
			return
		}
		h.logger.Error("register: create user", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	resp, err := h.createSessionResponse(r, u)
	if err != nil {
		h.logger.Error("register: create session", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	h.logger.Info("user registered", "user_id", u.ID, "email", u.Email)
	writeJSON(w, http.StatusCreated, resp)
}

// Login handles POST /auth/login.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	u, err := h.store.GetByEmail(r.Context(), req.Email)
	if err != nil {
		if errors.Is(err, user.ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "invalid email or password")
			return
		}
		h.logger.Error("login: get user", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if err := comparePassword(u.PasswordHash, req.Password); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	resp, err := h.createSessionResponse(r, u)
	if err != nil {
		h.logger.Error("login: create session", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	h.logger.Info("user logged in", "user_id", u.ID)
	writeJSON(w, http.StatusOK, resp)
}

// Logout handles POST /auth/logout. Requires a valid Bearer token.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	token := middleware.BearerToken(r)
	if token == "" {
		writeError(w, http.StatusUnauthorized, "missing authorization token")
		return
	}

	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])

	if err := h.store.DeleteSession(r.Context(), tokenHash); err != nil {
		h.logger.Error("logout: delete session", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// createSessionResponse generates a token, stores the session, and returns the auth response.
func (h *AuthHandler) createSessionResponse(r *http.Request, u *user.User) (*authResponse, error) {
	token, tokenHash, err := generateToken()
	if err != nil {
		return nil, fmt.Errorf("generateToken: %w", err)
	}

	expiresAt := time.Now().Add(h.sessionDuration)
	if _, err := h.store.CreateSession(r.Context(), u.ID, tokenHash, expiresAt); err != nil {
		return nil, fmt.Errorf("CreateSession: %w", err)
	}

	return &authResponse{
		Token:     token,
		ExpiresAt: expiresAt,
		User: userResponse{
			ID:        u.ID,
			Email:     u.Email,
			Role:      u.Role,
			CreatedAt: u.CreatedAt,
		},
	}, nil
}

// generateToken creates a cryptographically random opaque token and its SHA-256 hash.
func generateToken() (token, tokenHash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", fmt.Errorf("rand.Read: %w", err)
	}
	token = base64.URLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(token))
	tokenHash = hex.EncodeToString(h[:])
	return token, tokenHash, nil
}

func hashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return "", fmt.Errorf("bcrypt.GenerateFromPassword: %w", err)
	}
	return string(b), nil
}

func comparePassword(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

func validateCredentials(email, password string) error {
	if !strings.Contains(email, "@") || len(email) < 3 {
		return fmt.Errorf("invalid email address")
	}
	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	return nil
}

