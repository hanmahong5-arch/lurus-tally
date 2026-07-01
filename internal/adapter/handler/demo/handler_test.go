package demo_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	demohandler "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/demo"
	demoapp "github.com/hanmahong5-arch/lurus-tally/internal/app/demo"
)

type fakeProv struct {
	res   demoapp.Result
	err   error
	calls int
}

func (f *fakeProv) Provision(context.Context) (demoapp.Result, error) {
	f.calls++
	return f.res, f.err
}

func frozen() time.Time { return time.Date(2026, 6, 24, 9, 0, 0, 0, time.UTC) }

func serve(h *demohandler.Handler) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h.RegisterRoutes(r)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/demo/start", nil)
	r.ServeHTTP(w, req)
	return w
}

// TestStart_DisabledIsHidden: with demo mode off the endpoint answers 404 and
// never provisions — a production deployment that mounts the route stays safe.
func TestStart_DisabledIsHidden(t *testing.T) {
	prov := &fakeProv{}
	h := demohandler.New(prov, false, 10)
	w := serve(h)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", w.Code)
	}
	if prov.calls != 0 {
		t.Errorf("provision called %d times, want 0 when disabled", prov.calls)
	}
}

// TestStart_HappyPath: enabled → 200 with the entry credentials.
func TestStart_HappyPath(t *testing.T) {
	tenantID := uuid.New()
	exp := frozen().Add(24 * time.Hour)
	prov := &fakeProv{res: demoapp.Result{TenantID: tenantID, Token: "tally_pat_demo123", ExpiresAt: exp}}
	h := demohandler.New(prov, true, 10)

	w := serve(h)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var body demoapp.Result
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.TenantID != tenantID || body.Token != "tally_pat_demo123" {
		t.Errorf("body=%+v, want tenant %v + token", body, tenantID)
	}
	if prov.calls != 1 {
		t.Errorf("provision called %d times, want 1", prov.calls)
	}
}

// TestStart_RateLimited: a public tenant-creating endpoint must throttle. With a
// frozen clock (no refill) the bucket of 2 is exhausted on the 3rd call → 429.
func TestStart_RateLimited(t *testing.T) {
	prov := &fakeProv{res: demoapp.Result{TenantID: uuid.New(), Token: "x"}}
	h := demohandler.NewWithClock(prov, true, 2, frozen)

	if w := serve(h); w.Code != http.StatusOK {
		t.Fatalf("call 1 status=%d, want 200", w.Code)
	}
	if w := serve(h); w.Code != http.StatusOK {
		t.Fatalf("call 2 status=%d, want 200", w.Code)
	}
	w := serve(h)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("call 3 status=%d, want 429", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("429 should carry Retry-After")
	}
	if prov.calls != 2 {
		t.Errorf("provision called %d times, want 2 (3rd throttled before provision)", prov.calls)
	}
}

// TestStart_ProvisionErrorIs500: a provisioning failure surfaces as 500, not a
// half-built sandbox.
func TestStart_ProvisionErrorIs500(t *testing.T) {
	prov := &fakeProv{err: context.DeadlineExceeded}
	h := demohandler.New(prov, true, 10)
	w := serve(h)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d, want 500", w.Code)
	}
}
