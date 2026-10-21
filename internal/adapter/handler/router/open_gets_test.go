package router

import (
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/hanmahong5-arch/lurus-tally/internal/domain/permission"
	"github.com/hanmahong5-arch/lurus-tally/internal/domain/team"
)

// probe is one request made to an open GET route.
//
//   - visible is a marker every role must see in a 200 body, so an empty stub
//     cannot make the probe pass vacuously;
//   - secrets are sentinel strings the owner must see (proving the stub carries
//     them) and the clerk and cashier must not;
//   - hiddenStatus is the status the clerk and cashier are expected to get
//     (200 when zero); ownerStatus likewise for the owner;
//   - payOK exempts the probe from the payment-key check: the route carries
//     collection figures on purpose (see the reason of its openGet). The basis
//     is the owner's collection ruling of 2026-10-12: whoever can see a sale
//     bill sees its receivable / unpaid / receipts, and the till takings on the
//     home page; these are not cost. Cost, purchase price, margin and purchase
//     amounts stay hidden from clerk and cashier regardless.
type probe struct {
	path         string
	visible      string
	secrets      []string
	hiddenStatus int
	ownerStatus  int
	payOK        bool
	search       bool
	// never are sentinels no role may get, the owner included (rows the route must filter out).
	never []string
	// hiddenKeys are JSON keys the clerk and cashier must not get (the owner may).
	hiddenKeys []string
}

// openGet is one GET route that needs no permission. Every GET route not in
// permission.RoutePerm must be listed here with the reason it is safe to leave
// open, so a new read route cannot ship open by accident. Routes that can carry
// shop money are probed; so is every other open route, so a money field added to
// one later is caught. Each entry carries at least one probe.
type openGet struct {
	reason string
	probes []probe
}

const (
	pidS  = "22222222-2222-2222-2222-222222222222"
	widS  = "33333333-3333-3333-3333-333333333333"
	cidS  = "44444444-4444-4444-4444-444444444444"
	saleS = "55555555-5555-5555-5555-555555555555"
	purS  = "66666666-6666-6666-6666-666666666666"
)

