// Package config loads application configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"time"
)

// Config holds all runtime configuration for the application.
type Config struct {
	DatabaseURL     string
	RedisURL        string
	Port            string
	Env             string
	SessionDuration time.Duration
	StripeAPIKey    string
	StripeWebhookSecret string
}

// Load reads configuration from environment variables and returns a Config.
// Returns an error if any required variables are missing.
func Load() (*Config, error) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return nil, fmt.Errorf("config: DATABASE_URL not set")
	}

	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://localhost:6379"
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	env := os.Getenv("ENV")
	if env == "" {
		env = "development"
	}

	return &Config{
		DatabaseURL:         databaseURL,
		RedisURL:            redisURL,
		Port:                port,
		Env:                 env,
		SessionDuration:     24 * time.Hour,
		StripeAPIKey:        os.Getenv("STRIPE_API_KEY"),
		StripeWebhookSecret: os.Getenv("STRIPE_WEBHOOK_SECRET"),
	}, nil
}
