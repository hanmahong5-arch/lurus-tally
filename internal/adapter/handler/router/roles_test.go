package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	handlerbill "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/bill"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/health"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	"github.com/hanmahong5-arch/lurus-tally/internal/domain/team"
)

// roleRouter is the real route table with (almost) no handlers behind it (routes
// answer 501) and an auth stage that takes the role from X-Test-Role. A route
// the role may not use is refused with 403 before it reaches any handler; one it
// may use gets as far as the 501.
func roleRouter() *gin.Engine {
	tenant := uuid.New()
	authMW := func(c *gin.Context) {
		c.Set(middleware.CtxKeyTenantID, tenant)
		c.Set(middleware.CtxKeyShopRole, team.Role(c.GetHeader("X-Test-Role")))
		c.Next()
	}
	saleh := handlerbill.NewSaleHandler(nil, nil, nil, nil, nil, nil, nil)
	return New(health.New("dev"), authMW, nil, nil, nil, nil, nil, nil, nil, nil, nil, saleh, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
}

func TestRoleGates_ByRole(t *testing.T) {
	r := roleRouter()
	cases := []struct {
		role         team.Role
		method, path string
		forbidden    bool
	}{
		// API keys are the owner's alone
		{team.RoleManager, "GET", "/api/v1/auth/pats", true},
		{team.RoleManager, "POST", "/api/v1/auth/pats", true},
		{team.RoleManager, "DELETE", "/api/v1/auth/pats/abc", true},
		{team.RoleClerk, "GET", "/api/v1/auth/pats", true},
		{team.RoleOwner, "GET", "/api/v1/auth/pats", false},
		// a clerk reaches the purchase drafts (the handler narrows them); a cashier does not
		{team.RoleClerk, "GET", "/api/v1/purchase-bills", false},
		{team.RoleClerk, "GET", "/api/v1/purchase-bills/abc", false},
		{team.RoleClerk, "PUT", "/api/v1/purchase-bills/abc", false},
		{team.RoleCashier, "GET", "/api/v1/purchase-bills", true},
		{team.RoleCashier, "PUT", "/api/v1/purchase-bills/abc", true},
		{team.RoleFinance, "GET", "/api/v1/purchase-bills", false},
		// return: the cashier gets in (the handler limits it to today's own sale)
		{team.RoleCashier, "POST", "/api/v1/sale-bills/abc/return", false},
		{team.RoleClerk, "POST", "/api/v1/sale-bills/abc/return", true},
		{team.RoleFinance, "POST", "/api/v1/sale-bills/abc/return", true},
		{team.RoleManager, "POST", "/api/v1/sale-bills/abc/return", false},
		// purchase-side actions stay with the boss and manager
		{team.RoleClerk, "POST", "/api/v1/purchase-bills/abc/approve", true},
		{team.RoleClerk, "POST", "/api/v1/stock/adjustments", false},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.Header.Set("X-Test-Role", string(tc.role))
		req.Header.Set("Idempotency-Key", "k-1")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if got := w.Code == http.StatusForbidden; got != tc.forbidden {
			t.Errorf("%s %s %s: status %d, forbidden=%v want %v", tc.role, tc.method, tc.path, w.Code, got, tc.forbidden)
		}
	}
}
