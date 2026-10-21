package billing_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	handlerbilling "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/billing"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appbilling "github.com/hanmahong5-arch/lurus-tally/internal/app/billing"
	"github.com/hanmahong5-arch/lurus-tally/internal/app/subscription"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/platformclient"
)

type fakeAcct struct{}

func (fakeAcct) PlanAccountID(context.Context, uuid.UUID) (int64, bool, error) { return 1, true, nil }

type fakePlatform struct {
	status string
	calls  int
}

func (f *fakePlatform) Subscription(context.Context, int64) (*subscription.Snapshot, error) {
	f.calls++
	return &subscription.Snapshot{PlanCode: "pro", Status: f.status}, nil
}

func tenantEngine(h *handlerbilling.Handler, tid uuid.UUID) *gin.Engine {
	e := gin.New()
	e.Use(func(c *gin.Context) {
		c.Set(middleware.CtxKeyIDPSubject, "sub-abc")
		c.Set(middleware.CtxKeyTenantID, tid)
		c.Next()
	})
	h.RegisterRoutes(e.Group("/api/v1"))
	return e
}

func getState(e *gin.Engine, q string) string {
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/subscription/state"+q, nil))
	return rec.Body.String()
}

func TestLockState_RefreshThrottledTo10s(t *testing.T) {
	plat := &fakePlatform{status: "expired"}
	svc := subscription.NewService(fakeAcct{}, plat)
	now := time.Now()
	h := handlerbilling.New(nil, nil, nil).WithLock(svc).WithClock(func() time.Time { return now })
	e := tenantEngine(h, uuid.New())

	if b := getState(e, ""); !strings.Contains(b, `"locked":true`) || !strings.Contains(b, `"refreshed":false`) {
		t.Fatalf("plain: %s", b)
	}
	plat.status = "active"
	if b := getState(e, ""); !strings.Contains(b, `"locked":true`) {
		t.Fatalf("cache expected: %s", b)
	}
	if b := getState(e, "?refresh=1"); !strings.Contains(b, `"locked":false`) || !strings.Contains(b, `"refreshed":true`) {
		t.Fatalf("first refresh: %s", b)
	}
	before := plat.calls
	plat.status = "expired"
	now = now.Add(5 * time.Second)
	if b := getState(e, "?refresh=1"); !strings.Contains(b, `"refreshed":false`) || !strings.Contains(b, `"locked":false`) {
		t.Fatalf("throttled refresh: %s", b)
	}
	if plat.calls != before {
		t.Fatalf("throttled refresh hit platform: %d -> %d", before, plat.calls)
	}
	now = now.Add(6 * time.Second)
	if b := getState(e, "?refresh=1"); !strings.Contains(b, `"locked":true`) || !strings.Contains(b, `"refreshed":true`) {
		t.Fatalf("second refresh: %s", b)
	}
}

func TestSubscribe_SuccessInvalidatesState(t *testing.T) {
	plat := &fakePlatform{status: "expired"}
	svc := subscription.NewService(fakeAcct{}, plat)
	sub := &stubSubscribe{out: &appbilling.SubscribeOutput{
		Subscription: &platformclient.SubscriptionSnapshot{PlanCode: "pro", Status: "active"},
	}}
	h := handlerbilling.New(sub, nil, nil).WithLock(svc)
	e := tenantEngine(h, uuid.New())
	if b := getState(e, ""); !strings.Contains(b, `"locked":true`) {
		t.Fatalf("want locked: %s", b)
	}
	plat.status = "active"
	req := httptest.NewRequest(http.MethodPost, "/api/v1/billing/subscribe",
		strings.NewReader(`{"plan_code":"pro","billing_cycle":"monthly","payment_method":"wallet"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("subscribe: %d %s", rec.Code, rec.Body.String())
	}
	if b := getState(e, ""); !strings.Contains(b, `"locked":false`) {
		t.Fatalf("state must reflect new plan at once: %s", b)
	}
}
