//go:build integration

// Package integration — Prometheus metrics scrape tests (WAD / AI plan / web telemetry).
//
// Approach: minimal harness (approach b).  The full lifecycle.NewApp requires a
// real Redis + real LLM connection to wire the AI handler.  Since ConfirmPlan
// (the only call site for IncWAD / IncAIPlanExecuted) is gated behind the
// orchestrator, triggering it end-to-end without a running LLM service would
// require a test double that is out of scope for this file boundary.
//
// Instead the harness:
//  1. Builds a gin engine with GET /internal/v1/metrics wired to MetricsHandler
//     (no auth gate — expectedKey is "").
//  2. Builds a gin engine with POST /internal/v1/telemetry/web wired to the real
//     TelemetryHandler backed by a noop NATS publisher.
//  3. Calls middleware.IncWAD / middleware.IncAIPlanExecuted / middleware.IncWebTelemetry
//     directly — these are the exact same exported functions that production
//     handlers call — then scrapes /internal/v1/metrics to verify exposition.
//
// Note: the Prometheus default registry accumulates state across tests in the
// same process run.  Pre/post deltas (not absolute values) are used throughout
// so that test ordering does not affect correctness.
package integration

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	handlermetrics "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/metrics"
	handlertelemetry "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/telemetry"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	adapternats "github.com/hanmahong5-arch/lurus-tally/internal/adapter/nats"
)

// metricsEngine returns a gin engine that serves GET /internal/v1/metrics
// with no bearer-token gate (expectedKey == "").
func metricsEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	mh := handlermetrics.NewMetricsHandler("")
	r.GET("/internal/v1/metrics", mh.Serve)
	return r
}

// telemetryEngine returns a gin engine with the real telemetry handler mounted.
// It uses a noop NATS publisher and an empty bearer key (dev mode, no auth).
func telemetryEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	pub, _ := adapternats.NewPublisher(adapternats.Config{NoOpFallback: true})
	th := handlertelemetry.New(pub, "", "anonymous", nil)
	th.Register(r)
	return r
}

// scrapeMetrics performs GET /internal/v1/metrics against the provided engine
// and returns the response body as a string.
func scrapeMetrics(t *testing.T, engine *gin.Engine) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/internal/v1/metrics", nil)
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("scrape /internal/v1/metrics: unexpected status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("scrape: read body: %v", err)
	}
	return string(body)
}

