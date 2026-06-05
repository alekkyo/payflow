package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// All metrics are registered with the default Prometheus registry via promauto,
// which auto-registers on package init — no manual Register calls needed.

var (
	// OrdersTotal counts orders created and their terminal outcomes.
	OrdersTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "payflow_orders_total",
		Help: "Total orders by status transition.",
	}, []string{"status"})

	// PaymentsTotal counts payment attempts and their outcomes.
	PaymentsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "payflow_payments_total",
		Help: "Total payment attempts by status.",
	}, []string{"status"})

	// WebhookEventsTotal counts incoming Stripe webhook events by type.
	WebhookEventsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "payflow_webhook_events_total",
		Help: "Total Stripe webhook events processed by type.",
	}, []string{"type"})

	// ReconciliationDiscrepanciesTotal counts discrepancies found during reconciliation.
	ReconciliationDiscrepanciesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "payflow_reconciliation_discrepancies_total",
		Help: "Total reconciliation discrepancies by type.",
	}, []string{"type"})

	// APIRequestDuration measures HTTP request latency by method, route pattern, and status code.
	// Uses chi.RouteContext to get the pattern (/orders/{id}) rather than the actual path
	// (/orders/abc-123), which would create unbounded cardinality in Prometheus.
	APIRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "payflow_api_request_duration_seconds",
		Help:    "HTTP request latency by method, route, and status.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "route", "status"})

	// PaymentProcessingDuration measures end-to-end payment processing time
	// from when the worker picks up the message to when Stripe responds.
	PaymentProcessingDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "payflow_payment_processing_duration_seconds",
		Help:    "End-to-end payment processing duration in the payment worker.",
		Buckets: prometheus.DefBuckets,
	})

	// QueueProcessingDuration measures per-message processing time in each worker stream.
	QueueProcessingDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "payflow_queue_processing_duration_seconds",
		Help:    "Time spent processing a single stream message by worker type.",
		Buckets: prometheus.DefBuckets,
	}, []string{"stream", "worker"})

	// QueueDepth tracks the number of unacknowledged messages in each stream.
	// Updated by a background goroutine in the worker service.
	QueueDepth = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "payflow_queue_depth",
		Help: "Number of pending (unacknowledged) messages per stream.",
	}, []string{"stream"})
)
