package health_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/health"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func newRouter(h *health.Handler) *gin.Engine {
	r := gin.New()
	r.GET("/internal/v1/tally/health", h.Healthz)
	r.GET("/internal/v1/tally/ready", h.Readyz)
	return r
}

func TestHealthHandler_Healthz_Returns200WithOKStatus(t *testing.T) {
	h := health.New("v1.2.3")
	r := newRouter(h)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/internal/v1/tally/health", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("expected status=ok, got %q", body["status"])
	}
	if body["service"] != "lurus-tally" {
		t.Errorf("expected service=lurus-tally, got %q", body["service"])
	}
	if body["version"] != "v1.2.3" {
		t.Errorf("expected version=v1.2.3, got %q", body["version"])
	}
}

func TestHealthHandler_Readyz_Returns200WithReadyStatus(t *testing.T) {
	h := health.New("dev")
	r := newRouter(h)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/internal/v1/tally/ready", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}
	if body["status"] != "ready" {
		t.Errorf("expected status=ready, got %q", body["status"])
	}
}

func TestHealthHandler_Healthz_ResponseTimeUnder10ms(t *testing.T) {
	h := health.New("dev")
	r := newRouter(h)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/internal/v1/tally/health", nil)

	start := time.Now()
	r.ServeHTTP(w, req)
	elapsed := time.Since(start)

	if elapsed > 10*time.Millisecond {
		t.Errorf("response time %v exceeded 10ms — handler must not block", elapsed)
	}
}
