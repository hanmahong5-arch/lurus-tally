package middleware

import (
	"os"

	"github.com/prometheus/client_golang/prometheus"
)

// perTenantEnabled controls whether counters that carry a tenant_id label are
// registered. Set TALLY_METRICS_PER_TENANT=false to collapse all tenant series
// into a single unlabelled metric — useful in high-cardinality production
// environments where the tenant count would otherwise explode the metric DB.
var perTenantEnabled = os.Getenv("TALLY_METRICS_PER_TENANT") != "false"

var idempotencySkipped = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "tally_idempotency_skipped_total",
		Help: "Idempotency middleware decisions to skip dedup, by reason (no_tenant|wrong_type|no_key).",
	},
	[]string{"reason"},
)

// Bill lifecycle counters. Each carries a "type" label (purchase|sale) and,
// when perTenantEnabled, a "tenant_id" label.
var (
	billApproved  *prometheus.CounterVec
	billCancelled *prometheus.CounterVec
)

// Payment counter. Carries a "currency" label and, when perTenantEnabled, a "tenant_id" label.
var paymentCreated *prometheus.CounterVec

// Stock movement counter. Carries a "direction" label (in|out) and, when
// perTenantEnabled, a "tenant_id" label.
var stockMovement *prometheus.CounterVec

// Outbox gauges are always unlabelled (they represent global service state, not per-tenant).
var (
	outboxPendingCount     prometheus.Gauge
	outboxOldestAgeSeconds prometheus.Gauge
)

func init() {
	prometheus.MustRegister(idempotencySkipped)

	billLabels := []string{"type"}
	paymentLabels := []string{"currency"}
	stockLabels := []string{"direction"}
	if perTenantEnabled {
		billLabels = append(billLabels, "tenant_id")
		paymentLabels = append(paymentLabels, "tenant_id")
		stockLabels = append(stockLabels, "tenant_id")
	}

	billApproved = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tally_bill_approved_total",
			Help: "Total bills approved, by type (purchase|sale) and optionally tenant_id.",
		},
		billLabels,
	)
	billCancelled = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tally_bill_cancelled_total",
			Help: "Total bills cancelled, by type (purchase|sale) and optionally tenant_id.",
		},
		billLabels,
	)
	paymentCreated = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tally_payment_created_total",
			Help: "Total payments recorded, by currency and optionally tenant_id.",
		},
		paymentLabels,
	)
	stockMovement = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tally_stock_movement_total",
			Help: "Total stock movements, by direction (in|out) and optionally tenant_id.",
		},
		stockLabels,
	)

	outboxPendingCount = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "tally_outbox_pending_count",
		Help: "Number of unpublished outbox rows (published_at IS NULL, attempts < max).",
	})
	outboxOldestAgeSeconds = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "tally_outbox_oldest_age_seconds",
		Help: "Age in seconds of the oldest unpublished outbox row (0 when queue is empty).",
	})

	prometheus.MustRegister(
		billApproved,
		billCancelled,
		paymentCreated,
		stockMovement,
		outboxPendingCount,
		outboxOldestAgeSeconds,
	)
}

// billLabelValues constructs the label values slice for bill counters.
// The order matches the label names registered in init (type, [tenant_id]).
func billLabelValues(billType, tenantID string) []string {
	if perTenantEnabled {
		return []string{billType, tenantID}
	}
	return []string{billType}
}

// paymentLabelValues constructs label values for the payment counter.
func paymentLabelValues(currency, tenantID string) []string {
	if perTenantEnabled {
		return []string{currency, tenantID}
	}
	return []string{currency}
}

// stockLabelValues constructs label values for the stock movement counter.
func stockLabelValues(direction, tenantID string) []string {
	if perTenantEnabled {
		return []string{direction, tenantID}
	}
	return []string{direction}
}

// IncBillApproved increments tally_bill_approved_total for the given bill type and tenant.
// billType must be "purchase" or "sale". tenantID is a UUID string.
func IncBillApproved(billType, tenantID string) {
	billApproved.WithLabelValues(billLabelValues(billType, tenantID)...).Inc()
}

// IncBillCancelled increments tally_bill_cancelled_total for the given bill type and tenant.
func IncBillCancelled(billType, tenantID string) {
	billCancelled.WithLabelValues(billLabelValues(billType, tenantID)...).Inc()
}

// IncPaymentCreated increments tally_payment_created_total for the given currency and tenant.
func IncPaymentCreated(currency, tenantID string) {
	paymentCreated.WithLabelValues(paymentLabelValues(currency, tenantID)...).Inc()
}

// IncStockMovement increments tally_stock_movement_total for the given direction and tenant.
// direction must be "in" or "out".
func IncStockMovement(direction, tenantID string) {
	stockMovement.WithLabelValues(stockLabelValues(direction, tenantID)...).Inc()
}

// SetOutboxPending sets tally_outbox_pending_count to the supplied count.
// Called each outbox worker tick after querying SELECT COUNT(*) from the pending rows.
func SetOutboxPending(count float64) {
	outboxPendingCount.Set(count)
}

// SetOutboxOldestAge sets tally_outbox_oldest_age_seconds to the supplied age.
// Pass 0 when the outbox is empty.
func SetOutboxOldestAge(ageSeconds float64) {
	outboxOldestAgeSeconds.Set(ageSeconds)
}
