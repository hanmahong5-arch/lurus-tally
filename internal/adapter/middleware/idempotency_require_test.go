package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
)

// newRequireKeyEngine builds an engine with RequireIdempotencyKey in front of a
// set of routes mirroring the production allowlist (plus a read on an
// allowlisted path and a non-allowlisted write). The leaf handler writes a
// sentinel so tests can tell "reached the handler" from "aborted 400".
func newRequireKeyEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	api := e.Group("/api/v1")
	api.Use(middleware.RequireIdempotencyKey())
	reached := func(c *gin.Context) { c.String(http.StatusOK, "reached") }
	api.POST("/payments", reached)
	api.GET("/payments", reached) // read on an allowlisted path
	api.POST("/purchase-bills/:id/approve", reached)
	api.POST("/sale-bills/:id/approve", reached)
	api.POST("/sale-bills/quick-checkout", reached)
	api.POST("/ai/plans/:plan_id/confirm", reached)
	api.POST("/products", reached) // non-allowlisted write
	return e
}

// allowlistedRoutes is the (method, path) set the production middleware guards.
var allowlistedRoutes = []struct{ method, path string }{
	{http.MethodPost, "/api/v1/payments"},
	{http.MethodPost, "/api/v1/purchase-bills/11111111-1111-1111-1111-111111111111/approve"},
	{http.MethodPost, "/api/v1/sale-bills/22222222-2222-2222-2222-222222222222/approve"},
	{http.MethodPost, "/api/v1/sale-bills/quick-checkout"},
	{http.MethodPost, "/api/v1/ai/plans/33333333-3333-3333-3333-333333333333/confirm"},
}

func TestRequireIdempotencyKey_AllowlistedNoKey_Returns400(t *testing.T) {
	e := newRequireKeyEngine()
	for _, rt := range allowlistedRoutes {
		req, _ := http.NewRequest(rt.method, rt.path, nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s %s without key: status = %d, want 400 (body=%s)", rt.method, rt.path, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "missing_idempotency_key") {
			t.Errorf("%s %s: body = %s, want missing_idempotency_key", rt.method, rt.path, rec.Body.String())
		}
	}
}

func TestRequireIdempotencyKey_AllowlistedWithKey_Passes(t *testing.T) {
	e := newRequireKeyEngine()
	for _, rt := range allowlistedRoutes {
		req, _ := http.NewRequest(rt.method, rt.path, nil)
		req.Header.Set(middleware.HeaderIdempotencyKey, "client-key-123")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK || rec.Body.String() != "reached" {
			t.Errorf("%s %s with key: status = %d body = %q, want 200 \"reached\"", rt.method, rt.path, rec.Code, rec.Body.String())
		}
	}
}

func TestRequireIdempotencyKey_NonAllowlistedWrite_Passes(t *testing.T) {
	e := newRequireKeyEngine()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/products", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || rec.Body.String() != "reached" {
		t.Errorf("non-allowlisted POST without key: status = %d body = %q, want 200 \"reached\"", rec.Code, rec.Body.String())
	}
}

func TestRequireIdempotencyKey_ReadOnAllowlistedPath_Passes(t *testing.T) {
	e := newRequireKeyEngine()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/payments", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || rec.Body.String() != "reached" {
		t.Errorf("GET on allowlisted path without key: status = %d body = %q, want 200 \"reached\"", rec.Code, rec.Body.String())
	}
}
