// Package user defines the user and session domain types and the persistence interface.
package user

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// User represents an authenticated account in the system.
type User struct {
	ID           uuid.UUID
	Email        string
	PasswordHash string
	Role         string
	CreatedAt    time.Time
}

// Session represents an active login session backed by an opaque token.
type Session struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	TokenHash string
	ExpiresAt time.Time
	CreatedAt time.Time
}

// Store is the persistence interface for users and sessions.
// Implementations live in internal/store/postgres.
type Store interface {
	// Create inserts a new user and returns it.
	Create(ctx context.Context, email, passwordHash string) (*User, error)

	// GetByEmail returns a user by email, or an error wrapping ErrNotFound.
	GetByEmail(ctx context.Context, email string) (*User, error)

	// CreateSession persists a new session token hash for the given user.
	CreateSession(ctx context.Context, userID uuid.UUID, tokenHash string, expiresAt time.Time) (*Session, error)

	// GetSessionByTokenHash looks up a session by its hashed token.
	// Returns an error wrapping ErrNotFound if the session does not exist.
	GetSessionByTokenHash(ctx context.Context, tokenHash string) (*Session, error)

	// DeleteSession removes a session by its hashed token (logout).
	DeleteSession(ctx context.Context, tokenHash string) error
}

// ErrNotFound is returned by Store methods when a record does not exist.
var ErrNotFound = &notFoundError{}

type notFoundError struct{}

func (e *notFoundError) Error() string { return "record not found" }
