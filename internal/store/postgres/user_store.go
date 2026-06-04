package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/alexkua/payflow/internal/domain/user"
)

// UserStore implements user.Store against PostgreSQL.
type UserStore struct {
	pool *pgxpool.Pool
}

// NewUserStore creates a UserStore backed by the given connection pool.
func NewUserStore(pool *pgxpool.Pool) *UserStore {
	return &UserStore{pool: pool}
}

// Create inserts a new user row and returns the persisted record.
func (s *UserStore) Create(ctx context.Context, email, passwordHash string) (*user.User, error) {
	const q = `
		INSERT INTO users (email, password_hash)
		VALUES ($1, $2)
		RETURNING id, email, password_hash, role, created_at`

	u := &user.User{}
	err := s.pool.QueryRow(ctx, q, email, passwordHash).Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("user_store.Create: %w", err)
	}
	return u, nil
}

// GetByID fetches a user by primary key.
func (s *UserStore) GetByID(ctx context.Context, id uuid.UUID) (*user.User, error) {
	const q = `
		SELECT id, email, password_hash, role, created_at
		FROM users
		WHERE id = $1`

	u := &user.User{}
	err := s.pool.QueryRow(ctx, q, id).Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("user_store.GetByID %s: %w", id, user.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("user_store.GetByID: %w", err)
	}
	return u, nil
}

// GetByEmail fetches a user by email address.
func (s *UserStore) GetByEmail(ctx context.Context, email string) (*user.User, error) {
	const q = `
		SELECT id, email, password_hash, role, created_at
		FROM users
		WHERE email = $1`

	u := &user.User{}
	err := s.pool.QueryRow(ctx, q, email).Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("user_store.GetByEmail %s: %w", email, user.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("user_store.GetByEmail: %w", err)
	}
	return u, nil
}

// CreateSession inserts a new session row and returns it.
func (s *UserStore) CreateSession(ctx context.Context, userID uuid.UUID, tokenHash string, expiresAt time.Time) (*user.Session, error) {
	const q = `
		INSERT INTO sessions (user_id, token_hash, expires_at)
		VALUES ($1, $2, $3)
		RETURNING id, user_id, token_hash, expires_at, created_at`

	sess := &user.Session{}
	err := s.pool.QueryRow(ctx, q, userID, tokenHash, expiresAt).Scan(
		&sess.ID, &sess.UserID, &sess.TokenHash, &sess.ExpiresAt, &sess.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("user_store.CreateSession: %w", err)
	}
	return sess, nil
}

// GetSessionByTokenHash returns the session matching the given hashed token.
func (s *UserStore) GetSessionByTokenHash(ctx context.Context, tokenHash string) (*user.Session, error) {
	const q = `
		SELECT id, user_id, token_hash, expires_at, created_at
		FROM sessions
		WHERE token_hash = $1`

	sess := &user.Session{}
	err := s.pool.QueryRow(ctx, q, tokenHash).Scan(
		&sess.ID, &sess.UserID, &sess.TokenHash, &sess.ExpiresAt, &sess.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("user_store.GetSessionByTokenHash: %w", user.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("user_store.GetSessionByTokenHash: %w", err)
	}
	return sess, nil
}

// DeleteSession removes the session with the given hashed token.
func (s *UserStore) DeleteSession(ctx context.Context, tokenHash string) error {
	const q = `DELETE FROM sessions WHERE token_hash = $1`
	if _, err := s.pool.Exec(ctx, q, tokenHash); err != nil {
		return fmt.Errorf("user_store.DeleteSession: %w", err)
	}
	return nil
}
