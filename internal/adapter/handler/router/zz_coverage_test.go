// Package router (internal test file, NOT router_test) so the unexported
// helpers (isStreamingRoute, productHandler, unitHandler) can be exercised
// directly. This file only ADDS tests; it must not modify router.go or any
// existing *_test.go in this directory.
package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	handleracct "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/account"
	handlerai "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/ai"
	handlerAuth "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/auth"
	handlerbill "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/bill"
	handlerbilling "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/billing"
	handlercurrency "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/currency"
	handlerdigest "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/digest"
	handlerexport "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/export"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/health"
	handlerhorticulture "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/horticulture"
	handlerimporting "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/importing"
	handlermetrics "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/metrics"
	handleronboarding "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/onboarding"
	handlerpayment "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/payment"
	handlerproduct "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/product"
	handlerproject "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/project"
	handlerreplenish "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/replenish"
	handlerreports "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/reports"
	handlersearch "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/search"
	handlerstock "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/stock"
	handlersupp "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/supplier"
	handlerunit "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/unit"
	handlerwarehouse "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/warehouse"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// buildNilRouter mirrors router_test.go's newTestRouter: only the health
// handler is real, every business handler and every middleware slot is nil.
func buildNilRouter() *gin.Engine {
	h := health.New("dev")
	return New(h, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
}

// buildFullyWiredRouter constructs New(...) with every handler slot and every
// middleware slot non-nil (internal use-case fields stay nil — RegisterRoutes
// only needs a non-nil receiver to register routes; it never invokes the
// use case at construction time). This exercises every "if x != nil" TRUE
// branch in router.New that buildNilRouter's FALSE branches cannot reach.
func buildFullyWiredRouter() *gin.Engine {
	h := health.New("dev")
	authMW := func(c *gin.Context) { c.Next() }
	tenantDBMW := func(c *gin.Context) { c.Next() }
	idempotencyMW := func(c *gin.Context) { c.Next() }

	ph := handlerproduct.New(nil, nil, nil, nil, nil, nil)
	uh := handlerunit.New(nil, nil, nil)
	ah := handlerAuth.New(nil, nil)
	pat := handlerAuth.NewPATHandler(nil)
	sh := handlerstock.New(nil, nil, nil, nil, nil)
	bh := handlerbill.New(nil, nil, nil, nil, nil, nil, nil)
	ch := handlercurrency.New(nil, nil, nil, nil)
	saleh := handlerbill.NewSaleHandler(nil, nil, nil, nil, nil, nil, nil)
	payh := handlerpayment.New(nil, nil)
	bilh := handlerbilling.New(nil, nil)
	aih := handlerai.New(nil)
	dh := handlerhorticulture.NewDictHandler(nil, nil, nil, nil, nil, nil)
	projh := handlerproject.NewProjectHandler(nil, nil, nil, nil, nil, nil)
	mh := handlermetrics.NewMetricsHandler("")
	suph := handlersupp.New(nil, nil, nil, nil, nil, nil)
	wh := handlerwarehouse.New(nil, nil, nil, nil, nil, nil)
	exh := handlerexport.New(nil, nil, nil, nil)
	acct := handleracct.New(nil, nil, nil, nil, nil, nil, nil)
	replh := handlerreplenish.New(nil)
	reph := handlerreports.New(nil)
	srch := handlersearch.New(nil)
	imph := handlerimporting.New(nil, uuid.Nil)
	digh := handlerdigest.New(nil)
	onh := handleronboarding.NewForTest(nil, nil)

	return New(h, authMW, tenantDBMW, idempotencyMW, ph, uh, ah, pat, sh, bh, ch, saleh, payh, bilh, aih, dh, projh, mh, suph, wh, exh, acct, replh, reph, srch, imph, digh, onh)
}

// TestNilHandlerGroups_Return501NotFound is the outline's killer-feature table:
// every business-handler group, when its handler pointer is nil, must respond
// 501 "handler not configured" (via notImplemented) — never 404 and never a
// panic. GET/DELETE requests are used for allowlisted-idempotency-safe probes
// (no route in this table requires an Idempotency-Key header — see
// idempotency_require.go's requireIdempotencyKeyRoutes allowlist).
func TestNilHandlerGroups_Return501NotFound(t *testing.T) {
	r := buildNilRouter()

	cases := []struct {
		group  string
		method string
		path   string
	}{
		{"products", http.MethodGet, "/api/v1/products"},
		{"units", http.MethodGet, "/api/v1/units"},
		{"stock", http.MethodGet, "/api/v1/stock/snapshots"},
		{"purchase-bills", http.MethodGet, "/api/v1/purchase-bills"},
		{"sale-bills", http.MethodGet, "/api/v1/sale-bills"},
		{"payments", http.MethodGet, "/api/v1/payments"},
		{"billing", http.MethodGet, "/api/v1/billing/overview"},
		{"ai", http.MethodGet, "/api/v1/ai/plans"},
		{"nursery-dict", http.MethodGet, "/api/v1/nursery-dict"},
		{"projects", http.MethodGet, "/api/v1/projects"},
		{"suppliers", http.MethodGet, "/api/v1/suppliers"},
		{"warehouses", http.MethodGet, "/api/v1/warehouses"},
		{"exports", http.MethodGet, "/api/v1/exports/bills.csv"},
		{"account", http.MethodGet, "/api/v1/account/profile"},
		{"replenish", http.MethodGet, "/api/v1/replenish/suggestions"},
		{"reports", http.MethodGet, "/api/v1/reports/gross-margin"},
		{"search", http.MethodGet, "/api/v1/search"},
		{"imports", http.MethodGet, "/api/v1/imports/mappings"},
		{"weekly-summary", http.MethodGet, "/api/v1/weekly-summary"},
		{"onboarding", http.MethodPost, "/api/v1/onboarding/seed-demo"},
		{"currency", http.MethodGet, "/api/v1/currencies"},
		{"auth-me", http.MethodGet, "/api/v1/me"},
		{"auth-pat", http.MethodGet, "/api/v1/auth/pats"},
	}

	for _, tc := range cases {
		t.Run(tc.group, func(t *testing.T) {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest(tc.method, tc.path, nil)
			r.ServeHTTP(w, req)

			if w.Code == http.StatusNotFound {
				t.Fatalf("%s %s: got 404 — route not registered (want 501 stub)", tc.method, tc.path)
			}
			if w.Code != http.StatusNotImplemented {
				t.Fatalf("%s %s: want 501, got %d (body=%s)", tc.method, tc.path, w.Code, w.Body.String())
			}
			if ct := w.Body.String(); ct != `{"error":"handler not configured"}` {
				t.Errorf("%s %s: unexpected 501 body %q", tc.method, tc.path, ct)
			}
		})
	}
}

// TestRequireIdempotencyKey_EnforcedEvenWhenDedupMiddlewareNil is business
// invariant #1: the mandatory Idempotency-Key guard on the allowlisted write
// routes (idempotency_require.go) must fire even when idempotencyMW (the
// opt-in Redis dedup layer) is nil — the retry-safety contract must not
// depend on cache availability. POST /api/v1/payments is in the allowlist.
func TestRequireIdempotencyKey_EnforcedEvenWhenDedupMiddlewareNil(t *testing.T) {
	r := buildNilRouter() // authMW, tenantDBMW, idempotencyMW, payh all nil

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/payments", nil)
	// Deliberately NOT setting Idempotency-Key.
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("POST /api/v1/payments without Idempotency-Key: want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
	if w.Code == http.StatusNotImplemented {
		t.Fatal("the 400 idempotency guard must fire BEFORE the nil-handler 501 stub is reached")
	}
}

// TestMiddlewareOrder_AuthWinsBeforeIdempotencyCheck is business invariant #2:
// authMW must run before RequireIdempotencyKey, so an unauthenticated request
// to an allowlisted write route 401s rather than 400ing on the missing header.
func TestMiddlewareOrder_AuthWinsBeforeIdempotencyCheck(t *testing.T) {
	h := health.New("dev")
	denyAuth := func(c *gin.Context) {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
	}
	r := New(h, denyAuth, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/payments", nil)
	// No Idempotency-Key header either — if ordering were reversed this would 400.
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("authMW must run first: want 401, got %d (body=%s)", w.Code, w.Body.String())
	}
}

// TestFullyWiredRouter_RegistersEveryHandlerGroup exercises every "if x != nil"
// TRUE branch in router.New (all business handlers + all three optional
// middlewares + the metrics handler) by constructing the engine with none of
// them nil. It does not assert success status codes for the delegated routes
// (the wired handlers carry nil internal use cases, so gin.Recovery may turn a
// panic into a 500) — only that dispatch reaches the real handler (not the 501
// stub) and that the metrics route, gated on mh!=nil, is actually mounted.
func TestFullyWiredRouter_RegistersEveryHandlerGroup(t *testing.T) {
	r := buildFullyWiredRouter()

	// Health routes still work with middleware attached.
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/internal/v1/tally/health", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("GET /internal/v1/tally/health with full wiring: want 200, got %d", w.Code)
	}

	// /internal/v1/metrics is only registered when mh != nil.
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/internal/v1/metrics", nil)
	r.ServeHTTP(w, req)
	if w.Code == http.StatusNotFound {
		t.Error("GET /internal/v1/metrics: got 404 — should be registered when mh != nil")
	}

	delegated := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/products"},
		{http.MethodGet, "/api/v1/units"},
		{http.MethodGet, "/api/v1/me"},
		{http.MethodGet, "/api/v1/auth/pats"},
		{http.MethodGet, "/api/v1/stock/snapshots"},
		{http.MethodGet, "/api/v1/purchase-bills"},
		{http.MethodGet, "/api/v1/currencies"},
		{http.MethodGet, "/api/v1/sale-bills"},
		{http.MethodGet, "/api/v1/payments"},
		{http.MethodGet, "/api/v1/billing/overview"},
		{http.MethodGet, "/api/v1/ai/plans"},
		{http.MethodGet, "/api/v1/nursery-dict"},
		{http.MethodGet, "/api/v1/projects"},
		{http.MethodGet, "/api/v1/suppliers"},
		{http.MethodGet, "/api/v1/warehouses"},
		{http.MethodGet, "/api/v1/exports/bills.csv"},
		{http.MethodGet, "/api/v1/account/profile"},
		{http.MethodGet, "/api/v1/replenish/suggestions"},
		{http.MethodGet, "/api/v1/reports/gross-margin"},
		{http.MethodGet, "/api/v1/search"},
		{http.MethodGet, "/api/v1/imports/mappings"},
		{http.MethodGet, "/api/v1/weekly-summary"},
	}
	for _, tc := range delegated {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(tc.method, tc.path, nil)
		r.ServeHTTP(w, req)
		if w.Code == http.StatusNotFound {
			t.Errorf("%s %s: got 404 with full wiring — route not registered", tc.method, tc.path)
		}
		if w.Code == http.StatusNotImplemented {
			t.Errorf("%s %s: got 501 with a non-nil handler — RegisterRoutes should have replaced the stub", tc.method, tc.path)
		}
	}
}

