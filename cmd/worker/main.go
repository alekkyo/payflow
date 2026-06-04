package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/alexkua/payflow/internal/config"
	"github.com/alexkua/payflow/internal/domain/product"
	"github.com/alexkua/payflow/internal/observability"
	pgstore "github.com/alexkua/payflow/internal/store/postgres"
	rdstore "github.com/alexkua/payflow/internal/store/redis"
	"github.com/alexkua/payflow/internal/worker"
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

	orderStore     := pgstore.NewOrderStore(pool)
	inventoryStore := pgstore.NewInventoryStore(pool)
	invSvc         := product.NewInventoryService(inventoryStore)
	locker         := rdstore.NewLocker(rdb)

	// Run multiple inventory worker instances for parallelism.
	const numInventoryWorkers = 3

	var wg sync.WaitGroup
	for i := 0; i < numInventoryWorkers; i++ {
		workerID := fmt.Sprintf("inventory-worker-%d", i)

		invWorker, err := worker.NewInventoryWorker(ctx, rdb, orderStore, invSvc, locker, workerID, cfg.WorkerDelay, logger)
		if err != nil {
			logger.Error("creating inventory worker", "worker_id", workerID, "error", err)
			os.Exit(1)
		}

		wg.Add(1)
		go func(w *worker.InventoryWorker) {
			defer wg.Done()
			w.Run(ctx)
		}(invWorker)
	}

	logger.Info("worker service started", "inventory_workers", numInventoryWorkers)
	wg.Wait()
	logger.Info("worker service stopped")
}
