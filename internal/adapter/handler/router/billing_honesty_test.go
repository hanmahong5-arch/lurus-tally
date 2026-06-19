// Honesty lock test (TALLY-04) — converts the verified claim
//
//	"Billing /api/v1/billing/{overview,subscribe}; env
//	 PLATFORM_INTERNAL_KEY+PLATFORM_BASE_URL 默认 svc:18104; 空→501"
//
// into a behavioural contract on the router's nil-handler degradation: when the
// billing handler is nil (the wiring outcome when PLATFORM_INTERNAL_KEY is
// empty → nil platform client → nil billingHandler, see lifecycle/app.go), the
// /api/v1/billing/{overview,subscribe} routes must return 501. When a real
// handler is wired, the same routes must NOT return 501.
//
// HONESTY NOTE on the claim wording: the claim says "501 not_implemented", and
// the HTTP STATUS is indeed 501 (http.StatusNotImplemented). The response BODY,
// however, is {"error":"handler not configured"} — the literal token
// "not_implemented" does NOT appear in the body. This test asserts the actual
// observed contract (status 501 + body error=="handler not configured") rather
// than the claim's paraphrase, and the divergence is documented here.
//
// These assertions exercise the route-registration / dispatch contract a client
// observes; they are not tautologies (§4.1③).
package router_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	handlerbilling "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/billing"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/health"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/router"
	appbilling "github.com/hanmahong5-arch/lurus-tally/internal/app/billing"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/platformclient"

	"github.com/gin-gonic/gin"
)

// stub executors satisfy the billing handler's narrow interfaces so we can wire
// a NON-nil handler without any platform / network dependency (hermetic).
type stubSubscribeExec struct{}

func (stubSubscribeExec) Execute(_ context.Context, _ appbilling.SubscribeInput) (*appbilling.SubscribeOutput, error) {
	return &appbilling.SubscribeOutput{}, nil
}

type stubOverviewExec struct{}

func (stubOverviewExec) Execute(_ context.Context, _ string) (*platformclient.AccountOverview, error) {
	return &platformclient.AccountOverview{}, nil
}

// routerWithBilling builds the engine with every handler nil EXCEPT the billing
// handler (which may itself be nil to model the PLATFORM_INTERNAL_KEY-empty
// case). authMW is nil so we exercise pure dispatch.
func routerWithBilling(bilh *handlerbilling.Handler) *gin.Engine {
	h := health.New("dev")
	return router.New(
		h, nil, nil, nil, // health, authMW, tenantDBMW, idempotencyMW
		nil, nil, nil, nil, nil, nil, nil, nil, nil, // ph,uh,ah,pat,sh,bh,ch,saleh,payh
		bilh,                                                                 // bilh — the one under test
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, // remaining handlers
	)
}

// TestBillingRoutes_NilHandler_Returns501 is the TALLY-04 anchor: a nil billing
// handler (PLATFORM_INTERNAL_KEY empty) makes both billing routes return 501
// with the documented degradation body.
func TestBillingRoutes_NilHandler_Returns501(t *testing.T) {
	r := routerWithBilling(nil)

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/billing/overview"},
		{http.MethodPost, "/api/v1/billing/subscribe"},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(tc.method, tc.path, nil)
		// /billing/subscribe is now idempotency-gated (RequireIdempotencyKey);
		// supply a key so the request reaches the handler/stub under test rather
		// than short-circuiting at the 400 guard. GET overview ignores it.
		req.Header.Set("Idempotency-Key", "honesty-test-key")
		r.ServeHTTP(w, req)

		if w.Code != http.StatusNotImplemented {
			t.Fatalf("%s %s with nil handler: want 501, got %d (body=%s)", tc.method, tc.path, w.Code, w.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s %s: 501 body not JSON: %v", tc.method, tc.path, err)
		}
		// Actual contract token (see HONESTY NOTE): "handler not configured".
		if body["error"] != "handler not configured" {
			t.Errorf("%s %s: error body = %v, want \"handler not configured\"", tc.method, tc.path, body["error"])
		}
	}
}

// TestBillingRoutes_RealHandler_NotImplementedReplaced locks the inverse: when a
// real handler is wired, the same routes are served by it (NOT the 501 stub).
// Without a Zitadel sub in context the handler returns 401 — the key assertion
// is that the status is NOT 501, proving RegisterRoutes ran instead of the
// notImplemented placeholder.
func TestBillingRoutes_RealHandler_NotImplementedReplaced(t *testing.T) {
	bilh := handlerbilling.New(stubSubscribeExec{}, stubOverviewExec{})
	r := routerWithBilling(bilh)

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/billing/overview"},
		{http.MethodPost, "/api/v1/billing/subscribe"},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(tc.method, tc.path, nil)
		// /billing/subscribe is now idempotency-gated (RequireIdempotencyKey);
		// supply a key so the request reaches the handler/stub under test rather
		// than short-circuiting at the 400 guard. GET overview ignores it.
		req.Header.Set("Idempotency-Key", "honesty-test-key")
		r.ServeHTTP(w, req)

		if w.Code == http.StatusNotImplemented {
			t.Errorf("%s %s with real handler must NOT be 501 (route should be handler-served), got 501", tc.method, tc.path)
		}
		if w.Code == http.StatusNotFound {
			t.Errorf("%s %s: route not registered (404)", tc.method, tc.path)
		}
		// No sub in context → handler returns 401 (overview reads X-Zitadel-Sub /
		// ctx; subscribe also checks auth before body binding).
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s real handler without auth: want 401, got %d (body=%s)", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
}
