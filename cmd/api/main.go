package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alexkua/payflow/internal/api"
	"github.com/alexkua/payflow/internal/config"
	"github.com/alexkua/payflow/internal/observability"
	pgstore "github.com/alexkua/payflow/internal/store/postgres"
	rdstore "github.com/alexkua/payflow/internal/store/redis"
	stripeclient "github.com/alexkua/payflow/internal/stripe"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("loading config", "error", err)
		os.Exit(1)
	}

	logger := observability.NewLogger(cfg.Env)
	slog.SetDefault(logger)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// InitTracer sets up the global OTel TracerProvider and W3C propagator.
	// Every HTTP request handled by otelhttp gets a root span automatically.
	shutdownTracer, err := observability.InitTracer(ctx, "payflow-api", cfg.OTLPEndpoint)
	if err != nil {
		logger.Error("initialising tracer", "error", err)
		os.Exit(1)
	}
	defer shutdownTracer(context.Background()) //nolint:errcheck

	pool, err := pgstore.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("connecting to postgres", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	rdb, err := rdstore.NewClient(ctx, cfg.RedisURL)
	if err != nil {
		logger.Error("connecting to redis", "error", err)
		os.Exit(1)
	}
	defer rdb.Close()

	userStore       := pgstore.NewUserStore(pool)
	productStore    := pgstore.NewProductStore(pool)
	inventoryStore  := pgstore.NewInventoryStore(pool)
	orderStore      := pgstore.NewOrderStore(pool)
	paymentStore    := pgstore.NewPaymentStore(pool)
	reconcileStore  := pgstore.NewReconciliationStore(pool)
	stripeProvider  := stripeclient.NewClient(cfg.StripeAPIKey, cfg.StripeWebhookSecret)

	srv := api.NewServer(cfg, pool, rdb, userStore, productStore, inventoryStore, orderStore, paymentStore, stripeProvider, reconcileStore, logger)

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- srv.Start()
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-serverErr:
		logger.Error("server error", "error", err)
		os.Exit(1)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}

	logger.Info("shutdown complete")
}
