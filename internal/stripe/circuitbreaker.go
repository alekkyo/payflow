package stripe

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/sony/gobreaker"

	"github.com/alexkua/payflow/internal/domain/payment"
)

// circuitBreakerProvider wraps a PaymentProvider with a circuit breaker.
// The breaker opens after 5 consecutive failures and stays open for 30 seconds,
// rejecting calls immediately rather than letting them hang until timeout.
// After 30 seconds it moves to half-open: one probe request is allowed through.
// If it succeeds the breaker closes; if it fails it opens again.
type circuitBreakerProvider struct {
	inner payment.PaymentProvider
	cb    *gobreaker.CircuitBreaker
}

// NewCircuitBreakerProvider wraps inner with a Stripe-specific circuit breaker.
// Pass the wrapped value to workers that need a PaymentProvider — no other
// code needs to change because the interface is identical.
func NewCircuitBreakerProvider(inner payment.PaymentProvider, logger *slog.Logger) payment.PaymentProvider {
	settings := gobreaker.Settings{
		Name:        "stripe",
		MaxRequests: 1,              // allow 1 probe request in half-open state
		Interval:    10 * time.Second, // reset consecutive failure count every 10s in closed state
		Timeout:     30 * time.Second, // stay open for 30s before probing again
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			// Open after 5 consecutive failures. A single transient error won't trip it.
			return counts.ConsecutiveFailures >= 5
		},
		OnStateChange: func(name string, from, to gobreaker.State) {
			logger.Warn("stripe circuit breaker state change",
				"name", name,
				"from", from.String(),
				"to", to.String(),
			)
		},
	}

	return &circuitBreakerProvider{
		inner: inner,
		cb:    gobreaker.NewCircuitBreaker(settings),
	}
}

func (c *circuitBreakerProvider) CreatePaymentIntent(ctx context.Context, req payment.PaymentIntentRequest) (*payment.PaymentIntentResult, error) {
	result, err := c.cb.Execute(func() (any, error) {
		return c.inner.CreatePaymentIntent(ctx, req)
	})
	if err != nil {
		return nil, fmt.Errorf("stripe circuit breaker: %w", err)
	}
	return result.(*payment.PaymentIntentResult), nil
}

func (c *circuitBreakerProvider) CreateRefund(ctx context.Context, req payment.RefundRequest) (*payment.RefundResult, error) {
	result, err := c.cb.Execute(func() (any, error) {
		return c.inner.CreateRefund(ctx, req)
	})
	if err != nil {
		return nil, fmt.Errorf("stripe circuit breaker: %w", err)
	}
	return result.(*payment.RefundResult), nil
}

// ListPaymentIntents is used by the reconcile worker. It's a read-only operation
// so it also participates in the circuit breaker — a prolonged Stripe outage
// should not cause the reconciler to hammer the API indefinitely.
func (c *circuitBreakerProvider) ListPaymentIntents(ctx context.Context, from, to time.Time) ([]payment.PaymentIntentSummary, error) {
	result, err := c.cb.Execute(func() (any, error) {
		return c.inner.ListPaymentIntents(ctx, from, to)
	})
	if err != nil {
		return nil, fmt.Errorf("stripe circuit breaker: %w", err)
	}
	return result.([]payment.PaymentIntentSummary), nil
}

func (c *circuitBreakerProvider) ConstructWebhookEvent(payload []byte, signature string) (*payment.WebhookEvent, error) {
	// Webhook validation is local (HMAC check) — no network call to Stripe.
	// Skip the circuit breaker so webhook processing continues even while the
	// breaker is open for outbound API calls.
	return c.inner.ConstructWebhookEvent(payload, signature)
}
