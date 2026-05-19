package llmgateway

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestRecordUsage_KnownModel_IncrementsBothCounters(t *testing.T) {
	resetCounters(t)

	ctx := WithTenant(context.Background(), "tenant-A")
	RecordUsage(ctx, "deepseek-v4", 100, 50)

	gotIn := testutil.ToFloat64(llmTokens.WithLabelValues("tenant-A", "deepseek-v4", "in"))
	if gotIn != 100 {
		t.Errorf("tokens_total in = %v, want 100", gotIn)
	}
	gotOut := testutil.ToFloat64(llmTokens.WithLabelValues("tenant-A", "deepseek-v4", "out"))
	if gotOut != 50 {
		t.Errorf("tokens_total out = %v, want 50", gotOut)
	}

	// 100 * 0.002/1000 + 50 * 0.008/1000 = 0.0002 + 0.0004 = 0.0006 CNY
	gotCost := testutil.ToFloat64(llmCostCNY.WithLabelValues("tenant-A", "deepseek-v4"))
	const wantCost = 0.0006
	if abs(gotCost-wantCost) > 1e-9 {
		t.Errorf("cost_cny = %v, want %v", gotCost, wantCost)
	}
}

func TestRecordUsage_UnknownModel_FallsBackToUnknownLabel_ZeroCost(t *testing.T) {
	resetCounters(t)

	RecordUsage(context.Background(), "mystery-model-xyz", 200, 100)

	gotIn := testutil.ToFloat64(llmTokens.WithLabelValues("unknown", "unknown", "in"))
	if gotIn != 200 {
		t.Errorf("unknown-model tokens_total in = %v, want 200", gotIn)
	}
	gotCost := testutil.ToFloat64(llmCostCNY.WithLabelValues("unknown", "unknown"))
	if gotCost != 0 {
		t.Errorf("unknown-model cost = %v, want 0 (no pricing entry)", gotCost)
	}
}

func TestRecordUsage_ZeroTokens_NoOp(t *testing.T) {
	resetCounters(t)

	RecordUsage(WithTenant(context.Background(), "tenant-B"), "deepseek-v4", 0, 0)

	// Counters are not even instantiated for zero usage — Collect should yield
	// no sample lines for this label set.
	if got := testutil.ToFloat64(llmCostCNY.WithLabelValues("tenant-B", "deepseek-v4")); got != 0 {
		t.Errorf("cost should remain 0 on zero tokens, got %v", got)
	}
}

func TestRecordUsage_MissingTenant_UsesUnknownLabel(t *testing.T) {
	resetCounters(t)

	// No WithTenant call — TenantFrom should resolve to "unknown".
	RecordUsage(context.Background(), "deepseek-v4", 10, 5)

	got := testutil.ToFloat64(llmTokens.WithLabelValues("unknown", "deepseek-v4", "in"))
	if got != 10 {
		t.Errorf("missing-tenant defaulted to unknown: tokens_in = %v, want 10", got)
	}
}

func TestHandler_ExposesPrometheusFormat(t *testing.T) {
	resetCounters(t)
	RecordUsage(WithTenant(context.Background(), "scrape-tenant"), "deepseek-v4", 7, 3)

	req := httptest.NewRequest("GET", "/internal/v1/metrics", nil)
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, want := range []string{
		"tally_llm_tokens_total",
		"tally_llm_cost_cny_total",
		`tenant="scrape-tenant"`,
		`model="deepseek-v4"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("scrape output missing %q\nfull body:\n%s", want, body)
		}
	}
}

// --- helpers ---

// resetCounters wipes any series accumulated by prior tests so each subtest
// starts from zero. Necessary because the registry is process-global.
func resetCounters(t *testing.T) {
	t.Helper()
	llmTokens.Reset()
	llmCostCNY.Reset()
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
