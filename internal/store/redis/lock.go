package redis

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// luaRelease atomically deletes a lock key only if its value matches the token.
// This prevents releasing a lock owned by a different process that re-acquired it.
var luaRelease = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
    return redis.call("DEL", KEYS[1])
else
    return 0
end`)

// Locker provides distributed locking via Redis SET NX.
type Locker struct {
	client *redis.Client
}

// NewLocker creates a Locker using the given Redis client.
func NewLocker(client *redis.Client) *Locker {
	return &Locker{client: client}
}

// Acquire attempts to acquire a lock for the given key with the specified TTL.
// Returns the lock token (needed for Release) and true on success, or "" and false if the
// lock is already held.
func (l *Locker) Acquire(ctx context.Context, key string, ttl time.Duration) (string, bool, error) {
	token, err := randomToken()
	if err != nil {
		return "", false, fmt.Errorf("lock.Acquire generate token: %w", err)
	}

	// SET key token NX PX <ttl_ms> — only sets if key does not exist.
	ok, err := l.client.SetNX(ctx, key, token, ttl).Result()
	if err != nil {
		return "", false, fmt.Errorf("lock.Acquire setnx %s: %w", key, err)
	}
	if !ok {
		return "", false, nil
	}
	return token, true, nil
}

// Release releases the lock identified by key, but only if the token matches.
// A non-matching token means another process already owns the lock — we do not release it.
func (l *Locker) Release(ctx context.Context, key, token string) error {
	if err := luaRelease.Run(ctx, l.client, []string{key}, token).Err(); err != nil && err != redis.Nil {
		return fmt.Errorf("lock.Release %s: %w", key, err)
	}
	return nil
}

func randomToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("rand.Read: %w", err)
	}
	return hex.EncodeToString(b), nil
}
