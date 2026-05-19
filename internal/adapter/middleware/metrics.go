package middleware

import "github.com/prometheus/client_golang/prometheus"

var idempotencySkipped = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "tally_idempotency_skipped_total",
		Help: "Idempotency middleware decisions to skip dedup, by reason (no_tenant|wrong_type|no_key).",
	},
	[]string{"reason"},
)

func init() {
	prometheus.MustRegister(idempotencySkipped)
}