// TestProductHandlerHelper_NilAndNonNil unit-tests the productHandler adapter
// directly: nil -> notImplemented (501, fn never invoked); non-nil -> the
// returned gin.HandlerFunc invokes fn with the same handler pointer.
func TestProductHandlerHelper_NilAndNonNil(t *testing.T) {
	var called bool
	var gotH *handlerproduct.Handler
	fn := func(h *handlerproduct.Handler, c *gin.Context) {
		called = true
		gotH = h
		c.Status(http.StatusTeapot)
	}

	// nil handler.
	w1 := httptest.NewRecorder()
	c1, _ := gin.CreateTestContext(w1)
	c1.Request = httptest.NewRequest(http.MethodGet, "/x", nil)
	productHandler(nil, fn)(c1)
	if w1.Code != http.StatusNotImplemented {
		t.Fatalf("nil handler: want 501, got %d", w1.Code)
	}
	if called {
		t.Fatal("fn must NOT be invoked when the handler pointer is nil")
	}

	// non-nil handler.
	ph := handlerproduct.New(nil, nil, nil, nil, nil, nil)
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodGet, "/x", nil)
	productHandler(ph, fn)(c2)
	if !called {
		t.Fatal("fn must be invoked when the handler pointer is non-nil")
	}
	if gotH != ph {
		t.Fatal("fn must be called with the same handler pointer passed to productHandler")
	}
	// c.Status only buffers the code on gin's internal ResponseWriter; it is
	// flushed to the underlying httptest.Recorder on the first Write() or by
	// the engine's post-handler-chain WriteHeaderNow(), neither of which runs
	// here since we invoke the handler func directly. Assert against gin's
	// own tracked status instead of the (still-default) recorder code.
	if got := c2.Writer.Status(); got != http.StatusTeapot {
		t.Fatalf("non-nil handler: want delegate's status %d, got %d", http.StatusTeapot, got)
	}
}

