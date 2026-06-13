// Package llmgateway centralises LLM observability and (future) rate-limit/
// SQL-guard concerns. Story S0.Q2 lands the first surface: Prometheus
// counters for token + CNY spend, exposed at /internal/v1/metrics.
package llmgateway

import (
	"context"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const unknownTenant = "unknown"

type ctxKey struct{}

// WithTenant returns a child context tagged with the tenant id. LLM
// orchestration code MUST call this before invoking the llmclient so that
// RecordUsage attributes spend to the correct tenant label.
func WithTenant(ctx context.Context, tenant string) context.Context {
	if tenant == "" {
		return ctx
	}
	return context.WithValue(ctx, ctxKey{}, tenant)
}

// TenantFrom extracts the tenant tag previously installed by WithTenant.
// Returns "unknown" when absent so that metrics remain queryable but the
// missing-tenant case is observable as its own label value.
func TenantFrom(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKey{}).(string); ok && v != "" {
		return v
	}
	return unknownTenant
}

var (
	llmCostCNY = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tally_llm_cost_cny_total",
			Help: "Total LLM spend in CNY, by tenant and model. Computed from token usage via the pricing table.",
		},
		[]string{"tenant", "model"},
	)
	llmTokens = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tally_llm_tokens_total",
			Help: "LLM token usage, by tenant, model, and direction (in|out).",
		},
		[]string{"tenant", "model", "direction"},
	)
)

func init() {
	prometheus.MustRegister(llmCostCNY, llmTokens)
}

// RecordUsage attributes one LLM completion to the correct tenant+model.
// Safe to call with zero token counts and unknown models.
func RecordUsage(ctx context.Context, model string, promptTokens, completionTokens int) {
	tenant := TenantFrom(ctx)
	m := modelLabel(model)

	if promptTokens > 0 {
		llmTokens.WithLabelValues(tenant, m, "in").Add(float64(promptTokens))
	}
	if completionTokens > 0 {
		llmTokens.WithLabelValues(tenant, m, "out").Add(float64(completionTokens))
	}

	// Cost stays at 0 for unknown models — surfaces the cardinality gap without
	// silently miscounting CNY.
	if cost := costCNYFor(model, promptTokens, completionTokens); cost > 0 {
		llmCostCNY.WithLabelValues(tenant, m).Add(cost)
	}
}

// Handler returns the /internal/v1/metrics http.Handler. Callers wire this
// into the internal route group; auth (INTERNAL_API_KEY) belongs at the
// router layer, not here, so this stays plain promhttp.
func Handler() http.Handler {
	return promhttp.Handler()
}
