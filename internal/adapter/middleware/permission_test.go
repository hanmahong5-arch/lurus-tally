package middleware_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	"github.com/hanmahong5-arch/lurus-tally/internal/domain/permission"
	"github.com/hanmahong5-arch/lurus-tally/internal/domain/team"
)

var permTenant = uuid.MustParse("a21b880b-9d5d-481f-8dfa-daf2cdeb564c")

// permEngine builds an engine whose fake auth injects tenant + sub, then runs
// WithShopRole(resolve) and EnforceRoutePerms in front of two routes.
func permEngine(resolve middleware.RoleResolver, sub string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/api/v1")
	g.Use(func(c *gin.Context) {
		c.Set(middleware.CtxKeyTenantID, permTenant)
		if sub != "" {
			c.Set(middleware.CtxKeyIDPSubject, sub)
		}
		c.Next()
	})
	g.Use(middleware.WithShopRole(resolve, nil))
	g.Use(middleware.EnforceRoutePerms())
	ok := func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"cost": middleware.CanViewCost(c), "tenant": middleware.GetTenantID(c)})
	}
	g.POST("/sale-bills/:id/approve", ok)
	g.GET("/billing/overview", ok)
	g.GET("/products", ok)
	return r
}

func fixed(role team.Role, err error) middleware.RoleResolver {
	return func(context.Context, uuid.UUID, string) (team.Role, error) { return role, err }
}

func do(r *gin.Engine, method, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(method, path, nil)
	r.ServeHTTP(w, req)
	return w
}

func TestEnforceRoutePerms_ByRole(t *testing.T) {
	cases := []struct {
		role         team.Role
		method, path string
		want         int
	}{
		{team.RoleClerk, "POST", "/api/v1/sale-bills/x/approve", 403},
		{team.RoleCashier, "POST", "/api/v1/sale-bills/x/approve", 403},
		{team.RoleFinance, "POST", "/api/v1/sale-bills/x/approve", 403},
		{team.RoleManager, "POST", "/api/v1/sale-bills/x/approve", 200},
		{team.RoleOwner, "POST", "/api/v1/sale-bills/x/approve", 200},
		{team.RoleManager, "GET", "/api/v1/billing/overview", 403},
		{team.RoleOwner, "GET", "/api/v1/billing/overview", 200},
		{team.RoleClerk, "GET", "/api/v1/products", 200},
	}
	for _, tc := range cases {
		w := do(permEngine(fixed(tc.role, nil), "sub-1"), tc.method, tc.path)
		if w.Code != tc.want {
			t.Errorf("%s %s %s: got %d want %d (%s)", tc.role, tc.method, tc.path, w.Code, tc.want, w.Body.String())
		}
	}
}

func TestEnforceRoutePerms_ForbiddenBodyIsChinese(t *testing.T) {
	w := do(permEngine(fixed(team.RoleClerk, nil), "s"), "POST", "/api/v1/sale-bills/x/approve")
	body := w.Body.String()
	if w.Code != 403 || !contains(body, `"error":"forbidden"`) || !contains(body, "审核单据") {
		t.Fatalf("unexpected: %d %s", w.Code, body)
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }

func TestWithShopRole_PATActsAsManagerNotOwner(t *testing.T) {
	r := permEngine(fixed(team.RoleOwner, nil), "") // no subject -> automation
	if w := do(r, "POST", "/api/v1/sale-bills/x/approve"); w.Code != 200 {
		t.Fatalf("pat approve: %d", w.Code)
	}
	if w := do(r, "GET", "/api/v1/billing/overview"); w.Code != 403 {
		t.Fatalf("pat must not reach billing: %d", w.Code)
	}
}

func TestWithShopRole_NotMemberLosesTenant(t *testing.T) {
	r := permEngine(fixed("", middleware.ErrNotShopMember), "ghost")
	w := do(r, "GET", "/api/v1/products")
	if w.Code != 200 || !contains(w.Body.String(), uuid.Nil.String()) {
		t.Fatalf("tenant must be dropped: %d %s", w.Code, w.Body.String())
	}
	if w := do(r, "POST", "/api/v1/sale-bills/x/approve"); w.Code != 200 {
		// no tenant: the handler itself answers 401 in production
		t.Fatalf("tenantless request is left to the handler: %d", w.Code)
	}
}

func TestWithShopRole_LookupFailureIs503AndNeverOwner(t *testing.T) {
	r := permEngine(fixed("", errors.New("db down")), "s")
	w := do(r, "GET", "/api/v1/products")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d", w.Code)
	}
}