// TestUnitHandlerHelper_NilAndNonNil mirrors TestProductHandlerHelper_NilAndNonNil
// for the unitHandler adapter.
func TestUnitHandlerHelper_NilAndNonNil(t *testing.T) {
	var called bool
	var gotH *handlerunit.Handler
	fn := func(h *handlerunit.Handler, c *gin.Context) {
		called = true
		gotH = h
		c.Status(http.StatusTeapot)
	}

	// nil handler.
	w1 := httptest.NewRecorder()
	c1, _ := gin.CreateTestContext(w1)
	c1.Request = httptest.NewRequest(http.MethodGet, "/x", nil)
	unitHandler(nil, fn)(c1)
	if w1.Code != http.StatusNotImplemented {
		t.Fatalf("nil handler: want 501, got %d", w1.Code)
	}
	if called {
		t.Fatal("fn must NOT be invoked when the handler pointer is nil")
	}

	// non-nil handler.
	uh := handlerunit.New(nil, nil, nil)
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodGet, "/x", nil)
	unitHandler(uh, fn)(c2)
	if !called {
		t.Fatal("fn must be invoked when the handler pointer is non-nil")
	}
	if gotH != uh {
		t.Fatal("fn must be called with the same handler pointer passed to unitHandler")
	}
	if got := c2.Writer.Status(); got != http.StatusTeapot {
		t.Fatalf("non-nil handler: want delegate's status %d, got %d", http.StatusTeapot, got)
	}
}

