// Package config loads application configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Config holds all runtime configuration for the application.
type Config struct {
	DatabaseURL         string
	RedisURL            string
	Port                string
	Env                 string
	SessionDuration     time.Duration
	WorkerDelay         time.Duration // artificial delay before processing; useful in dev to observe state transitions via SSE
	StripeAPIKey        string
	StripeWebhookSecret string
	AllowedOrigins      []string // CORS allowed origins, e.g. ["http://localhost:5173"]
	OTLPEndpoint        string   // OpenTelemetry collector endpoint, e.g. "localhost:4318"
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

	workerDelay, _ := time.ParseDuration(os.Getenv("WORKER_DELAY"))

	// ALLOWED_ORIGINS is a comma-separated list: "http://localhost:5173,https://app.example.com"
	// Defaults to localhost:5173 (Vite dev server) in development.
	allowedOrigins := []string{"http://localhost:5173"}
	if raw := os.Getenv("ALLOWED_ORIGINS"); raw != "" {
		allowedOrigins = strings.Split(raw, ",")
	}

	return &Config{
		DatabaseURL:         databaseURL,
		RedisURL:            redisURL,
		Port:                port,
		Env:                 env,
		SessionDuration:     24 * time.Hour,
		WorkerDelay:         workerDelay,
		StripeAPIKey:        os.Getenv("STRIPE_API_KEY"),
		StripeWebhookSecret: os.Getenv("STRIPE_WEBHOOK_SECRET"),
		AllowedOrigins:      allowedOrigins,
		OTLPEndpoint:        os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
	}, nil
}