func TestEnforceRoutePerms_ShopBoundWithoutRoleFailsClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/api/v1")
	g.Use(func(c *gin.Context) { c.Set(middleware.CtxKeyTenantID, permTenant); c.Next() })
	g.Use(middleware.EnforceRoutePerms())
	g.GET("/products", func(c *gin.Context) { c.Status(200) })
	if w := do(r, "GET", "/api/v1/products"); w.Code != 403 {
		t.Fatalf("got %d", w.Code)
	}
}

func TestRequirePerm(t *testing.T) {
	gin.SetMode(gin.TestMode)
	build := func(role team.Role) *gin.Engine {
		r := gin.New()
		r.Use(func(c *gin.Context) {
			c.Set(middleware.CtxKeyTenantID, permTenant)
			c.Set(middleware.CtxKeyShopRole, role)
		})
		r.GET("/x", middleware.RequirePerm(permission.CostView), func(c *gin.Context) { c.Status(200) })
		return r
	}
	if w := do(build(team.RoleClerk), "GET", "/x"); w.Code != 403 {
		t.Fatalf("clerk: %d", w.Code)
	}
	if w := do(build(team.RoleFinance), "GET", "/x"); w.Code != 200 {
		t.Fatalf("finance: %d", w.Code)
	}
}

func TestHasPerm_NoRoleInContextIsUnitTestMode(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	if !middleware.CanViewCost(c) {
		t.Fatal("bare context should not trim")
	}
	c.Set(middleware.CtxKeyShopRole, team.RoleCashier)
	if middleware.CanViewCost(c) {
		t.Fatal("cashier must not see cost")
	}
}

func TestJSONTrimmed_StripsCostForNonViewers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := gin.H{"items": []gin.H{{"on_hand_qty": "3", "unit_cost": "9.5", "cost_strategy": "wac", "total_cost": "28.5"}}}
	render := func(role team.Role) string {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set(middleware.CtxKeyShopRole, role)
		middleware.JSONTrimmed(c, http.StatusOK, body)
		return w.Body.String()
	}
	if got := render(team.RoleOwner); !strings.Contains(got, "unit_cost") {
		t.Errorf("owner must see cost: %s", got)
	}
	got := render(team.RoleClerk)
	for _, k := range []string{"unit_cost", "cost_strategy", "total_cost"} {
		if strings.Contains(got, k) {
			t.Errorf("clerk body still has %s: %s", k, got)
		}
	}
	if !strings.Contains(got, "on_hand_qty") {
		t.Errorf("quantity must stay: %s", got)
	}
}

func TestTrimCostResponse_OnlyListedRouteAndOnlyNonViewers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	build := func(role team.Role) *gin.Engine {
		r := gin.New()
		r.Use(func(c *gin.Context) { c.Set(middleware.CtxKeyShopRole, role); c.Next() })
		r.Use(middleware.TrimCostResponse("/summary"))
		h := func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"replenish": gin.H{"count": 2, "amount_cny": "88.00"}})
		}
		r.GET("/summary", h)
		r.GET("/other", h)
		return r
	}
	if w := do(build(team.RoleClerk), "GET", "/summary"); strings.Contains(w.Body.String(), "amount_cny") || !strings.Contains(w.Body.String(), `"count":2`) {
		t.Errorf("clerk summary: %s", w.Body.String())
	}
	if w := do(build(team.RoleOwner), "GET", "/summary"); !strings.Contains(w.Body.String(), "amount_cny") {
		t.Errorf("owner summary: %s", w.Body.String())
	}
	if w := do(build(team.RoleClerk), "GET", "/other"); !strings.Contains(w.Body.String(), "amount_cny") {
		t.Errorf("unlisted route must be untouched: %s", w.Body.String())
	}
	if w := do(build(team.RoleClerk), "GET", "/summary"); w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		t.Errorf("status/content-type: %d %s", w.Code, w.Header().Get("Content-Type"))
	}
}

