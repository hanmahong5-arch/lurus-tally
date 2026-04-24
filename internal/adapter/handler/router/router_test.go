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

func TestRouter_HealthzRouteRegistered(t *testing.T) {
	h := health.New("dev")
	r := router.New(h)

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
	h := health.New("dev")
	r := router.New(h)

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
