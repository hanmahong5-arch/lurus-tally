package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/app/subscription"
)

type lockedChecker struct{ calls int }

func (l *lockedChecker) State(context.Context, uuid.UUID) subscription.State {
	l.calls++
	return subscription.State{Locked: true, Reason: subscription.ReasonExpired, PlanCode: "pro", ExpiresAt: "2026-01-01T00:00:00Z"}
}

func guardEngine(chk subscription.Checker) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(CtxKeyTenantID, uuid.New()); c.Next() })
	r.Use(SubscriptionGuard(chk))
	r.Any("/*path", func(c *gin.Context) { c.Status(http.StatusOK) })
	return r
}

func do(r *gin.Engine, method, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader("{}")))
	return w
}

func TestSubscriptionGuardLocksWrites(t *testing.T) {
	r := guardEngine(&lockedChecker{})
	w := do(r, http.MethodPost, "/api/v1/sale-bills")
	if w.Code != http.StatusPaymentRequired {
		t.Fatalf("POST sale-bills = %d, want 402", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{`"code":"subscription_expired"`, "套餐已到期，现在只能查看和导出，续费后恢复开单。", `"plan_code":"pro"`, `"expires_at"`} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %s: %s", want, body)
		}
	}
	for _, m := range []string{http.MethodPut, http.MethodDelete, http.MethodPatch} {
		if got := do(r, m, "/api/v1/products/1").Code; got != http.StatusPaymentRequired {
			t.Errorf("%s = %d, want 402", m, got)
		}
	}
	// Lookalike prefixes must not slip through.
	for _, p := range []string{"/api/v1/accounts", "/api/v1/menu", "/api/v1/billingx", "/api/v1/team/invites", "/api/v1/tenant/cashier-mode"} {
		if got := do(r, http.MethodPost, p).Code; got != http.StatusPaymentRequired {
			t.Errorf("POST %s = %d, want 402", p, got)
		}
	}
}

func TestSubscriptionGuardReadsAlwaysPass(t *testing.T) {
	chk := &lockedChecker{}
	r := guardEngine(chk)
	for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		if got := do(r, m, "/api/v1/sale-bills").Code; got != http.StatusOK {
			t.Errorf("%s = %d, want 200", m, got)
		}
	}
	if chk.calls != 0 {
		t.Errorf("safe methods must not consult the checker, calls=%d", chk.calls)
	}
}

func TestSubscriptionGuardAllowlist(t *testing.T) {
	r := guardEngine(&lockedChecker{})
	paths := []string{
		"/api/v1/auth/logout",
		"/api/v1/auth/pats",
		"/api/v1/me",
		"/api/v1/billing/subscribe",
		"/api/v1/exports/bills.csv",
		"/api/v1/team/join",
		"/api/v1/tenant/profile",
		"/api/v1/account/profile",
		"/api/v1/account/avatar",
		"/api/v1/tenant/cashier-mode/verify",
	}
	for _, p := range paths {
		if got := do(r, http.MethodPost, p).Code; got != http.StatusOK {
			t.Errorf("POST %s = %d, want 200 (allow-listed)", p, got)
		}
	}
	// Every table entry is covered above.
	for _, p := range SubscriptionWriteAllowlist {
		if !SubscriptionWriteAllowed(p) {
			t.Errorf("allowlist entry %s not allowed", p)
		}
	}
}

func TestSubscriptionGuardOpenWhenNotLocked(t *testing.T) {
	for _, chk := range []subscription.Checker{nil, subscription.Never{}} {
		if got := do(guardEngine(chk), http.MethodPost, "/api/v1/sale-bills").Code; got != http.StatusOK {
			t.Errorf("unlocked POST = %d, want 200", got)
		}
	}
}

func TestSubscriptionGuardNilTenantPassesThrough(t *testing.T) {
	chk := &lockedChecker{}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(SubscriptionGuard(chk))
	r.Any("/*path", func(c *gin.Context) { c.Status(http.StatusOK) })
	if got := do(r, http.MethodPost, "/api/v1/sale-bills").Code; got != http.StatusOK {
		t.Fatalf("no tenant = %d, want 200 (left to the auth layer)", got)
	}
	if chk.calls != 0 {
		t.Errorf("checker called %d times for a request without a shop", chk.calls)
	}
}