// parseCounter extracts the first numeric value of a counter line matching the
// given metric name and label pair from a Prometheus text-format scrape body.
// Returns 0 and false when not found.
func parseCounter(body, metricName string, labels map[string]string) (float64, bool) {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.HasPrefix(line, metricName) {
			continue
		}
		// Check all required labels appear in the line.
		match := true
		for k, v := range labels {
			needle := k + `="` + v + `"`
			if !strings.Contains(line, needle) {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		// Value is the last field.
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		var val float64
		if _, err := fmt.Sscanf(parts[len(parts)-1], "%g", &val); err == nil {
			return val, true
		}
	}
	return 0, false
}

// TestMetrics_WAD_IncrementsOnPlanConfirm verifies that tally_wad_total
// increments by exactly 1 when middleware.IncWAD is called.
//
// Production trigger: ConfirmPlan handler → middleware.IncWAD(tenantID) when
// result.BillID != nil (i.e. a purchase draft was created).
// Harness approach: call middleware.IncWAD directly — same code path as handler.
func TestMetrics_WAD_IncrementsOnPlanConfirm(t *testing.T) {
	engine := metricsEngine()
	tenantID := "test-tenant-wad-001"

	pre, _ := parseCounter(scrapeMetrics(t, engine), "tally_wad_total", map[string]string{"tenant_id": tenantID})
	t.Logf("tally_wad_total{tenant_id=%q} pre:  %.0f", tenantID, pre)

	middleware.IncWAD(tenantID)

	post, ok := parseCounter(scrapeMetrics(t, engine), "tally_wad_total", map[string]string{"tenant_id": tenantID})
	t.Logf("tally_wad_total{tenant_id=%q} post: %.0f", tenantID, post)

	if !ok {
		t.Fatalf("FAIL: tally_wad_total{tenant_id=%q} not found in scrape after IncWAD", tenantID)
	}
	if post != pre+1 {
		t.Errorf("FAIL: expected tally_wad_total delta +1, got pre=%.0f post=%.0f", pre, post)
	} else {
		t.Logf("PASS: tally_wad_total incremented %.0f → %.0f (+1)", pre, post)
	}
}

// TestMetrics_AIPlanExecuted_ByTypeLabel verifies that tally_ai_plan_executed_total
// increments per distinct type label.
//
// Production trigger: ConfirmPlan handler → middleware.IncAIPlanExecuted(planType, tenantID).
// Harness approach: call middleware.IncAIPlanExecuted directly.
func TestMetrics_AIPlanExecuted_ByTypeLabel(t *testing.T) {
	engine := metricsEngine()
	tenantID := "test-tenant-plan-002"

	// Read pre-increment baselines for all three plan types.
	scrape0 := scrapeMetrics(t, engine)
	preDraft, _ := parseCounter(scrape0, "tally_ai_plan_executed_total", map[string]string{"type": "create_purchase_draft", "tenant_id": tenantID})
	prePrice, _ := parseCounter(scrape0, "tally_ai_plan_executed_total", map[string]string{"type": "price_change", "tenant_id": tenantID})
	preBulk, _ := parseCounter(scrape0, "tally_ai_plan_executed_total", map[string]string{"type": "bulk_stock_adjust", "tenant_id": tenantID})

	t.Logf("pre: create_purchase_draft=%.0f  price_change=%.0f  bulk_stock_adjust=%.0f", preDraft, prePrice, preBulk)

	// Trigger one of each type.
	middleware.IncAIPlanExecuted("create_purchase_draft", tenantID)
	middleware.IncAIPlanExecuted("price_change", tenantID)
	middleware.IncAIPlanExecuted("bulk_stock_adjust", tenantID)

	scrape1 := scrapeMetrics(t, engine)
	postDraft, okDraft := parseCounter(scrape1, "tally_ai_plan_executed_total", map[string]string{"type": "create_purchase_draft", "tenant_id": tenantID})
	postPrice, okPrice := parseCounter(scrape1, "tally_ai_plan_executed_total", map[string]string{"type": "price_change", "tenant_id": tenantID})
	postBulk, okBulk := parseCounter(scrape1, "tally_ai_plan_executed_total", map[string]string{"type": "bulk_stock_adjust", "tenant_id": tenantID})

	t.Logf("post: create_purchase_draft=%.0f  price_change=%.0f  bulk_stock_adjust=%.0f", postDraft, postPrice, postBulk)

	fail := false
	if !okDraft || postDraft != preDraft+1 {
		t.Errorf("FAIL: create_purchase_draft pre=%.0f post=%.0f (ok=%v)", preDraft, postDraft, okDraft)
		fail = true
	}
	if !okPrice || postPrice != prePrice+1 {
		t.Errorf("FAIL: price_change pre=%.0f post=%.0f (ok=%v)", prePrice, postPrice, okPrice)
		fail = true
	}
	if !okBulk || postBulk != preBulk+1 {
		t.Errorf("FAIL: bulk_stock_adjust pre=%.0f post=%.0f (ok=%v)", preBulk, postBulk, okBulk)
		fail = true
	}
	if !fail {
		t.Log("PASS: all three plan type labels incremented independently")
	}
}

// TestMetrics_WebTelemetry_FromHTTPHandler verifies that POSTing a telemetry
// event to /internal/v1/telemetry/web increments tally_web_telemetry_total
// via the real HTTP handler (not a direct function call).
func TestMetrics_WebTelemetry_FromHTTPHandler(t *testing.T) {
	metricsEng := metricsEngine()
	telEng := telemetryEngine()

	event := "plan_accept_rate"

	// Scrape baseline.
	scrape0 := scrapeMetrics(t, metricsEng)
	pre, _ := parseCounter(scrape0, "tally_web_telemetry_total", map[string]string{"event": event})
	t.Logf("tally_web_telemetry_total{event=%q} pre: %.0f", event, pre)

	// POST the event through the real handler.
	body := bytes.NewBufferString(`{"event":"plan_accept_rate"}`)
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/telemetry/web", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	telEng.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("telemetry POST returned %d: %s", w.Code, w.Body.String())
	}

	// Scrape post.
	scrape1 := scrapeMetrics(t, metricsEng)
	post, ok := parseCounter(scrape1, "tally_web_telemetry_total", map[string]string{"event": event})
	t.Logf("tally_web_telemetry_total{event=%q} post: %.0f", event, post)

	if !ok {
		t.Fatalf("FAIL: tally_web_telemetry_total{event=%q} not found in scrape", event)
	}
	if post != pre+1 {
		t.Errorf("FAIL: expected delta +1, got pre=%.0f post=%.0f", pre, post)
	} else {
		t.Logf("PASS: tally_web_telemetry_total{event=%q} incremented %.0f → %.0f", event, pre, post)
	}
}

