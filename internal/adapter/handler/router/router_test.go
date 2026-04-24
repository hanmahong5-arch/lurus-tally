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

// newTestRouter creates a Gin engine with nil product/unit handlers for route-registration tests.
// nil handlers are safe as long as the tested routes don't reach the handler bodies.
func newTestRouter() *gin.Engine {
	h := health.New("dev")
	return router.New(h, nil, nil)
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
