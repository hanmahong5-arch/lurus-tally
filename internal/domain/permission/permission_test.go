package permission_test

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"testing"

	"github.com/hanmahong5-arch/lurus-tally/internal/domain/permission"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/team"
)

func TestAllowed_Matrix(t *testing.T) {
	cases := []struct {
		role domain.Role
		perm permission.Perm
		want bool
	}{
		{domain.RoleOwner, permission.BillingManage, true},
		{domain.RoleManager, permission.BillingManage, false},
		{domain.RoleManager, permission.BillApprove, true},
		{domain.RoleManager, permission.CostView, true},
		{domain.RoleManager, permission.TeamManage, true},
		{domain.RoleClerk, permission.BillApprove, false},
		{domain.RoleClerk, permission.CostView, false},
		{domain.RoleClerk, permission.BillDraft, true},
		{domain.RoleClerk, permission.ProductWrite, true},
		{domain.RoleClerk, permission.ReportView, false},
		{domain.RoleCashier, permission.BillCheckout, true},
		{domain.RoleCashier, permission.BillDraft, false},
		{domain.RoleCashier, permission.CostView, false},
		{domain.RoleCashier, permission.SettingsManage, false},
		{domain.RoleFinance, permission.CostView, true},
		{domain.RoleFinance, permission.PaymentRecord, true},
		{domain.RoleFinance, permission.BillApprove, false},
		{domain.RoleFinance, permission.StockAdjust, false},
		{domain.RoleFinance, permission.ReportView, true},
		{domain.RoleOwner, permission.TokenManage, true},
		{domain.RoleManager, permission.TokenManage, false},
		{domain.RoleClerk, permission.TokenManage, false},
		{domain.RoleCashier, permission.TokenManage, false},
		{domain.RoleFinance, permission.TokenManage, false},
		{domain.RoleOwner, permission.PurchaseView, true},
		{domain.RoleManager, permission.PurchaseView, true},
		{domain.RoleFinance, permission.PurchaseView, true},
		{domain.RoleClerk, permission.PurchaseView, true},
		{domain.RoleCashier, permission.PurchaseView, false},
		{domain.RoleOwner, permission.BillReturnOwn, true},
		{domain.RoleManager, permission.BillReturnOwn, true},
		{domain.RoleCashier, permission.BillReturnOwn, true},
		{domain.RoleClerk, permission.BillReturnOwn, false},
		{domain.RoleFinance, permission.BillReturnOwn, false},
		{domain.RoleCashier, permission.BillReturn, false},
		{domain.Role(""), permission.BillRead, false},
		{domain.Role("viewer"), permission.BillRead, false},
	}
	for _, tc := range cases {
		if got := permission.Allowed(tc.role, tc.perm); got != tc.want {
			t.Errorf("Allowed(%q,%q)=%v want %v", tc.role, tc.perm, got, tc.want)
		}
	}
}

func TestOnlyOwnerHoldsBilling(t *testing.T) {
	for _, r := range []domain.Role{domain.RoleManager, domain.RoleClerk, domain.RoleCashier, domain.RoleFinance} {
		if permission.Allowed(r, permission.BillingManage) {
			t.Errorf("%s must not manage billing", r)
		}
	}
}

func TestRoutePerm_ValuesAreKnownPerms(t *testing.T) {
	for route, p := range permission.RoutePerm {
		if !permission.Allowed(domain.RoleOwner, p) {
			t.Errorf("route %q needs %q which the owner does not hold", route, p)
		}
	}
}

func TestStripCostFields_NestedAndKeepsNumbers(t *testing.T) {
	in := map[string]any{
		"items":  []any{map[string]any{"unit_cost": "1.5", "qty": "2", "n": json.Number("12345678901234567890")}},
		"margin": "10%",
		"keep":   "x",
	}
	out, err := permission.StripCostFields(in)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	if got := string(b); got != `{"items":[{"n":12345678901234567890,"qty":"2"}],"keep":"x"}` {
		t.Fatalf("got %s", got)
	}
}

func TestRoutePerm_OwnOnlyAndTokenGates(t *testing.T) {
	want := map[string]permission.Perm{
		"POST /api/v1/auth/pats":             permission.TokenManage,
		"GET /api/v1/auth/pats":              permission.TokenManage,
		"DELETE /api/v1/auth/pats/:id":       permission.TokenManage,
		"POST /api/v1/sale-bills/:id/return": permission.BillReturnOwn,
		"GET /api/v1/purchase-bills":         permission.PurchaseView,
		"GET /api/v1/purchase-bills/:id":     permission.PurchaseView,
		"GET /api/v1/account/audit-log":      permission.SettingsManage,
	}
	for key, perm := range want {
		if got := permission.RoutePerm[key]; got != perm {
			t.Errorf("%s: got %q want %q", key, got, perm)
		}
	}
}

func TestCostHiddenContext(t *testing.T) {
	ctx := context.Background()
	if permission.CostHidden(ctx) {
		t.Fatal("a plain context hides nothing")
	}
	if !permission.CostHidden(permission.WithCostHidden(ctx)) {
		t.Fatal("WithCostHidden must mark the context")
	}
}

// matrixFixture is the role -> permissions table the web client mirrors. The
// web test web/lib/roles.test.ts reads the same file, so the two sides cannot
// drift apart. Regenerate with UPDATE_MATRIX=1 go test ./internal/domain/permission/.
func matrixFixture() map[string][]string {
	out := map[string][]string{}
	for _, r := range []domain.Role{domain.RoleOwner, domain.RoleManager, domain.RoleClerk, domain.RoleCashier, domain.RoleFinance} {
		ps := []string{}
		for _, p := range permission.Perms(r) {
			ps = append(ps, string(p))
		}
		sort.Strings(ps)
		out[string(r)] = ps
	}
	return out
}

func TestMatrixFixtureMatchesWebCopy(t *testing.T) {
	const path = "testdata/matrix.json"
	want, err := json.MarshalIndent(matrixFixture(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want = append(want, '\n')
	if os.Getenv("UPDATE_MATRIX") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, want, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v (run with UPDATE_MATRIX=1)", path, err)
	}
	if string(got) != string(want) {
		t.Errorf("%s is stale; run UPDATE_MATRIX=1 go test ./internal/domain/permission/", path)
	}
}

// TestRoutesFixtureMatchesWebCopy writes RoutePerm as testdata/routes.json, the
// table the web role-request fixture (web/lib/role-requests.test.tsx) reads to
// assert no page asks for a route its role cannot call. Regenerate with
// UPDATE_MATRIX=1 go test ./internal/domain/permission/.
func TestRoutesFixtureMatchesWebCopy(t *testing.T) {
	const path = "testdata/routes.json"
	routes := map[string]string{}
	for k, p := range permission.RoutePerm {
		routes[k] = string(p)
	}
	want, err := json.MarshalIndent(routes, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want = append(want, '\n')
	if os.Getenv("UPDATE_MATRIX") == "1" {
		if err := os.WriteFile(path, want, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v (run with UPDATE_MATRIX=1)", path, err)
	}
	if string(got) != string(want) {
		t.Errorf("%s is stale; run UPDATE_MATRIX=1 go test ./internal/domain/permission/", path)
	}
}
