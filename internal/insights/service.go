// Package insights provides AI-powered payment analytics using the Anthropic API.
package insights

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/redis/go-redis/v9"

	"github.com/alexkua/payflow/internal/domain/payment"
	"github.com/alexkua/payflow/internal/domain/reconciliation"
)

const (
	cacheKey = "insights:summary:7d"
	cacheTTL = 30 * time.Minute
	period   = "last_7_days"
)

// Stats holds aggregated payment metrics for a time period.
type Stats struct {
	TotalTransactions  int     `json:"total_transactions"`
	TotalVolumeCents   int     `json:"total_volume_cents"`
	SuccessRate        float64 `json:"success_rate"`
	FailureRate        float64 `json:"failure_rate"`
	RefundRate         float64 `json:"refund_rate"`
	AvgTransactionCents int    `json:"avg_transaction_cents"`
}

// Summary is the full AI-generated insights response.
type Summary struct {
	GeneratedAt time.Time `json:"generated_at"`
	Period      string    `json:"period"`
	Cached      bool      `json:"cached"`
	Stats       Stats     `json:"stats"`
	Anomalies   []string  `json:"anomalies"`
	AISummary   string    `json:"ai_summary"`
}

// Service generates AI-powered payment insights.
type Service struct {
	paymentStore   payment.Store
	reconcileStore reconciliation.Store
	rdb            *redis.Client
	anthropic      *anthropic.Client
	logger         *slog.Logger
}

// NewService creates an insights Service. If apiKey is empty the service still
// works but returns an error when the Claude call is attempted.
func NewService(
	paymentStore payment.Store,
	reconcileStore reconciliation.Store,
	rdb *redis.Client,
	apiKey string,
	logger *slog.Logger,
) *Service {
	var client *anthropic.Client
	if apiKey != "" {
		c := anthropic.NewClient(option.WithAPIKey(apiKey))
		client = &c
	}
	return &Service{
		paymentStore:   paymentStore,
		reconcileStore: reconcileStore,
		rdb:            rdb,
		anthropic:      client,
		logger:         logger,
	}
}

// GenerateSummary returns a cached summary if available, otherwise fetches
// payment data, aggregates stats, calls Claude, caches, and returns the result.
// Pass refresh=true to bypass the cache.
func (s *Service) GenerateSummary(ctx context.Context, refresh bool) (*Summary, error) {
	if !refresh {
		if cached, err := s.loadCache(ctx); err == nil {
			cached.Cached = true
			return cached, nil
		}
	}

	now := time.Now().UTC()
	from := now.AddDate(0, 0, -7)

	payments, err := s.paymentStore.ListPaymentsByDateRange(ctx, from, now)
	if err != nil {
		return nil, fmt.Errorf("insights: fetch payments: %w", err)
	}

	stats, anomalies := s.aggregate(payments)

	recentRuns, err := s.reconcileStore.ListRuns(ctx, 3)
	if err != nil {
		s.logger.Error("insights: fetch reconciliation runs", "error", err)
		// Non-fatal — continue without reconciliation context.
	}

	aiSummary, err := s.callClaude(ctx, stats, anomalies, recentRuns)
	if err != nil {
		return nil, fmt.Errorf("insights: claude call: %w", err)
	}

	result := &Summary{
		GeneratedAt: now,
		Period:      period,
		Cached:      false,
		Stats:       stats,
		Anomalies:   anomalies,
		AISummary:   aiSummary,
	}

	if err := s.saveCache(ctx, result); err != nil {
		s.logger.Error("insights: save cache", "error", err)
	}

	return result, nil
}

