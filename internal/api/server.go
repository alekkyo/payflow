// Package api wires together routing, middleware, and handlers into an HTTP server.
package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/alexkua/payflow/internal/api/handlers"
	"github.com/alexkua/payflow/internal/api/middleware"
	"github.com/alexkua/payflow/internal/config"
	"github.com/alexkua/payflow/internal/domain/user"
)

// Server wraps the HTTP server and its dependencies.
type Server struct {
	httpServer *http.Server
	logger     *slog.Logger
}

// NewServer constructs and configures the HTTP server with all routes.
func NewServer(cfg *config.Config, pool *pgxpool.Pool, rdb *redis.Client, userStore user.Store, logger *slog.Logger) *Server {
	r := chi.NewRouter()

	// Global middleware
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.Recoverer)
	r.Use(middleware.Logger(logger))

	authHandler := handlers.NewAuthHandler(userStore, cfg.SessionDuration, logger)

	// Observability
	r.Get("/health", handlers.Health)
	r.Get("/ready", handlers.Ready(pool, rdb))

	// Auth (public)
	r.Post("/auth/register", authHandler.Register)
	r.Post("/auth/login", authHandler.Login)

	// Auth (protected)
	r.Group(func(r chi.Router) {
		r.Use(middleware.Auth(userStore))
		r.Post("/auth/logout", authHandler.Logout)
	})

	return &Server{
		httpServer: &http.Server{
			Addr:         ":" + cfg.Port,
			Handler:      r,
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 30 * time.Second,
			IdleTimeout:  120 * time.Second,
		},
		logger: logger,
	}
}

// Start begins listening for HTTP requests. It blocks until the server stops.
func (s *Server) Start() error {
	s.logger.Info("API server starting", "addr", s.httpServer.Addr)
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server.Start: %w", err)
	}
	return nil
}

// Shutdown gracefully stops the server, waiting up to 10 seconds for in-flight requests.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("API server shutting down")
	return s.httpServer.Shutdown(ctx)
}