var openGets = map[string]openGet{
	"GET /api/v1/me":                            {reason: "own identity and role", probes: []probe{{path: "/api/v1/me", visible: "我的名字"}}},
	"GET /api/v1/subscription/state":            {reason: "whether the shop is read-only after the plan lapsed; no money, every role sees the banner", probes: []probe{{path: "/api/v1/subscription/state", visible: "\"locked\":false"}}},
	"GET /api/v1/account/profile":               {reason: "own profile", probes: []probe{{path: "/api/v1/account/profile", visible: "我的档案"}}},
	"GET /api/v1/account/avatar":                {reason: "own avatar", probes: []probe{{path: "/api/v1/account/avatar", visible: "AVATAR-BYTES"}}},
	"GET /api/v1/account/sessions":              {reason: "own sessions", probes: []probe{{path: "/api/v1/account/sessions", visible: "probe-browser"}}},
	"GET /api/v1/tenant/print-settings":         {reason: "receipt header and footer text; the cashier prints with it", probes: []probe{{path: "/api/v1/tenant/print-settings", visible: "示例小店"}}},
	"GET /api/v1/tenant/cashier-mode":           {reason: "whether the till is locked to cashier mode; the hash and salt never leave the server", probes: []probe{{path: "/api/v1/tenant/cashier-mode", visible: "has_pin", never: []string{secretPinHash}}}},
	"GET /api/v1/currencies":                    {reason: "static currency list", probes: []probe{{path: "/api/v1/currencies", visible: "人民币"}}},
	"GET /api/v1/exchange-rates":                {reason: "public rates, no shop money", probes: []probe{{path: "/api/v1/exchange-rates?from=USD&to=CNY", visible: "7.13"}}},
	"GET /api/v1/exchange-rates/history":        {reason: "public rates, no shop money", probes: []probe{{path: "/api/v1/exchange-rates/history?from=USD&to=CNY", visible: "7.14"}}},
	"GET /api/v1/units":                         {reason: "unit master data", probes: []probe{{path: "/api/v1/units", visible: "公斤"}}},
	"GET /api/v1/warehouses":                    {reason: "warehouse master data, needed to ring up a sale", probes: []probe{{path: "/api/v1/warehouses", visible: "主仓库"}}},
	"GET /api/v1/warehouses/:id":                {reason: "warehouse master data", probes: []probe{{path: "/api/v1/warehouses/" + widS, visible: "主仓库"}}},
	"GET /api/v1/nursery-dict":                  {reason: "plant dictionary, no shop money", probes: []probe{{path: "/api/v1/nursery-dict", visible: "红叶石楠"}}},
	"GET /api/v1/nursery-dict/:id":              {reason: "plant dictionary, no shop money", probes: []probe{{path: "/api/v1/nursery-dict/" + cidS, visible: "红叶石楠"}}},
	"GET /api/v1/products/:id/units":            {reason: "unit conversion ratios, no money", probes: []probe{{path: "/api/v1/products/" + pidS + "/units", visible: "整箱"}}},
	"GET /internal/v1/metrics":                  {reason: "service metrics, outside the shop API", probes: []probe{{path: "/internal/v1/metrics", visible: "# HELP"}}},
	"GET /internal/v1/tally/health":             {reason: "liveness", probes: []probe{{path: "/internal/v1/tally/health", visible: "\"status\":\"ok\""}}},
	"GET /internal/v1/tally/ready":              {reason: "readiness", probes: []probe{{path: "/internal/v1/tally/ready", visible: "\"status\":\"ok\""}}},
	"GET /api/v1/customers/import/template.csv": {reason: "blank import template with two example rows; holds no shop data", probes: []probe{{path: "/api/v1/customers/import/template.csv", visible: "客户名称"}}},
	"GET /api/v1/suppliers/import/template.csv": {reason: "blank import template with two example rows; holds no shop data", probes: []probe{{path: "/api/v1/suppliers/import/template.csv", visible: "供应商名称"}}},
	"GET /api/v1/customers":                     {reason: "customer master data, needed to ring up a sale; no balances", probes: []probe{{path: "/api/v1/customers", visible: "老王"}}},
	"GET /api/v1/customers/:id":                 {reason: "customer master data; no balances", probes: []probe{{path: "/api/v1/customers/" + cidS, visible: "老王"}}},
	"GET /api/v1/suppliers":                     {reason: "supplier master data, needed to draft a purchase; no balances", probes: []probe{{path: "/api/v1/suppliers", visible: "供应商甲"}}},
	"GET /api/v1/suppliers/:id":                 {reason: "supplier master data; no balances", probes: []probe{{path: "/api/v1/suppliers/" + cidS, visible: "供应商甲"}}},
	"GET /api/v1/projects":                      {reason: "project list; contract amount is revenue, no cost figures", probes: []probe{{path: "/api/v1/projects", visible: "示例工程"}}},
	"GET /api/v1/projects/:id":                  {reason: "project detail; contract amount is revenue, no cost figures", probes: []probe{{path: "/api/v1/projects/" + cidS, visible: "示例工程"}}},
	"GET /api/v1/products":                      {reason: "handler drops purchase_price and last_purchase_price without cost.view", probes: []probe{{path: "/api/v1/products", visible: "示例商品", secrets: []string{secretPurchasePx}}}},
	"GET /api/v1/products/:id":                  {reason: "handler drops purchase_price and last_purchase_price without cost.view", probes: []probe{{path: "/api/v1/products/" + pidS, visible: "示例商品", secrets: []string{secretPurchasePx}}}},
	"GET /api/v1/stock/snapshots":               {reason: "stock handler drops inventory value and unit cost without cost.view", probes: []probe{{path: "/api/v1/stock/snapshots", visible: "on_hand_qty", secrets: []string{secretUnitCost}}}},
	"GET /api/v1/stock/movements":               {reason: "stock handler drops unit and total cost without cost.view", probes: []probe{{path: "/api/v1/stock/movements", visible: "qty_base", secrets: []string{secretUnitCost, secretPurchaseAmt}}}},
	"GET /api/v1/stock/alerts/low-stock":        {reason: "quantities only; the row has no cost field", probes: []probe{{path: "/api/v1/stock/alerts/low-stock", visible: "示例商品"}}},
	"GET /api/v1/stock/snapshots/:product_id/:warehouse_id": {reason: "stock handler drops inventory value and unit cost without cost.view",
		probes: []probe{{path: "/api/v1/stock/snapshots/" + pidS + "/" + widS, visible: "on_hand_qty", secrets: []string{secretUnitCost}}}},
	"GET /api/v1/last-prices": {reason: "handler refuses the purchase price type without cost.view",
		probes: []probe{
			{path: "/api/v1/last-prices?partner_id=" + cidS + "&product_ids=" + pidS, visible: "18.00"},
			{path: "/api/v1/last-prices?type=purchase&partner_id=" + cidS + "&product_ids=" + pidS, secrets: []string{secretPurchasePx}, hiddenStatus: http.StatusForbidden},
		}},
	"GET /api/v1/sale-bills": {reason: "sale bills carry sale prices and the collection state (paid, unpaid, payments) that BillRead roles need to settle a sale (collection ruling of 2026-10-12: not cost); purchase bills are filtered out",
		probes: []probe{{path: "/api/v1/sale-bills", visible: "SL-20261011-0001", payOK: true, never: []string{secretPurchaseNo, secretPurchaseAmt}}}},
	"GET /api/v1/sale-bills/:id": {reason: "sale bill detail with its collection state, as above (collection ruling of 2026-10-12); a purchase bill id is refused",
		probes: []probe{
			{path: "/api/v1/sale-bills/" + saleS, visible: "SL-20261011-0001", payOK: true},
			{path: "/api/v1/sale-bills/" + purS, hiddenStatus: http.StatusNotFound, ownerStatus: http.StatusNotFound},
		}},
	"GET /api/v1/sale-bills/pos-summary": {reason: "till takings, shown on the home page to whoever can see sale bills (collection ruling of 2026-10-12: not cost); a role that cannot take returns only gets its own shift", probes: []probe{{path: "/api/v1/sale-bills/pos-summary", visible: "total_amount", payOK: true, secrets: []string{secretShopTakings, secretCashierName}, hiddenKeys: []string{"cashiers"}}, {path: "/api/v1/sale-bills/pos-summary?cashier=" + probeCashierID.String(), visible: "total_amount", payOK: true, hiddenKeys: []string{"cashiers"}}}},
	"GET /api/v1/search": {reason: "search repository drops purchase bills when cost is hidden",
		probes: []probe{{path: "/api/v1/search?q=SL", visible: "SL-20261011-0001", secrets: []string{secretPurchaseNo}, search: true}}},
	"GET /api/v1/weekly-summary": {reason: "TrimCostResponse strips the estimated purchase amount without cost.view", probes: []probe{{path: "/api/v1/weekly-summary", visible: "replenish", secrets: []string{secretReplenishAmt}}}},
	"GET /api/v1/todo/summary":   {reason: "TrimTodoSummary drops purchase drafts and receivable figures per role", probes: []probe{{path: "/api/v1/todo/summary", visible: "SL-1", secrets: []string{secretPurchaseNo, secretPurchaseAmt, secretOverdueAmt, secretUnpaidAmt, secretCollectedAmt, secretUninvoiced, secretReceivable}}}},
}