// TestIsStreamingRoute_Predicate unit-tests isStreamingRoute directly against a
// *gin.Context whose FullPath is set by routing a real request through a
// minimal engine (gin.Context.FullPath() has no public setter, so real
// dispatch is the only way to populate it deterministically).
func TestIsStreamingRoute_Predicate(t *testing.T) {
	cases := []struct {
		name  string
		route string
		want  bool
	}{
		{"ai chat exact match streams", "/api/v1/ai/chat", true},
		{"csv export suffix streams", "/api/v1/exports/bills.csv", true},
		{"another csv suffix streams", "/api/v1/exports/stock.csv", true},
		{"normal json route does not stream", "/api/v1/products", false},
		{"health route does not stream", "/internal/v1/tally/health", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			var got bool
			r.GET(tc.route, func(c *gin.Context) { got = isStreamingRoute(c) })

			w := httptest.NewRecorder()
			req, _ := http.NewRequest(http.MethodGet, tc.route, nil)
			r.ServeHTTP(w, req)

			if got != tc.want {
				t.Errorf("isStreamingRoute(%q) = %v, want %v", tc.route, got, tc.want)
			}
		})
	}
}

// TestNotImplemented_ReturnsCanonicalBody locks the exact 501 payload shape
// that every nil-handler stub and both productHandler/unitHandler adapters
// depend on.
func TestNotImplemented_ReturnsCanonicalBody(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/x", nil)

	notImplemented(c)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("want 501, got %d", w.Code)
	}
	if w.Body.String() != `{"error":"handler not configured"}` {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}
}
