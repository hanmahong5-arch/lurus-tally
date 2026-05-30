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

// AI / north-star counters.
//   - wadTotal       : Weekly Active Decisions — AI-confirmed purchase drafts (the north star).
//   - aiPlanExecuted : all confirmed AI plan executions, by type (create_purchase_draft|price_change|bulk_stock_adjust).
//
// Both carry an optional tenant_id label (perTenantEnabled).
var (
	wadTotal       *prometheus.CounterVec
	aiPlanExecuted *prometheus.CounterVec
)

// webTelemetry counts browser product-telemetry events received at
// /internal/v1/telemetry/web, by event name. Always unlabelled by tenant —
// the event-name cardinality is bounded by the allow-list; per-user DAU is
// derived downstream from the PSI_TELEMETRY.web.* stream, not this counter.
var webTelemetry = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "tally_web_telemetry_total",
		Help: "Browser product-telemetry events received, by event name (palette_invocation|ai_drawer_open|plan_accept_rate|...).",
	},
	[]string{"event"},
)

// planAccept counts AI-plan confirm/cancel decisions split by outcome so the
// KS2 kill-switch (AI suggestion adoption rate) is computable as
// tally_plan_accept_total{accepted="1"} / sum(tally_plan_accept_total).
// The label is bounded to {"1","0","unknown"} by IncPlanAccept — never the
// raw client value — so cardinality stays fixed regardless of payload drift.
var planAccept = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "tally_plan_accept_total",
		Help: "AI plan decisions, by accepted (1=confirmed|0=cancelled|unknown). KS2 = accepted=1 / sum.",
	},
	[]string{"accepted"},
)

// tenantSignups is the authoritative onboarding denominator: one increment per
// brand-new tenant bootstrap (not per login). KS1 onboarding-completion rate =
// tally_web_telemetry_total{event="onboarding_first_po_exported"} / sum(tally_tenant_signups_total).
// Labelled by profile_type (cross_border|retail|horticulture) — bounded
// cardinality — so signups can also be sliced by persona.
var tenantSignups = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "tally_tenant_signups_total",
		Help: "Brand-new tenant bootstraps, by profile_type. KS1 denominator.",
	},
	[]string{"profile_type"},
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

	aiLabels := []string{"type"}
	wadLabels := []string{}
	if perTenantEnabled {
		aiLabels = append(aiLabels, "tenant_id")
		wadLabels = append(wadLabels, "tenant_id")
	}
	aiPlanExecuted = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tally_ai_plan_executed_total",
			Help: "Confirmed AI plan executions, by type and optionally tenant_id.",
		},
		aiLabels,
	)
	wadTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tally_wad_total",
			Help: "North star — Weekly Active Decisions: AI-confirmed purchase drafts, optionally by tenant_id.",
		},
		wadLabels,
	)

	prometheus.MustRegister(
		billApproved,
		billCancelled,
		paymentCreated,
		stockMovement,
		outboxPendingCount,
		outboxOldestAgeSeconds,
		aiPlanExecuted,
		wadTotal,
		webTelemetry,
		planAccept,
		tenantSignups,
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

// IncAIPlanExecuted increments tally_ai_plan_executed_total for a confirmed AI
// plan, by plan type (create_purchase_draft|price_change|bulk_stock_adjust).
func IncAIPlanExecuted(planType, tenantID string) {
	if perTenantEnabled {
		aiPlanExecuted.WithLabelValues(planType, tenantID).Inc()
		return
	}
	aiPlanExecuted.WithLabelValues(planType).Inc()
}

// IncWAD increments tally_wad_total — one Weekly Active Decision per AI-confirmed
// purchase draft. This is the product north star.
func IncWAD(tenantID string) {
	if perTenantEnabled {
		wadTotal.WithLabelValues(tenantID).Inc()
		return
	}
	wadTotal.WithLabelValues().Inc()
}

// IncWebTelemetry increments tally_web_telemetry_total for one accepted browser
// telemetry event (event name must already be allow-listed by the caller).
func IncWebTelemetry(event string) {
	webTelemetry.WithLabelValues(event).Inc()
}

// IncPlanAccept records one AI-plan decision for KS2. The accepted argument is
// normalized at this boundary to one of {"1","0","unknown"} so an unexpected
// or missing client value can never explode the metric's label cardinality.
func IncPlanAccept(accepted string) {
	switch accepted {
	case "1", "0":
		// keep as-is
	default:
		accepted = "unknown"
	}
	planAccept.WithLabelValues(accepted).Inc()
}

// IncTenantSignup records one brand-new tenant bootstrap for the KS1
// onboarding-completion denominator. Call exactly once per fresh tenant (the
// created path), never on returning-user logins, so the count stays a true
// signup tally. profileType is the chosen persona (cross_border|retail|horticulture).
func IncTenantSignup(profileType string) {
	tenantSignups.WithLabelValues(profileType).Inc()
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
