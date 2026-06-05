// Package stripe wraps the Stripe Go SDK behind the payment.PaymentProvider interface.
// Keeping Stripe behind an interface means tests can inject a mock without hitting
// the real Stripe API, and swapping providers later requires only a new implementation.
package stripe

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	stripelib "github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/paymentintent"
	"github.com/stripe/stripe-go/v81/refund"
	stripewebhook "github.com/stripe/stripe-go/v81/webhook"

	"github.com/alexkua/payflow/internal/domain/payment"
)

// Client is a PaymentProvider backed by the Stripe API.
type Client struct {
	webhookSecret string
}

// NewClient configures the Stripe SDK with the given API key and returns a Client.
// The webhook secret is used to validate the Stripe-Signature header on incoming events.
func NewClient(apiKey, webhookSecret string) *Client {
	stripelib.Key = apiKey
	return &Client{webhookSecret: webhookSecret}
}

// CreatePaymentIntent creates a Stripe PaymentIntent and confirms it immediately
// using the test payment method pm_card_visa. In production this would only
// create the intent (unconfirmed) and return the ClientSecret to the frontend,
// where Stripe.js handles card collection and confirmation.
func (c *Client) CreatePaymentIntent(ctx context.Context, req payment.PaymentIntentRequest) (*payment.PaymentIntentResult, error) {
	params := &stripelib.PaymentIntentParams{
		Amount:        stripelib.Int64(int64(req.AmountCents)),
		Currency:      stripelib.String(req.Currency),
		PaymentMethod: stripelib.String("pm_card_visa"),
		Confirm:       stripelib.Bool(true),
		// AutomaticPaymentMethods with AllowRedirects=never lets us confirm without
		// providing a ReturnURL — required when confirming server-side.
		AutomaticPaymentMethods: &stripelib.PaymentIntentAutomaticPaymentMethodsParams{
			Enabled:        stripelib.Bool(true),
			AllowRedirects: stripelib.String("never"),
		},
		Metadata: map[string]string{
			"order_id": req.OrderID.String(),
		},
	}
	// The idempotency key ensures Stripe deduplicates if we retry the same request.
	params.SetIdempotencyKey(req.IdempotencyKey)

	pi, err := paymentintent.New(params)
	if err != nil {
		return nil, fmt.Errorf("stripe.CreatePaymentIntent: %w", err)
	}

	return &payment.PaymentIntentResult{
		StripeID:     pi.ID,
		Status:       string(pi.Status),
		ClientSecret: pi.ClientSecret,
	}, nil
}

// CreateRefund issues a full or partial refund against a Stripe PaymentIntent.
func (c *Client) CreateRefund(ctx context.Context, req payment.RefundRequest) (*payment.RefundResult, error) {
	params := &stripelib.RefundParams{
		PaymentIntent: stripelib.String(req.StripePaymentID),
		Amount:        stripelib.Int64(int64(req.AmountCents)),
	}
	params.SetIdempotencyKey(req.IdempotencyKey)

	r, err := refund.New(params)
	if err != nil {
		return nil, fmt.Errorf("stripe.CreateRefund: %w", err)
	}

	return &payment.RefundResult{
		StripeRefundID: r.ID,
		Status:         string(r.Status),
	}, nil
}

// ListPaymentIntents fetches all PaymentIntents created within [from, to] from the Stripe API.
// We paginate automatically via the SDK iterator so we don't miss intents when there are many.
// The "order_id" metadata field is used to correlate intents back to our local records.
func (c *Client) ListPaymentIntents(ctx context.Context, from, to time.Time) ([]payment.PaymentIntentSummary, error) {
	params := &stripelib.PaymentIntentListParams{}
	// Stripe filters by Unix timestamps; [gte] and [lte] form a closed interval.
	params.Filters.AddFilter("created[gte]", "", strconv.FormatInt(from.Unix(), 10))
	params.Filters.AddFilter("created[lte]", "", strconv.FormatInt(to.Unix(), 10))
	params.Filters.AddFilter("limit", "", "100")

	var results []payment.PaymentIntentSummary
	iter := paymentintent.List(params)
	for iter.Next() {
		pi := iter.PaymentIntent()
		orderID := ""
		if pi.Metadata != nil {
			orderID = pi.Metadata["order_id"]
		}
		results = append(results, payment.PaymentIntentSummary{
			StripeID:    pi.ID,
			Status:      string(pi.Status),
			AmountCents: int(pi.Amount),
			Currency:    string(pi.Currency),
			OrderID:     orderID,
		})
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("stripe.ListPaymentIntents: %w", err)
	}
	return results, nil
}

// ConstructWebhookEvent validates the Stripe-Signature header and parses the event body.
// Returns ErrAlreadyProcessed if the signature is invalid — callers should return 400.
func (c *Client) ConstructWebhookEvent(body []byte, signature string) (*payment.WebhookEvent, error) {
	// IgnoreAPIVersionMismatch: the Stripe CLI forwards events using the account's
	// API version, which may differ from the stripe-go SDK version. Safe for local
	// development — in production, pin your webhook endpoint to the SDK's API version.
	event, err := stripewebhook.ConstructEventWithOptions(body, signature, c.webhookSecret,
		stripewebhook.ConstructEventOptions{IgnoreAPIVersionMismatch: true},
	)
	if err != nil {
		return nil, fmt.Errorf("stripe.ConstructWebhookEvent invalid signature: %w", err)
	}

	raw, err := json.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("stripe.ConstructWebhookEvent marshal: %w", err)
	}

	return &payment.WebhookEvent{
		ID:      event.ID,
		Type:    string(event.Type),
		Payload: raw,
	}, nil
}
