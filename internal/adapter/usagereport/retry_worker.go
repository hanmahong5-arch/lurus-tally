package usagereport

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/platform"
)

const (
	// usageRetryPollInterval is slower than the NATS outbox — retries are not
	// latency-critical, and a NULL-account row only heals once onboarding
	// re-runs, which is minutes-to-hours, not seconds.
	usageRetryPollInterval = 60 * time.Second
	usageRetryDrainLimit   = 100
)

// RetryWorker drains the durable usage-report outbox, RE-RESOLVES each tenant's
// platform account (so an event queued while the tenant was unprovisioned is
// back-reported once onboarding heals), and re-POSTs to the platform metering
// ingest with a stable idempotency key. Mirrors nats.OutboxWorker but targets
// the HTTP ingest instead of NATS. Dedicated (not the NATS worker) to keep the
// PSI_EVENTS business stream single-purpose.
type RetryWorker struct {
	store    UsageOutbox
	poster   EventPoster
	resolver AccountResolver
	timeout  time.Duration
	log      *slog.Logger
}

// NewRetryWorker builds the worker. Reuses the SAME poster + resolver the
// Reporter receives — no new adapters.
func NewRetryWorker(store UsageOutbox, poster EventPoster, resolver AccountResolver, cfg Config) *RetryWorker {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &RetryWorker{store: store, poster: poster, resolver: resolver, timeout: timeout, log: log}
}

// Run loops until ctx is cancelled, draining every usageRetryPollInterval.
// Drains once on startup so a recent backlog clears before the first tick.
func (w *RetryWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(usageRetryPollInterval)
	defer ticker.Stop()

	w.drainOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.drainOnce(ctx)
		}
	}
}

func (w *RetryWorker) drainOnce(ctx context.Context) {
	// Refresh gauges from the pre-drain backlog — the most actionable on-call signal.
	if stats, err := w.store.PendingStats(ctx); err == nil {
		usageRetryPending.Set(float64(stats.PendingCount))
		usageRetryOldestAge.Set(stats.OldestAgeSeconds)
	}

	rows, err := w.store.Drain(ctx, usageRetryDrainLimit)
	if err != nil {
		w.log.Error("usage retry: drain failed", slog.String("error", err.Error()))
		return
	}
	for _, row := range rows {
		w.processRow(ctx, row)
	}
}

func (w *RetryWorker) processRow(ctx context.Context, row PendingUsageRow) {
	// Bound the network legs (resolve + post); the store writes use the parent
	// ctx so a slow POST can't cost us the MarkSent / RecordAttemptError.
	netCtx, cancel := context.WithTimeout(ctx, w.timeout)
	defer cancel()

	// Re-resolve FRESH (never the Reporter's positive cache) so a NULL-account
	// row succeeds once the tenant is provisioned.
	accountID, ok, err := w.resolver.GetPlatformAccountID(netCtx, row.TenantID)
	if err != nil {
		w.recordError(ctx, row, fmt.Sprintf("resolve: %v", err))
		return
	}
	if !ok {
		w.recordError(ctx, row, "still no platform account for tenant")
		return
	}

	req := platform.UsageEventRequest{
		AccountID:      accountID,
		ProductID:      ProductID,
		Metric:         MetricLLMTokens,
		Quantity:       int64(row.PromptTokens + row.CompletionTokens),
		OccurredAt:     row.OccurredAt,
		IdempotencyKey: usageIdemKey(row.ID),
		Metadata: map[string]any{
			"tenant_id":         row.TenantID.String(),
			"model":             row.Model,
			"prompt_tokens":     row.PromptTokens,
			"completion_tokens": row.CompletionTokens,
			"source":            "ai_assistant",
			"retry_reason":      row.Reason,
		},
	}
	if err := w.poster.ReportUsageEvent(netCtx, req); err != nil {
		w.recordError(ctx, row, fmt.Sprintf("post: %v", err))
		return
	}
	if err := w.store.MarkSent(ctx, row.ID); err != nil {
		// Posted but not marked → next tick re-posts; platform dedups on the
		// stable idempotency key, so this never double-counts.
		w.log.Error("usage retry: posted but MarkSent failed (will re-post, deduped)",
			slog.String("id", row.ID.String()), slog.String("error", err.Error()))
		return
	}
	metricPosted.Inc()
}

// recordError increments attempts and fires the exhausted alert signal when the
// row crosses the cap (it then stays in the table for ops inspection).
func (w *RetryWorker) recordError(ctx context.Context, row PendingUsageRow, msg string) {
	attempts, err := w.store.RecordAttemptError(ctx, row.ID, msg)
	if err != nil {
		w.log.Error("usage retry: record attempt error failed",
			slog.String("id", row.ID.String()), slog.String("error", err.Error()))
		return
	}
	if attempts >= MaxUsageOutboxAttempts {
		metricRetryExhausted.Inc()
		w.log.Warn("usage retry: event exhausted attempts; billable usage at risk",
			slog.String("id", row.ID.String()),
			slog.String("reason", row.Reason),
			slog.String("last_error", msg))
	}
}
