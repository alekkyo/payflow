package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/alexkua/payflow/internal/config"
	"github.com/alexkua/payflow/internal/observability"
	pgstore "github.com/alexkua/payflow/internal/store/postgres"
	rdstore "github.com/alexkua/payflow/internal/store/redis"
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

	// Workers will be registered here in Week 3+.
	// For now, block until a shutdown signal is received.
	logger.Info("worker service started — awaiting shutdown signal")
	_ = pool
	_ = rdb

	<-ctx.Done()
	logger.Info("worker service stopped")
}
