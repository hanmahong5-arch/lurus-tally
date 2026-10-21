package router

import (
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	handleracct "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/account"
	appacct "github.com/hanmahong5-arch/lurus-tally/internal/app/account"
	"github.com/hanmahong5-arch/lurus-tally/internal/domain/permission"
)

// openWrites are the write routes any member of a shop may call without a
// dedicated permission. Every other write route under /api/v1 must be listed in
// permission.RoutePerm, so a new route cannot ship ungated by accident.
var openWrites = map[string]string{
	"POST /api/v1/auth/logout":            "sign out",
	"POST /api/v1/tenant/profile":         "first-time shop setup, before any role exists",
	"POST /api/v1/team/join":              "joining a shop by invite, before any role exists",
	"PUT /api/v1/account/profile":         "own profile",
	"POST /api/v1/account/avatar":         "own avatar",
	"DELETE /api/v1/account/sessions/:id": "own sessions",
	// A device leaves cashier mode with the PIN; the cashier who holds that device
	// has no settings permission. Wrong guesses are rate-limited per member.
	"POST /api/v1/tenant/cashier-mode/verify": "leave cashier mode with the PIN",
}

func TestRoutes_EveryWriteIsGatedOrExplicitlyOpen(t *testing.T) {
	r := newProbeEnv().engine
	var ungated []string
	gated := 0
	for _, rt := range r.Routes() {
		if !strings.HasPrefix(rt.Path, "/api/v1/") || rt.Method == http.MethodGet {
			continue
		}
		key := rt.Method + " " + rt.Path
		if _, ok := permission.ForRoute(rt.Method, rt.Path); ok {
			gated++
			continue
		}
		if _, ok := openWrites[key]; ok {
			continue
		}
		ungated = append(ungated, key)
	}
	sort.Strings(ungated)
	if len(ungated) > 0 {
		t.Fatalf("write routes with no permission and not in openWrites:\n%s", strings.Join(ungated, "\n"))
	}
	if gated < 40 {
		t.Fatalf("only %d gated write routes found; router wiring changed?", gated)
	}
}

// Every open write needs a written reason, and must still be a registered
// route: a stale entry would keep a hole open for a path that may come back
// under another handler. The probe router mounts every optional module,
// cashier mode included, so no entry is exempt from the registration check.
func TestRoutes_OpenWritesHaveReasonsAndExist(t *testing.T) {
	registered := map[string]bool{}
	for _, rt := range newProbeEnv().engine.Routes() {
		registered[rt.Method+" "+rt.Path] = true
	}
	for key, reason := range openWrites {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("openWrites %q has no reason", key)
		}
		method := strings.SplitN(key, " ", 2)[0]
		if method == http.MethodGet {
			t.Errorf("openWrites %q is a read; reads are guarded by openGets", key)
		}
		if _, gated := permission.ForRoute(method, strings.SplitN(key, " ", 2)[1]); gated {
			t.Errorf("openWrites %q is already gated by RoutePerm; drop the exemption", key)
		}
		if !registered[key] {
			t.Errorf("openWrites %q matches no registered route", key)
		}
	}
}

func TestRoutes_ApproveVoidReturnPayBillingTeamAreGated(t *testing.T) {
	want := map[string]permission.Perm{
		"POST /api/v1/sale-bills/:id/approve":     permission.BillApprove,
		"POST /api/v1/purchase-bills/:id/approve": permission.BillApprove,
		"POST /api/v1/sale-bills/:id/cancel":      permission.BillCancel,
		"POST /api/v1/sale-bills/:id/return":      permission.BillReturnOwn,
		"POST /api/v1/payments":                   permission.PaymentRecord,
		"POST /api/v1/billing/subscribe":          permission.BillingManage,
		"GET /api/v1/billing/overview":            permission.BillingManage,
		"POST /api/v1/team/invites":               permission.TeamManage,
		"POST /api/v1/auth/pats":                  permission.TokenManage,
		"GET /api/v1/auth/pats":                   permission.TokenManage,
		"DELETE /api/v1/auth/pats/:id":            permission.TokenManage,
	}
	registered := map[string]bool{}
	for _, rt := range newProbeEnv().engine.Routes() {
		registered[rt.Method+" "+rt.Path] = true
	}
	for key, perm := range want {
		parts := strings.SplitN(key, " ", 2)
		got, ok := permission.ForRoute(parts[0], parts[1])
		if !ok || got != perm {
			t.Errorf("%s: gated by %q (ok=%v), want %q", key, got, ok, perm)
		}
		if !registered[key] {
			t.Errorf("%s is not a registered route", key)
		}
	}
}

// A typo in a RoutePerm key would leave that route silently ungated, so every
// key must name a route the probe router registers (it mounts every optional
// module, the same table the GET guard enumerates).
func TestRoutes_EveryRoutePermKeyIsARegisteredRoute(t *testing.T) {
	registered := map[string]bool{}
	for _, rt := range newProbeEnv().engine.Routes() {
		registered[rt.Method+" "+rt.Path] = true
	}
	for key := range permission.RoutePerm {
		if registered[key] {
			continue
		}
		t.Errorf("RoutePerm key %q matches no registered route", key)
	}
}

// The cashier-mode routes are also checked on a bare router wired by hand, so
// the PUT + verify pair is pinned even if the shared fixture stops mounting it.
func TestRoutes_CashierModeWritesAreGatedOrOpen(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	handleracct.New(nil, nil, nil, nil, nil, nil, nil).
		WithCashierMode(appacct.NewGetCashierMode(nil), appacct.NewSetCashierMode(nil)).
		RegisterRoutes(e.Group("/api/v1"))
	seen := 0
	for _, rt := range e.Routes() {
		if !strings.Contains(rt.Path, "/tenant/cashier-mode") || rt.Method == http.MethodGet {
			continue
		}
		seen++
		key := rt.Method + " " + rt.Path
		if _, ok := permission.ForRoute(rt.Method, rt.Path); ok {
			continue
		}
		if _, ok := openWrites[key]; !ok {
			t.Errorf("%s has no permission and is not in openWrites", key)
		}
	}
	if seen != 2 {
		t.Fatalf("cashier-mode write routes = %d, want 2 (PUT + verify)", seen)
	}
}
