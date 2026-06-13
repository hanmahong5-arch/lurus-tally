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
	metricSkipped = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "tally_usage_events_skipped_total",
		Help: "LLM usage events skipped before posting, by reason.",
	}, []string{"reason"})
)

func init() {
	prometheus.MustRegister(metricEnqueued, metricDropped, metricPosted, metricPostErr, metricSkipped)
}
