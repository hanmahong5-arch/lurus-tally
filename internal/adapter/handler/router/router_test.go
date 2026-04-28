package router_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/health"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/router"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// newTestRouter creates a Gin engine with nil handlers for route-registration tests.
// nil handlers are safe as long as the tested routes don't reach the handler bodies.
// authMW is also nil in tests so we exercise pure route registration.
func newTestRouter() *gin.Engine {
	h := health.New("dev")
	return router.New(h, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
}

func TestRouter_HealthzRouteRegistered(t *testing.T) {
	r := newTestRouter()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/internal/v1/tally/health", nil)
	r.ServeHTTP(w, req)

	if w.Code == http.StatusNotFound {
		t.Error("GET /internal/v1/tally/health returned 404 — route not registered")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestRouter_ReadyzRouteRegistered(t *testing.T) {
	r := newTestRouter()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/internal/v1/tally/ready", nil)
	r.ServeHTTP(w, req)

	if w.Code == http.StatusNotFound {
		t.Error("GET /internal/v1/tally/ready returned 404 — route not registered")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestRouter_ProductRoutesRegistered(t *testing.T) {
	r := newTestRouter()

	routes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/products"},
		{http.MethodPost, "/api/v1/products"},
		{http.MethodGet, "/api/v1/products/some-id"},
		{http.MethodPut, "/api/v1/products/some-id"},
		{http.MethodDelete, "/api/v1/products/some-id"},
	}

	for _, tc := range routes {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(tc.method, tc.path, nil)
		r.ServeHTTP(w, req)
		if w.Code == http.StatusNotFound {
			t.Errorf("%s %s returned 404 — route not registered", tc.method, tc.path)
		}
	}
}

func TestRouter_UnitRoutesRegistered(t *testing.T) {
	r := newTestRouter()

	routes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/units"},
		{http.MethodPost, "/api/v1/units"},
		{http.MethodDelete, "/api/v1/units/some-id"},
	}

	for _, tc := range routes {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(tc.method, tc.path, nil)
		r.ServeHTTP(w, req)
		if w.Code == http.StatusNotFound {
			t.Errorf("%s %s returned 404 — route not registered", tc.method, tc.path)
		}
	}
}

// TestRouter_PurchaseRoutesRegistered verifies all 6 purchase bill routes are registered.
func TestRouter_PurchaseRoutesRegistered(t *testing.T) {
	r := newTestRouter()

	routes := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/v1/purchase-bills"},
		{http.MethodPut, "/api/v1/purchase-bills/some-id"},
		{http.MethodPost, "/api/v1/purchase-bills/some-id/approve"},
		{http.MethodPost, "/api/v1/purchase-bills/some-id/cancel"},
		{http.MethodGet, "/api/v1/purchase-bills"},
		{http.MethodGet, "/api/v1/purchase-bills/some-id"},
	}

	for _, tc := range routes {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(tc.method, tc.path, nil)
		r.ServeHTTP(w, req)
		if w.Code == http.StatusNotFound {
			t.Errorf("%s %s returned 404 — route not registered", tc.method, tc.path)
		}
	}
}

// TestRouter_CurrencyRoutesRegistered verifies currency and exchange rate routes are registered.
func TestRouter_CurrencyRoutesRegistered(t *testing.T) {
	r := newTestRouter()

	routes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/currencies"},
		{http.MethodGet, "/api/v1/exchange-rates"},
		{http.MethodPost, "/api/v1/exchange-rates"},
		{http.MethodGet, "/api/v1/exchange-rates/history"},
	}

	for _, tc := range routes {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(tc.method, tc.path, nil)
		r.ServeHTTP(w, req)
		if w.Code == http.StatusNotFound {
			t.Errorf("%s %s returned 404 — route not registered", tc.method, tc.path)
		}
	}
}

// TestRouter_SaleRoutes_Registered verifies all sale bill routes are registered.
func TestRouter_SaleRoutes_Registered(t *testing.T) {
	r := newTestRouter()

	routes := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/v1/sale-bills/quick-checkout"},
		{http.MethodPost, "/api/v1/sale-bills"},
		{http.MethodPut, "/api/v1/sale-bills/some-id"},
		{http.MethodPost, "/api/v1/sale-bills/some-id/approve"},
		{http.MethodPost, "/api/v1/sale-bills/some-id/cancel"},
		{http.MethodGet, "/api/v1/sale-bills"},
		{http.MethodGet, "/api/v1/sale-bills/some-id"},
	}

	for _, tc := range routes {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(tc.method, tc.path, nil)
		r.ServeHTTP(w, req)
		if w.Code == http.StatusNotFound {
			t.Errorf("%s %s returned 404 — route not registered", tc.method, tc.path)
		}
	}
}

// TestRouter_PaymentRoutesRegistered verifies payment routes are registered.
func TestRouter_PaymentRoutesRegistered(t *testing.T) {
	r := newTestRouter()

	routes := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/v1/payments"},
		{http.MethodGet, "/api/v1/payments"},
	}

	for _, tc := range routes {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(tc.method, tc.path, nil)
		r.ServeHTTP(w, req)
		if w.Code == http.StatusNotFound {
			t.Errorf("%s %s returned 404 — route not registered", tc.method, tc.path)
		}
	}
}

// TestRouter_BillingRoutesRegistered verifies the platform billing integration routes
// are wired (Tally → platform subscription checkout, Story 10.1).
func TestRouter_BillingRoutesRegistered(t *testing.T) {
	r := newTestRouter()

	routes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/billing/overview"},
		{http.MethodPost, "/api/v1/billing/subscribe"},
	}

	for _, tc := range routes {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(tc.method, tc.path, nil)
		r.ServeHTTP(w, req)
		if w.Code == http.StatusNotFound {
			t.Errorf("%s %s returned 404 — route not registered", tc.method, tc.path)
		}
	}
}

// TestRouter_AuthRoutesRegistered verifies auth and tenant profile routes are registered.
func TestRouter_AuthRoutesRegistered(t *testing.T) {
	r := newTestRouter()

	routes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/me"},
		{http.MethodPost, "/api/v1/tenant/profile"},
		{http.MethodPost, "/api/v1/auth/logout"},
	}

	for _, tc := range routes {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(tc.method, tc.path, nil)
		r.ServeHTTP(w, req)
		if w.Code == http.StatusNotFound {
			t.Errorf("%s %s returned 404 — route not registered", tc.method, tc.path)
		}
	}
}
