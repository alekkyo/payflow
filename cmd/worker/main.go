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
	stripeclient "github.com/alexkua/payflow/internal/stripe"
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
	paymentStore   := pgstore.NewPaymentStore(pool)
	invSvc         := product.NewInventoryService(inventoryStore)
	locker         := rdstore.NewLocker(rdb)
	stripeProvider := stripeclient.NewClient(cfg.StripeAPIKey, cfg.StripeWebhookSecret)

	var wg sync.WaitGroup

	// Inventory workers — 3 instances share the consumer group for parallelism.
	const numInventoryWorkers = 3
	for i := 0; i < numInventoryWorkers; i++ {
		workerID := fmt.Sprintf("inventory-worker-%d", i)

		invWorker, err := worker.NewInventoryWorker(ctx, rdb, orderStore, invSvc, locker, workerID, cfg.WorkerDelay, logger)
		if err != nil {
			logger.Error("creating inventory worker", "worker_id", workerID, "error", err)
			os.Exit(1)
		}

		wg.Add(2) // main loop + compensation loop
		go func(w *worker.InventoryWorker) {
			defer wg.Done()
			w.Run(ctx)
		}(invWorker)
		go func(w *worker.InventoryWorker) {
			defer wg.Done()
			w.RunCompensation(ctx)
		}(invWorker)
	}

	// Payment workers — 2 instances.
	const numPaymentWorkers = 2
	for i := 0; i < numPaymentWorkers; i++ {
		workerID := fmt.Sprintf("payment-worker-%d", i)

		payWorker, err := worker.NewPaymentWorker(ctx, rdb, orderStore, paymentStore, stripeProvider, locker, workerID, cfg.WorkerDelay, logger)
		if err != nil {
			logger.Error("creating payment worker", "worker_id", workerID, "error", err)
			os.Exit(1)
		}

		wg.Add(1)
		go func(w *worker.PaymentWorker) {
			defer wg.Done()
			w.Run(ctx)
		}(payWorker)
	}

	// Webhook workers — 2 instances process stream:stripe.webhooks.
	const numWebhookWorkers = 2
	for i := 0; i < numWebhookWorkers; i++ {
		workerID := fmt.Sprintf("webhook-worker-%d", i)

		whWorker, err := worker.NewWebhookWorker(ctx, rdb, orderStore, paymentStore, workerID, logger)
		if err != nil {
			logger.Error("creating webhook worker", "worker_id", workerID, "error", err)
			os.Exit(1)
		}

		wg.Add(1)
		go func(w *worker.WebhookWorker) {
			defer wg.Done()
			w.Run(ctx)
		}(whWorker)
	}

	// Refund workers — 1 instance.
	refundWorker, err := worker.NewRefundWorker(ctx, rdb, orderStore, paymentStore, invSvc, stripeProvider, "refund-worker-0", logger)
	if err != nil {
		logger.Error("creating refund worker", "error", err)
		os.Exit(1)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		refundWorker.Run(ctx)
	}()

	logger.Info("worker service started",
		"inventory_workers", numInventoryWorkers,
		"payment_workers", numPaymentWorkers,
		"webhook_workers", numWebhookWorkers,
		"refund_workers", 1,
	)

	wg.Wait()
	logger.Info("worker service stopped")
}
