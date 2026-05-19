package health_test

import (
	"context"
	"encoding/json"
	"errors"
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

type stubPinger struct {
	err   error
	delay time.Duration
}

func (s *stubPinger) Ping(ctx context.Context) error {
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return s.err
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

func TestHealthHandler_Readyz_NoDeps_Returns200WithOK(t *testing.T) {
	h := health.New("dev")
	r := newRouter(h)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/internal/v1/tally/ready", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("expected status=ok, got %v", body["status"])
	}
}

func TestHealthHandler_Readyz_AllDepsHealthy_Returns200WithOK(t *testing.T) {
	h := health.New("dev",
		health.Dep{Name: "db", Pinger: &stubPinger{}, Required: true},
		health.Dep{Name: "redis", Pinger: &stubPinger{}, Required: false},
	)
	r := newRouter(h)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/internal/v1/tally/ready", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("expected status=ok when all deps healthy, got %v", body["status"])
	}
}

func TestHealthHandler_Readyz_RequiredDepDown_Returns503Unhealthy(t *testing.T) {
	h := health.New("dev",
		health.Dep{Name: "db", Pinger: &stubPinger{err: errors.New("connection refused")}, Required: true},
	)
	r := newRouter(h)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/internal/v1/tally/ready", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when required dep is down, got %d (body=%s)", w.Code, w.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["status"] != "unhealthy" {
		t.Errorf("expected status=unhealthy, got %v", body["status"])
	}
	failures, ok := body["failures"].([]any)
	if !ok || len(failures) == 0 {
		t.Errorf("expected failures list in body, got %v", body)
	}
}

// TestHealthHandler_Readyz_OptionalDepDown_Returns200Degraded is the key E2 contract:
// PG healthy + Redis down → 200 with status="degraded" and degraded=["redis"].
// This keeps the pod in k8s endpoints (non-AI requests still work) while surfacing
// the degraded state for alerting.
func TestHealthHandler_Readyz_OptionalDepDown_Returns200Degraded(t *testing.T) {
	h := health.New("dev",
		health.Dep{Name: "db", Pinger: &stubPinger{}, Required: true},
		health.Dep{Name: "redis", Pinger: &stubPinger{err: errors.New("redis down")}, Required: false},
	)
	r := newRouter(h)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/internal/v1/tally/ready", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 when only optional dep is down, got %d (body=%s)", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}
	if body["status"] != "degraded" {
		t.Errorf("expected status=degraded, got %v", body["status"])
	}
	deg, ok := body["degraded"].([]any)
	if !ok || len(deg) == 0 {
		t.Errorf("expected degraded list in body, got %v", body)
	}
	if deg[0] != "redis" {
		t.Errorf("expected degraded[0]=redis, got %v", deg[0])
	}
}

// TestHealthHandler_Readyz_NATSDown_Degraded confirms NATS outage (optional dep)
// returns 200 degraded — the outbox pattern means NATS down does not lose writes.
func TestHealthHandler_Readyz_NATSDown_Degraded(t *testing.T) {
	h := health.New("dev",
		health.Dep{Name: "db", Pinger: &stubPinger{}, Required: true},
		health.Dep{Name: "nats", Pinger: &stubPinger{err: errors.New("nats unavailable")}, Required: false},
	)
	r := newRouter(h)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/internal/v1/tally/ready", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 when NATS (optional) is down, got %d (body=%s)", w.Code, w.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["status"] != "degraded" {
		t.Errorf("expected status=degraded for NATS down, got %v", body["status"])
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