// All GET routes the router registers, with every optional module mounted.
func registeredGets(env *probeEnv) map[string]bool {
	out := map[string]bool{}
	for _, rt := range env.engine.Routes() {
		if rt.Method == http.MethodGet {
			out[rt.Method+" "+rt.Path] = true
		}
	}
	return out
}

func TestOpenGets_EveryUnpermittedGetIsListedWithReason(t *testing.T) {
	var unlisted []string
	seen := map[string]bool{}
	for key := range registeredGets(newProbeEnv()) {
		parts := strings.SplitN(key, " ", 2)
		if _, ok := permission.ForRoute(parts[0], parts[1]); ok {
			continue
		}
		seen[key] = true
		if g, ok := openGets[key]; !ok || strings.TrimSpace(g.reason) == "" {
			unlisted = append(unlisted, key)
		}
	}
	sort.Strings(unlisted)
	if len(unlisted) > 0 {
		t.Fatalf("open GET routes missing from openGets (gate them in RoutePerm or list them with a reason):\n%s", strings.Join(unlisted, "\n"))
	}
	for key, g := range openGets {
		if !seen[key] {
			t.Errorf("openGets lists %q but no such open GET route is registered", key)
		}
		if len(g.probes) == 0 {
			t.Errorf("%s: an open GET route needs at least one probe", key)
		}
		for _, p := range g.probes {
			if p.hiddenStatus == 0 && p.visible == "" {
				t.Errorf("%s probe %s: a 200 probe needs a visible marker", key, p.path)
			}
		}
	}
}

