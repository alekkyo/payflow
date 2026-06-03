// Package observability provides structured logging and telemetry setup.
package observability

import (
	"log/slog"
	"os"
)

// NewLogger creates a structured slog.Logger. In production it emits JSON;
// in development it emits human-readable text.
func NewLogger(env string) *slog.Logger {
	var handler slog.Handler
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}

	if env == "production" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	return slog.New(handler)
}
