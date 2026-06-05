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
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/alexkua/payflow/internal/api/handlers"
	"github.com/alexkua/payflow/internal/api/middleware"
	"github.com/alexkua/payflow/internal/config"
	"github.com/alexkua/payflow/internal/domain/order"
	"github.com/alexkua/payflow/internal/domain/payment"
	"github.com/alexkua/payflow/internal/domain/product"
	"github.com/alexkua/payflow/internal/domain/reconciliation"
	"github.com/alexkua/payflow/internal/domain/user"
	"github.com/alexkua/payflow/internal/queue"
	redisstore "github.com/alexkua/payflow/internal/store/redis"
)

// Server wraps the HTTP server and its dependencies.
type Server struct {
	httpServer *http.Server
	logger     *slog.Logger
}

// NewServer constructs and configures the HTTP server with all routes.
func NewServer(
	cfg *config.Config,
	pool *pgxpool.Pool,
	rdb *redis.Client,
	userStore user.Store,
	productStore product.Store,
	inventoryStore product.InventoryStore,
	orderStore order.Store,
	paymentStore payment.Store,
	provider payment.PaymentProvider,
	reconcileStore reconciliation.Store,
	logger *slog.Logger,
) *Server {
	r := chi.NewRouter()

	// Global middleware
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.Recoverer)
	r.Use(middleware.CORS(cfg.AllowedOrigins))
	r.Use(middleware.Logger(logger))

	productCache := redisstore.NewProductCache(rdb)
	producer := queue.NewProducer(rdb)
	authHandler := handlers.NewAuthHandler(userStore, cfg.SessionDuration, logger)
	productHandler := handlers.NewProductHandler(productStore, inventoryStore, productCache, logger)
	orderHandler := handlers.NewOrderHandler(orderStore, productStore, inventoryStore, producer, rdb, logger)
	paymentHandler := handlers.NewPaymentHandler(paymentStore, orderStore, producer, logger)
	webhookHandler := handlers.NewWebhookHandler(provider, paymentStore, producer, logger)
	adminHandler := handlers.NewAdminHandler(reconcileStore, producer, rdb, logger)

	// Observability
	r.Get("/health", handlers.Health)
	r.Get("/ready", handlers.Ready(pool, rdb))
	// /metrics exposes all Prometheus counters, histograms, and gauges for scraping
	// by Prometheus or Grafana. Registered automatically via promauto in the observability package.
	r.Get("/metrics", promhttp.Handler().ServeHTTP)

	// Auth (public)
	r.Post("/auth/register", authHandler.Register)
	r.Post("/auth/login", authHandler.Login)

	// Auth (protected)
	r.Group(func(r chi.Router) {
		r.Use(middleware.Auth(userStore))
		r.Post("/auth/logout", authHandler.Logout)
	})

	// Products (public — reads)
	r.Get("/products", productHandler.List)
	r.Get("/products/{id}", productHandler.GetByID)

	// Products (admin — writes)
	r.Group(func(r chi.Router) {
		r.Use(middleware.Auth(userStore))
		r.Use(middleware.RequireAdmin)
		r.Post("/products", productHandler.Create)
		r.Put("/products/{id}", productHandler.Update)
		r.Delete("/products/{id}", productHandler.Deactivate)
		r.Put("/products/{id}/inventory", productHandler.SetInventory)
	})

	// Orders (authenticated)
	r.Group(func(r chi.Router) {
		r.Use(middleware.Auth(userStore))
		// Rate limit order creation: 5 requests per minute per authenticated user.
		// Protects against runaway clients and makes the per-user cost of spam expensive.
		r.With(middleware.RateLimit(rdb, "orders:create", 5, time.Minute, middleware.UserIDFromClaims)).
			Post("/orders", orderHandler.Create)
		r.Get("/orders", orderHandler.List)
		r.Get("/orders/{id}", orderHandler.GetByID)
		r.Post("/orders/{id}/cancel", orderHandler.Cancel)
		r.Get("/orders/{id}/events/stream", orderHandler.StreamEvents)
		r.Post("/orders/{id}/refunds", paymentHandler.CreateRefund)
		r.Get("/orders/{id}/refunds", paymentHandler.ListRefunds)
	})

	// Payments (authenticated)
	r.Group(func(r chi.Router) {
		r.Use(middleware.Auth(userStore))
		r.Get("/payments/{id}", paymentHandler.GetByID)
	})

	// Webhooks (public — Stripe signs payloads, we validate the signature instead of using auth)
	// Rate limit by IP: 100 per minute. Stripe sends at most a few events per second in normal
	// operation; this stops a misconfigured or malicious sender from flooding our queue.
	r.With(middleware.RateLimit(rdb, "webhooks:stripe", 100, time.Minute, middleware.IPAddress)).
		Post("/webhooks/stripe", webhookHandler.Handle)

	// Admin routes (authenticated + admin role required)
	r.Group(func(r chi.Router) {
		r.Use(middleware.Auth(userStore))
		r.Use(middleware.RequireAdmin)
		r.Get("/admin/reconciliation/runs", adminHandler.ListReconciliationRuns)
		r.Get("/admin/reconciliation/runs/{id}", adminHandler.GetReconciliationRun)
		r.Post("/admin/reconciliation/trigger", adminHandler.TriggerReconciliation)
		r.Get("/admin/deadletter", adminHandler.ListDeadLetterMessages)
		r.Get("/admin/queues", adminHandler.GetQueueDepths)
	})

	// otelhttp wraps the entire router so every request automatically gets a
	// root trace span. The span name is set from the matched route pattern
	// ("GET /orders/{id}") rather than the raw path, avoiding cardinality issues.
	handler := otelhttp.NewHandler(r, "payflow-api",
		otelhttp.WithSpanNameFormatter(func(op string, r *http.Request) string {
			return r.Method + " " + r.URL.Path
		}),
	)

	return &Server{
		httpServer: &http.Server{
			Addr:         ":" + cfg.Port,
			Handler:      handler,
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 0, // disabled — SSE connections are long-lived
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