// Every optional module must be mounted for the enumeration above to be complete:
// these routes only exist behind a With* option, the case that once escaped it.
func TestOpenGets_EnumerationCoversOptionalModules(t *testing.T) {
	gets := registeredGets(newProbeEnv())
	for _, key := range []string{
		"GET /api/v1/tenant/print-settings",
		"GET /api/v1/tenant/cashier-mode",
		"GET /api/v1/todo/summary",
		"GET /api/v1/last-prices",
		"GET /api/v1/receivables",
		"GET /api/v1/partners/:id/statement",
		"GET /api/v1/receipts/preview",
	} {
		if !gets[key] {
			t.Errorf("%s is not registered by the probe router; mount its module in newProbeEnv", key)
		}
	}
	for _, key := range []string{"GET /api/v1/team/members", "GET /api/v1/team/invites"} {
		if !gets[key] {
			t.Errorf("%s is not registered by the probe router; mount its module in newProbeEnv", key)
		}
	}
}

func (p probe) do(env *probeEnv, role team.Role) (int, string) {
	if p.search {
		env.expectSearch(permission.Allowed(role, permission.CostView))
	}
	req := httptest.NewRequest(http.MethodGet, p.path, nil)
	req.Header.Set("X-Test-Role", string(role))
	w := httptest.NewRecorder()
	env.engine.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

func TestOpenGets_NoCostOrMoneyLeaksToClerkAndCashier(t *testing.T) {
	paymentKeys := []string{"overdue_amount", "unpaid_amount", "today_collected", "receivable_unpaid", "month_uninvoiced", "receivable_amount", "payments",
		"paid_amount", "refund_amount", "credit_amount", "by_pay_type", "receipts", "receivable"}
	allSecrets := []string{secretPurchaseNo, secretPurchaseAmt, secretUnitCost, secretPurchasePx, secretReplenishAmt, secretOverdueAmt,
		secretUnpaidAmt, secretCollectedAmt, secretUninvoiced, secretReceivable, secretShopTakings, secretPinHash, secretCashierName}
	probed := 0
	for _, role := range []team.Role{team.RoleClerk, team.RoleCashier} {
		for key, g := range openGets {
			for _, p := range g.probes {
				probed++
				env := newProbeEnv()
				code, body := p.do(env, role)
				want := http.StatusOK
				if p.hiddenStatus != 0 {
					want = p.hiddenStatus
				}
				if code != want {
					t.Errorf("%s %s as %s: status %d, want %d: %s", key, p.path, role, code, want, body)
					continue
				}
				if want == http.StatusOK && !strings.Contains(body, p.visible) {
					t.Errorf("%s %s as %s: marker %q missing, the probe is vacuous: %s", key, p.path, role, p.visible, body)
				}
				for k := range permission.CostKeys {
					if strings.Contains(body, `"`+k+`"`) {
						t.Errorf("%s %s as %s: body carries cost key %q: %s", key, p.path, role, k, body)
					}
				}
				if !p.payOK {
					for _, k := range paymentKeys {
						if strings.Contains(body, `"`+k+`"`) {
							t.Errorf("%s %s as %s: body carries payment key %q: %s", key, p.path, role, k, body)
						}
					}
				}
				for _, k := range p.hiddenKeys {
					if strings.Contains(body, `"`+k+`"`) {
						t.Errorf("%s %s as %s: body carries owner-only key %q: %s", key, p.path, role, k, body)
					}
				}
				for _, s := range append(allSecrets, p.never...) {
					if strings.Contains(body, s) {
						t.Errorf("%s %s as %s: body carries sentinel %q: %s", key, p.path, role, s, body)
					}
				}
			}
		}
	}
	if probed < 2*len(openGets) {
		t.Fatalf("only %d probes ran; the table lost routes", probed)
	}
}

// The boss still sees every sentinel, so the probe above is not passing merely
// because a stub returns nothing.
func TestOpenGets_OwnerStillSeesTheMoney(t *testing.T) {
	for key, g := range openGets {
		for _, p := range g.probes {
			env := newProbeEnv()
			code, body := p.do(env, team.RoleOwner)
			want := http.StatusOK
			if p.ownerStatus != 0 {
				want = p.ownerStatus
			}
			if code != want {
				t.Errorf("%s %s as owner: status %d, want %d: %s", key, p.path, code, want, body)
				continue
			}
			if want != http.StatusOK {
				continue
			}
			if !strings.Contains(body, p.visible) {
				t.Errorf("%s %s as owner: marker %q missing: %s", key, p.path, p.visible, body)
			}
			for _, s := range p.never {
				if strings.Contains(body, s) {
					t.Errorf("%s %s as owner: body carries rows the route must filter out (%q): %s", key, p.path, s, body)
				}
			}
			for _, s := range p.secrets {
				if !strings.Contains(body, s) {
					t.Errorf("%s %s as owner: stub secret %q missing, the probe is vacuous: %s", key, p.path, s, body)
				}
			}
		}
	}
}