// aggregate computes payment stats and detects simple anomalies.
func (s *Service) aggregate(payments []*payment.Payment) (Stats, []string) {
	var stats Stats
	stats.TotalTransactions = len(payments)

	if len(payments) == 0 {
		return stats, nil
	}

	captured, failed, refunded := 0, 0, 0
	// Track failure reasons to surface patterns.
	failureReasonCounts := make(map[string]int)

	for _, p := range payments {
		stats.TotalVolumeCents += p.AmountCents
		switch p.Status {
		case payment.StatusCaptured:
			captured++
		case payment.StatusFailed:
			failed++
			if p.FailureReason != "" {
				failureReasonCounts[p.FailureReason]++
			}
		case payment.StatusRefunded:
			refunded++
		}
	}

	total := float64(len(payments))
	stats.SuccessRate = float64(captured) / total
	stats.FailureRate = float64(failed) / total
	stats.RefundRate = float64(refunded) / total
	stats.AvgTransactionCents = stats.TotalVolumeCents / len(payments)

	var anomalies []string

	// Flag any failure reason that accounts for more than 20% of failures.
	for reason, count := range failureReasonCounts {
		if failed > 0 && float64(count)/float64(failed) >= 0.2 {
			anomalies = append(anomalies,
				fmt.Sprintf("%d failed payments share the same reason: %q", count, reason))
		}
	}

	// Flag elevated failure rate.
	if stats.FailureRate > 0.10 {
		anomalies = append(anomalies,
			fmt.Sprintf("Failure rate is elevated at %.1f%% (threshold: 10%%)", stats.FailureRate*100))
	}

	// Flag elevated refund rate.
	if stats.RefundRate > 0.05 {
		anomalies = append(anomalies,
			fmt.Sprintf("Refund rate is elevated at %.1f%% (threshold: 5%%)", stats.RefundRate*100))
	}

	return stats, anomalies
}

// callClaude sends aggregated stats to Claude and returns a narrative summary.
func (s *Service) callClaude(ctx context.Context, stats Stats, anomalies []string, runs []*reconciliation.Run) (string, error) {
	if s.anthropic == nil {
		return "", errors.New("insights: ANTHROPIC_API_KEY is not configured")
	}

	reconcileContext := "No recent reconciliation runs."
	if len(runs) > 0 {
		latest := runs[0]
		reconcileContext = fmt.Sprintf(
			"Latest reconciliation run: %d matched, %d mismatched, %d missing locally, %d missing from Stripe.",
			latest.Matched, latest.Mismatched, latest.MissingLocal, latest.MissingStripe,
		)
	}

	anomalyContext := "No anomalies detected."
	if len(anomalies) > 0 {
		anomalyContext = fmt.Sprintf("Detected anomalies: %v", anomalies)
	}

	prompt := fmt.Sprintf(`You are an AI assistant analyzing payment data for a payment processing platform.

Here are the payment statistics for the last 7 days:
- Total transactions: %d
- Total volume: $%.2f
- Success rate: %.1f%%
- Failure rate: %.1f%%
- Refund rate: %.1f%%
- Average transaction: $%.2f

Reconciliation: %s

Anomalies: %s

Write a concise 2-3 sentence summary of the payment activity. Be direct and specific with numbers.
If there are anomalies, mention them briefly. Keep a professional, data-driven tone.`,
		stats.TotalTransactions,
		float64(stats.TotalVolumeCents)/100,
		stats.SuccessRate*100,
		stats.FailureRate*100,
		stats.RefundRate*100,
		float64(stats.AvgTransactionCents)/100,
		reconcileContext,
		anomalyContext,
	)

	msg, err := s.anthropic.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeHaiku4_5,
		MaxTokens: 300,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		return "", fmt.Errorf("insights: anthropic API: %w", err)
	}

	if len(msg.Content) == 0 {
		return "", errors.New("insights: empty response from Claude")
	}

	return msg.Content[0].Text, nil
}

func (s *Service) loadCache(ctx context.Context) (*Summary, error) {
	data, err := s.rdb.Get(ctx, cacheKey).Bytes()
	if err != nil {
		return nil, err
	}
	var summary Summary
	if err := json.Unmarshal(data, &summary); err != nil {
		return nil, err
	}
	return &summary, nil
}

func (s *Service) saveCache(ctx context.Context, summary *Summary) error {
	data, err := json.Marshal(summary)
	if err != nil {
		return err
	}
	return s.rdb.Set(ctx, cacheKey, data, cacheTTL).Err()
}
