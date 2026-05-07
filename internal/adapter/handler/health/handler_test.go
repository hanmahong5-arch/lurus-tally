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

func TestHealthHandler_Readyz_NoDeps_Returns200(t *testing.T) {
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
	if body["status"] != "ready" {
		t.Errorf("expected status=ready, got %v", body["status"])
	}
}

func TestHealthHandler_Readyz_AllDepsHealthy_Returns200(t *testing.T) {
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
}

func TestHealthHandler_Readyz_RequiredDepDown_Returns503(t *testing.T) {
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
	if body["status"] != "not_ready" {
		t.Errorf("expected status=not_ready, got %v", body["status"])
	}
}

func TestHealthHandler_Readyz_OptionalDepDown_Still200(t *testing.T) {
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