func TestTrimTodoSummary_ByPermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	build := func(role team.Role) *gin.Engine {
		r := gin.New()
		r.Use(func(c *gin.Context) { c.Set(middleware.CtxKeyShopRole, role); c.Next() })
		r.Use(middleware.TrimTodoSummary("/todo"))
		r.GET("/todo", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"draft_purchases":   gin.H{"count": 1, "items": []gin.H{{"total_amount": "200.00"}}},
				"draft_sales":       gin.H{"count": 1, "items": []gin.H{{"total_amount": "50.00"}}},
				"overdue_customers": gin.H{"count": 1, "items": []gin.H{{"unpaid_amount": "9.00"}}},
				"uninvoiced_sales":  gin.H{"count": 1, "items": []gin.H{{"total_amount": "7.00"}}},
				"kpis":              gin.H{"today_sales": "5.00", "today_collected": "4.00", "receivable_unpaid": "3.00", "month_uninvoiced": "2.00"},
			})
		})
		return r
	}
	owner := do(build(team.RoleOwner), "GET", "/todo").Body.String()
	for _, want := range []string{"200.00", "9.00", "receivable_unpaid", "today_collected"} {
		if !strings.Contains(owner, want) {
			t.Errorf("owner must see %s: %s", want, owner)
		}
	}
	for _, role := range []team.Role{team.RoleClerk, team.RoleCashier} {
		body := do(build(role), "GET", "/todo").Body.String()
		if !permission.Allowed(role, permission.CostView) && strings.Contains(body, "200.00") {
			t.Errorf("%s: purchase total leaked: %s", role, body)
		}
		if !permission.Allowed(role, permission.PaymentRead) {
			for _, bad := range []string{"9.00", "7.00", "receivable_unpaid", "today_collected", "month_uninvoiced"} {
				if strings.Contains(body, bad) {
					t.Errorf("%s: %s leaked: %s", role, bad, body)
				}
			}
		}
		if !strings.Contains(body, "50.00") || !strings.Contains(body, "today_sales") {
			t.Errorf("%s: sales part must stay: %s", role, body)
		}
	}
}

func TestTrimTodoSummary_TotalIsRecomputedFromSections(t *testing.T) {
	gin.SetMode(gin.TestMode)
	build := func(role team.Role) *gin.Engine {
		r := gin.New()
		r.Use(func(c *gin.Context) { c.Set(middleware.CtxKeyShopRole, role); c.Next() })
		r.Use(middleware.TrimTodoSummary("/todo"))
		r.GET("/todo", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"draft_purchases":   gin.H{"count": 4, "items": []gin.H{}},
				"draft_sales":       gin.H{"count": 2, "items": []gin.H{}},
				"low_stock":         gin.H{"count": 3, "items": []gin.H{}},
				"overdue_customers": gin.H{"count": 5, "items": []gin.H{}},
				"uninvoiced_sales":  gin.H{"count": 1, "items": []gin.H{}},
				"kpis":              gin.H{},
				"total":             15,
			})
		})
		return r
	}
	cases := []struct {
		role team.Role
		want string
	}{
		{team.RoleOwner, `"total":15`},
		{team.RoleClerk, `"total":5`},   // sales 2 + low stock 3; purchases, debtors, invoices dropped
		{team.RoleCashier, `"total":5`}, // same
		{team.RoleFinance, `"total":15`},
	}
	for _, tc := range cases {
		body := do(build(tc.role), "GET", "/todo").Body.String()
		if !strings.Contains(body, tc.want) {
			t.Errorf("%s: want %s in %s", tc.role, tc.want, body)
		}
	}
}

func TestMarkCostHidden_OnlyWithoutCostView(t *testing.T) {
	gin.SetMode(gin.TestMode)
	probe := func(role team.Role) bool {
		r := gin.New()
		r.Use(func(c *gin.Context) { c.Set(middleware.CtxKeyShopRole, role); c.Next() })
		r.Use(middleware.MarkCostHidden())
		hidden := false
		r.GET("/x", func(c *gin.Context) { hidden = permission.CostHidden(c.Request.Context()); c.Status(200) })
		do(r, "GET", "/x")
		return hidden
	}
	for role, want := range map[team.Role]bool{
		team.RoleOwner: false, team.RoleManager: false, team.RoleFinance: false,
		team.RoleClerk: true, team.RoleCashier: true,
	} {
		if got := probe(role); got != want {
			t.Errorf("%s: hidden=%v want %v", role, got, want)
		}
	}
}

