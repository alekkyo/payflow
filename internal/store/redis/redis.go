// Package redis provides a Redis client configured for PayFlow.
package redis

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// NewClient parses the given URL and returns a connected redis.Client.
func NewClient(ctx context.Context, redisURL string) (*redis.Client, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("redis.NewClient parse URL: %w", err)
	}

	client := redis.NewClient(opts)

	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("redis.NewClient ping: %w", err)
	}

	return client, nil
}
