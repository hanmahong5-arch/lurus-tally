package usagereport

import "github.com/prometheus/client_golang/prometheus"

// Reporter observability — surfaced at /internal/v1/metrics alongside the LLM
// counters. These make the shadow rollout auditable: posted vs skipped vs
// dropped is exactly what the revenue dashboard needs to trust the numbers
// before flipping a product to enforce.
var (
	metricEnqueued = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "tally_usage_events_enqueued_total",
		Help: "LLM usage events accepted into the reporter queue.",
	})
	metricDropped = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "tally_usage_events_dropped_total",
		Help: "LLM usage events dropped because the reporter queue was full.",
	})
	metricPosted = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "tally_usage_events_posted_total",
		Help: "LLM usage events successfully posted to the platform metering ingest.",
	})
	metricPostErr = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "tally_usage_events_post_errors_total",
		Help: "LLM usage events that failed to post to platform (shadow: dropped).",
	})
	// metricSkipped is labelled by reason so unprovisioned-tenant skips are
	// distinguishable from resolver errors and missing-tenant attribution.
	// Used now only for TERMINAL drops (no_tenant/bad_tenant) and the degraded
	// no-durable-store fallback; recoverable failures go to metricQueuedForRetry.
	metricSkipped = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "tally_usage_events_skipped_total",
		Help: "LLM usage events skipped before posting, by reason.",
	}, []string{"reason"})

	// metricQueuedForRetry counts events durably queued (not dropped) when a
	// recoverable failure occurred — labelled by reason so the dashboard shows
	// WHY usage is pending (no_account | resolve_error | post_error).
	metricQueuedForRetry = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "tally_usage_events_queued_for_retry_total",
		Help: "LLM usage events durably queued for retry, by reason.",
	}, []string{"reason"})

	// metricRetryExhausted is the ALERT signal: a queued event hit the attempts
	// cap and stopped retrying (billable usage at risk of permanent loss).
	metricRetryExhausted = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "tally_usage_events_retry_exhausted_total",
		Help: "Durably-queued usage events that hit the attempts cap and stopped retrying.",
	})

	// metricEnqueueFailed is true loss: platform AND the local DB were both
	// unreachable, so the event could not even be durably queued.
	metricEnqueueFailed = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "tally_usage_events_enqueue_failed_total",
		Help: "Usage events that could not be durably queued (platform AND DB down).",
	})

	// usageRetryPending / usageRetryOldestAge mirror the NATS outbox gauges for
	// the usage retry queue, refreshed each retry-worker tick.
	usageRetryPending = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "tally_usage_retry_pending_count",
		Help: "Durably-queued usage events awaiting retry (sent_at IS NULL, attempts < cap).",
	})
	usageRetryOldestAge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "tally_usage_retry_oldest_age_seconds",
		Help: "Age of the oldest pending usage-retry row (0 when none).",
	})
)

func init() {
	prometheus.MustRegister(
		metricEnqueued, metricDropped, metricPosted, metricPostErr, metricSkipped,
		metricQueuedForRetry, metricRetryExhausted, metricEnqueueFailed,
		usageRetryPending, usageRetryOldestAge,
	)
}
