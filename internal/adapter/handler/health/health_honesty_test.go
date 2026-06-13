// Honesty lock test (GOAL-09) — converts the verified claim
//
//	"go build ./cmd/server 退0 + go test health PASS"
//
// into behavioural contracts for the readiness/liveness handlers. These tests
// use the package-internal stub Pinger seam (no real DB / Redis / network) and
// lock the parts of the contract that the existing handler_test.go does not yet
// assert: liveness never depends on deps, a failing dep returns 503 instead of
// panicking, ready vs live semantics differ, the handlers are safe to call
// concurrently, and a cancelled request context surfaces as 503 (required dep)
// rather than a crash.
//
// These are NOT tautologies (§4.1③): they assert the HTTP status / JSON shape a
// k8s probe observes, not the handler's internal statements.
package health_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/health"
)

// failPinger always fails — used to assert "DB down → 503, not panic".
type failPinger struct{ msg string }

func (f failPinger) Ping(context.Context) error { return errors.New(f.msg) }

// ctxAwarePinger blocks until its context is cancelled, then returns ctx.Err().
// It lets us prove a cancelled request context resolves to a required-dep
// failure (503) rather than hanging or panicking.
type ctxAwarePinger struct{}

func (ctxAwarePinger) Ping(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

func honestyRouter(h *health.Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/internal/v1/tally/health", h.Healthz)
	r.GET("/internal/v1/tally/ready", h.Readyz)
	return r
}

// TestHealth_ReadyEndpoint_HealthyContract is the GOAL-09 anchor: with a healthy
// stub required dep the /ready endpoint returns 200 and the documented JSON
// status="ok" — using a stub Pinger, never a real DB.
func TestHealth_ReadyEndpoint_HealthyContract(t *testing.T) {
	h := health.New("v9.9.9",
		health.Dep{Name: "db", Pinger: okPinger{}, Required: true},
	)
	r := honestyRouter(h)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/internal/v1/tally/ready", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("ready with healthy stub dep: want 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("ready body not JSON: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("ready status: want ok, got %v", body["status"])
	}
	// Contract: a clean ready response must NOT carry failures/degraded keys.
	if _, present := body["failures"]; present {
		t.Errorf("healthy ready must omit failures key, got %v", body)
	}
	if _, present := body["degraded"]; present {
		t.Errorf("healthy ready must omit degraded key, got %v", body)
	}
}

// okPinger is the trivially-healthy stub used across this file.
type okPinger struct{}

func (okPinger) Ping(context.Context) error { return nil }

// TestHealth_ReadyEndpoint_RequiredDepDown_Returns503NotPanic locks the
// edge case "DB 失败返 503 非 panic": a required dep whose Ping errors must
// produce a clean 503 with status="unhealthy", and the request must not panic.
func TestHealth_ReadyEndpoint_RequiredDepDown_Returns503NotPanic(t *testing.T) {
	h := health.New("dev",
		health.Dep{Name: "db", Pinger: failPinger{msg: "connection refused"}, Required: true},
	)
	r := honestyRouter(h)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/internal/v1/tally/ready", nil)

	// If Readyz panicked, gin.Recovery() in production would convert it to 500;
	// here we run without Recovery so a panic would fail the test outright.
	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("Readyz panicked on failing dep (must return 503): %v", rec)
		}
	}()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("required dep down: want 503, got %d (body=%s)", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("503 body not JSON: %v", err)
	}
	if body["status"] != "unhealthy" {
		t.Errorf("status: want unhealthy, got %v", body["status"])
	}
	failures, ok := body["failures"].([]any)
	if !ok || len(failures) != 1 || failures[0] != "db" {
		t.Errorf("failures: want [db], got %v", body["failures"])
	}
}

// TestHealth_ReadyVsLive_Semantics locks the ready-vs-live distinction:
// liveness must stay 200 even while a required readiness dep is down, because
// a transient DB blip must not let k8s kill the pod. This is the core reason
// the two endpoints are separate.
func TestHealth_ReadyVsLive_Semantics(t *testing.T) {
	h := health.New("dev",
		health.Dep{Name: "db", Pinger: failPinger{msg: "db gone"}, Required: true},
	)
	r := honestyRouter(h)

	// Liveness: still 200 (process is alive, deps irrelevant).
	wl := httptest.NewRecorder()
	reqL, _ := http.NewRequest(http.MethodGet, "/internal/v1/tally/health", nil)
	r.ServeHTTP(wl, reqL)
	if wl.Code != http.StatusOK {
		t.Errorf("liveness must be 200 even when a required dep is down, got %d", wl.Code)
	}

	// Readiness: 503 (dep gone → pull from service endpoints).
	wr := httptest.NewRecorder()
	reqR, _ := http.NewRequest(http.MethodGet, "/internal/v1/tally/ready", nil)
	r.ServeHTTP(wr, reqR)
	if wr.Code != http.StatusServiceUnavailable {
		t.Errorf("readiness must be 503 when a required dep is down, got %d", wr.Code)
	}
}

// TestHealth_LivenessIgnoresBody locks "空 body": GET liveness with no request
// body still returns the identity JSON. (The handler must not require a body.)
func TestHealth_LivenessIgnoresBody(t *testing.T) {
	h := health.New("v1.0.0")
	r := honestyRouter(h)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/internal/v1/tally/health", nil) // nil body
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("liveness with empty body: want 200, got %d", w.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("liveness body not JSON: %v", err)
	}
	if body["service"] != "lurus-tally" || body["version"] != "v1.0.0" {
		t.Errorf("liveness identity wrong: %v", body)
	}
}

// TestHealth_Concurrent_NoDataRace fires many concurrent ready+live requests
// against the same Handler to lock the "并发" edge case: the handler holds no
// shared mutable state across requests, so this must complete without a data
// race (run with -race) or panic. The per-request results slice in Readyz is
// request-scoped, which this exercises under contention.
func TestHealth_Concurrent_NoDataRace(t *testing.T) {
	h := health.New("dev",
		health.Dep{Name: "db", Pinger: okPinger{}, Required: true},
		health.Dep{Name: "redis", Pinger: failPinger{msg: "down"}, Required: false},
	)
	r := honestyRouter(h)

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			path := "/internal/v1/tally/ready"
			if i%2 == 0 {
				path = "/internal/v1/tally/health"
			}
			w := httptest.NewRecorder()
			req, _ := http.NewRequest(http.MethodGet, path, nil)
			r.ServeHTTP(w, req)
			// ready → 200 degraded (redis optional down); live → 200.
			if w.Code != http.StatusOK {
				t.Errorf("concurrent %s: want 200, got %d", path, w.Code)
			}
		}(i)
	}
	wg.Wait()
}

// TestHealth_ReadyContextCancel_Returns503 locks the "context cancel" edge case:
// when the inbound request context is already cancelled, a required dep whose
// Ping respects the context resolves to a failure → 503, not a hang/panic.
func TestHealth_ReadyContextCancel_Returns503(t *testing.T) {
	h := health.New("dev",
		health.Dep{Name: "db", Pinger: ctxAwarePinger{}, Required: true},
	)
	r := honestyRouter(h)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before serving

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/internal/v1/tally/ready", nil)
	req = req.WithContext(ctx)

	done := make(chan struct{})
	go func() {
		r.ServeHTTP(w, req)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Readyz did not return within 2s on a cancelled context — possible hang")
	}

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("cancelled ctx with required dep: want 503, got %d (body=%s)", w.Code, w.Body.String())
	}
}