// TestMetrics_NoLeakAcrossLabels verifies that incrementing one plan type does
// NOT affect a different plan type counter.
func TestMetrics_NoLeakAcrossLabels(t *testing.T) {
	engine := metricsEngine()
	tenantID := "test-tenant-noleak-003"

	scrape0 := scrapeMetrics(t, engine)
	prePrice, _ := parseCounter(scrape0, "tally_ai_plan_executed_total", map[string]string{"type": "price_change", "tenant_id": tenantID})

	// Increment ONLY create_purchase_draft.
	middleware.IncAIPlanExecuted("create_purchase_draft", tenantID)

	scrape1 := scrapeMetrics(t, engine)
	postPrice, _ := parseCounter(scrape1, "tally_ai_plan_executed_total", map[string]string{"type": "price_change", "tenant_id": tenantID})

	t.Logf("price_change{tenant_id=%q}: pre=%.0f post=%.0f (only create_purchase_draft was called)", tenantID, prePrice, postPrice)

	if postPrice != prePrice {
		t.Errorf("FAIL: price_change label leaked: expected %.0f, got %.0f", prePrice, postPrice)
	} else {
		t.Logf("PASS: create_purchase_draft did not bump price_change label")
	}
}

// TestMetrics_EndpointShape verifies the /internal/v1/metrics endpoint returns
// HTTP 200 with Prometheus text-format content-type and all three metric names
// plus a # HELP line for each.
func TestMetrics_EndpointShape(t *testing.T) {
	engine := metricsEngine()

	// Use a real TCP listener to confirm the route works end-to-end (not just httptest).
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: engine}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	url := "http://" + ln.Addr().String() + "/internal/v1/metrics"
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("FAIL: expected 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("FAIL: Content-Type must start with text/plain, got: %q", ct)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	body := string(bodyBytes)

	// Print first 50 lines as evidence.
	lines := strings.Split(body, "\n")
	limit := 50
	if len(lines) < limit {
		limit = len(lines)
	}
	t.Logf("--- first %d lines of /internal/v1/metrics ---", limit)
	for _, l := range lines[:limit] {
		t.Log(l)
	}
	t.Log("--- end ---")

	requiredMetrics := []string{
		"tally_wad_total",
		"tally_ai_plan_executed_total",
		"tally_web_telemetry_total",
	}
	for _, name := range requiredMetrics {
		helpLine := "# HELP " + name
		if !strings.Contains(body, name) {
			t.Errorf("FAIL: metric %q not found in scrape body", name)
		} else if !strings.Contains(body, helpLine) {
			t.Errorf("FAIL: # HELP line for %q not found in scrape body", name)
		} else {
			t.Logf("PASS: %q present with # HELP line", name)
		}
	}

	if resp.StatusCode == http.StatusOK &&
		strings.HasPrefix(ct, "text/plain") {
		t.Log("PASS: endpoint shape — HTTP 200, text/plain, all metric names + HELP lines present")
	}
}