func TestEnforceRoutePerms_ReturnAndPurchaseDoors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	build := func(role team.Role) *gin.Engine {
		r := gin.New()
		g := r.Group("/api/v1")
		g.Use(func(c *gin.Context) {
			c.Set(middleware.CtxKeyTenantID, permTenant)
			c.Set(middleware.CtxKeyShopRole, role)
			c.Next()
		})
		g.Use(middleware.EnforceRoutePerms())
		ok := func(c *gin.Context) { c.Status(200) }
		g.POST("/sale-bills/:id/return", ok)
		g.GET("/purchase-bills", ok)
		g.GET("/purchase-bills/:id", ok)
		g.POST("/auth/pats", ok)
		g.GET("/auth/pats", ok)
		g.DELETE("/auth/pats/:id", ok)
		return r
	}
	cases := []struct {
		role         team.Role
		method, path string
		want         int
	}{
		{team.RoleCashier, "POST", "/api/v1/sale-bills/x/return", 200},
		{team.RoleClerk, "POST", "/api/v1/sale-bills/x/return", 403},
		{team.RoleFinance, "POST", "/api/v1/sale-bills/x/return", 403},
		{team.RoleClerk, "GET", "/api/v1/purchase-bills", 200},
		{team.RoleClerk, "GET", "/api/v1/purchase-bills/x", 200},
		{team.RoleCashier, "GET", "/api/v1/purchase-bills", 403},
		{team.RoleCashier, "GET", "/api/v1/purchase-bills/x", 403},
		{team.RoleFinance, "GET", "/api/v1/purchase-bills", 200},
		{team.RoleManager, "GET", "/api/v1/auth/pats", 403},
		{team.RoleManager, "POST", "/api/v1/auth/pats", 403},
		{team.RoleManager, "DELETE", "/api/v1/auth/pats/x", 403},
		{team.RoleOwner, "GET", "/api/v1/auth/pats", 200},
	}
	for _, tc := range cases {
		if w := do(build(tc.role), tc.method, tc.path); w.Code != tc.want {
			t.Errorf("%s %s %s: got %d want %d", tc.role, tc.method, tc.path, w.Code, tc.want)
		}
	}
}

func TestWithShopRole_OwnerKeepsReadingProducts(t *testing.T) {
	w := do(permEngine(fixed(team.RoleOwner, nil), "boss"), "GET", "/api/v1/products")
	if w.Code != 200 || !contains(w.Body.String(), permTenant.String()) {
		t.Fatalf("owner must keep the shop on /products: %d %s", w.Code, w.Body.String())
	}
}

func TestWriteForbidden_TokenManageNamesOwnerOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/t", func(c *gin.Context) { middleware.WriteForbidden(c, permission.TokenManage) })
	w := do(r, "GET", "/t")
	if w.Code != 403 || !contains(w.Body.String(), "请联系老板") || contains(w.Body.String(), "老板或店长") {
		t.Fatalf("unexpected: %d %s", w.Code, w.Body.String())
	}
}

func TestTrimCostResponse_UndecodableBodyIsA500NotTheOriginal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(middleware.CtxKeyShopRole, team.RoleClerk); c.Next() })
	r.Use(middleware.TrimCostResponse("/summary"))
	r.GET("/summary", func(c *gin.Context) {
		c.Data(http.StatusOK, "application/json; charset=utf-8", []byte(`{"amount_cny": "88.00", `))
	})
	w := do(r, "GET", "/summary")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "amount_cny") {
		t.Errorf("the untrimmed body must not be echoed: %s", w.Body.String())
	}
}

func TestTrimCostResponse_500DropsHeadersTheHandlerSet(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Header("X-Request-Id", "req-1"); c.Next() })
	r.Use(func(c *gin.Context) { c.Set(middleware.CtxKeyShopRole, team.RoleClerk); c.Next() })
	r.Use(middleware.TrimCostResponse("/summary"))
	r.GET("/summary", func(c *gin.Context) {
		c.Header("ETag", `"abc"`)
		c.Header("Content-Disposition", "attachment; filename=cost.json")
		c.Header("Content-Length", "24")
		c.Data(http.StatusOK, "application/json; charset=utf-8", []byte(`{"amount_cny": "88.00", `))
	})
	w := do(r, "GET", "/summary")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500: %s", w.Code, w.Body.String())
	}
	for _, h := range []string{"Etag", "Content-Disposition", "Content-Length"} {
		if v := w.Header().Get(h); v != "" {
			t.Errorf("header %s = %q must not survive a rewrite failure", h, v)
		}
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("error response lost its content type: %q", ct)
	}
	if id := w.Header().Get("X-Request-Id"); id != "req-1" {
		t.Errorf("X-Request-Id = %q, want it kept after a rewrite failure", id)
	}
}
