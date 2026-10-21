package router

import (
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	"github.com/hanmahong5-arch/lurus-tally/internal/app/subscription"
)

// unguardedWrites returns the write routes under /api/v1 that are neither on the
// allowlist nor answered with 402 by a locked shop.
func unguardedWrites(engine *gin.Engine) (unguarded []string, checked int) {
	for _, rt := range engine.Routes() {
		if !strings.HasPrefix(rt.Path, "/api/v1/") {
			continue
		}
		switch rt.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			continue
		}
		if middleware.SubscriptionWriteAllowed(rt.Path) {
			continue
		}
		checked++
		w := httptest.NewRecorder()
		req := httptest.NewRequest(rt.Method, rt.Path, strings.NewReader("{}"))
		req.Header.Set("X-Test-Role", "owner")
		req.Header.Set("Content-Type", "application/json")
		engine.ServeHTTP(w, req)
		if w.Code != http.StatusPaymentRequired {
			unguarded = append(unguarded, rt.Method+" "+rt.Path)
		}
	}
	sort.Strings(unguarded)
	return unguarded, checked
}

func lockedGuard() gin.HandlerFunc {
	return middleware.SubscriptionGuard(subscription.Fixed{Locked: true, Reason: subscription.ReasonExpired})
}

// Every write route under /api/v1 must be allow-listed or refused with 402 when
// the plan has lapsed; a new write route mounted outside the guard fails here.
func TestSubscriptionGuard_EveryWriteRouteIsLocked(t *testing.T) {
	bad, checked := unguardedWrites(newProbeEnvGuarded(lockedGuard()).engine)
	if checked < 40 {
		t.Fatalf("only %d write routes checked; router wiring changed?", checked)
	}
	if len(bad) > 0 {
		t.Fatalf("write routes neither allow-listed nor locked (402) on an expired plan:\n%s", strings.Join(bad, "\n"))
	}
}

func TestSubscriptionGuard_ShopifyAdminLockedButReadable(t *testing.T) {
	engine := newProbeEnvGuarded(lockedGuard()).engine
	for _, m := range []string{http.MethodPost, http.MethodDelete} {
		path := "/api/v1/shopify/shops"
		if m == http.MethodDelete {
			path += "/00000000-0000-0000-0000-000000000001"
		}
		w := httptest.NewRecorder()
		req := httptest.NewRequest(m, path, strings.NewReader("{}"))
		req.Header.Set("X-Test-Role", "owner")
		engine.ServeHTTP(w, req)
		if w.Code != http.StatusPaymentRequired {
			t.Errorf("%s %s = %d, want 402", m, path, w.Code)
		}
	}
}

// The detector must catch a write route mounted outside the guard.
func TestSubscriptionGuard_DetectsUnguardedWrite(t *testing.T) {
	t.Run("route without guard is reported", func(t *testing.T) {
		engine := newProbeEnvGuarded(lockedGuard()).engine
		engine.POST("/api/v1/zz-new-write", func(c *gin.Context) { c.Status(http.StatusOK) })
		bad, _ := unguardedWrites(engine)
		if len(bad) != 1 || bad[0] != "POST /api/v1/zz-new-write" {
			t.Fatalf("detector missed the unguarded route, got %v", bad)
		}
	})
	t.Run("route behind the guard is clean", func(t *testing.T) {
		engine := newProbeEnvGuarded(lockedGuard()).engine
		g := engine.Group("/api/v1", func(c *gin.Context) { c.Set(middleware.CtxKeyTenantID, probeTenant) }, lockedGuard())
		g.POST("/zz-new-write", func(c *gin.Context) { c.Status(http.StatusOK) })
		bad, _ := unguardedWrites(engine)
		if len(bad) != 0 {
			t.Fatalf("guarded route reported: %v", bad)
		}
	})
}
