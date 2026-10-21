package bill_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// asciiWord matches an English word of two or more letters; shop owners must never see one.
var asciiWord = regexp.MustCompile(`[A-Za-z]{2,}`)

type zhCase struct {
	name     string
	method   string
	path     string
	body     string
	noTenant bool
}

func runZhCases(t *testing.T, router http.Handler, cases []zhCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			if !tc.noTenant {
				req.Header.Set("X-Tenant-ID", tenantHeader(shTenantID))
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code < 400 || w.Code >= 500 {
				t.Fatalf("status = %d, want a 4xx; body: %s", w.Code, w.Body.String())
			}
			var resp struct {
				Message string `json:"message"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || resp.Message == "" {
				t.Fatalf("no message in body: %s", w.Body.String())
			}
			if word := asciiWord.FindString(resp.Message); word != "" {
				t.Errorf("message %q contains English word %q", resp.Message, word)
			}
		})
	}
}

func TestPurchaseHandler_ErrorMessagesAreChinese(t *testing.T) {
	f := newPOHTTPFixture(true)
	id := uuid.New().String()
	p := shProductID.String()
	one := `"items":[{"product_id":"` + p + `","qty":"1"}]`
	const base = "/api/v1/purchase-bills"
	cases := []zhCase{
		{name: "create no tenant", method: "POST", path: base, body: `{}`, noTenant: true},
		{name: "create bad json", method: "POST", path: base, body: `{`},
		{name: "create wrong type", method: "POST", path: base, body: `{"items":"x"}`},
		{name: "create long remark", method: "POST", path: base, body: `{"remark":"` + strings.Repeat("x", 501) + `",` + one + `}`},
		{name: "create no items", method: "POST", path: base, body: `{"items":[]}`},
		{name: "create bad product", method: "POST", path: base, body: `{"items":[{"product_id":"x","qty":"1"}]}`},
		{name: "create bad qty", method: "POST", path: base, body: `{"items":[{"product_id":"` + p + `","qty":"abc"}]}`},
		{name: "create zero qty", method: "POST", path: base, body: `{"items":[{"product_id":"` + p + `","qty":"0"}]}`},
		{name: "create bad price", method: "POST", path: base, body: `{"items":[{"product_id":"` + p + `","qty":"1","unit_price":"-1"}]}`},
		{name: "create bad unit", method: "POST", path: base, body: `{"items":[{"product_id":"` + p + `","qty":"1","unit_id":"x"}]}`},
		{name: "create bad partner", method: "POST", path: base, body: `{"partner_id":"x",` + one + `}`},
		{name: "create bad warehouse", method: "POST", path: base, body: `{"warehouse_id":"x",` + one + `}`},
		{name: "create bad date", method: "POST", path: base, body: `{"bill_date":"yesterday",` + one + `}`},
		{name: "create bad shipping", method: "POST", path: base, body: `{"shipping_fee":"x",` + one + `}`},
		{name: "create bad tax", method: "POST", path: base, body: `{"tax_amount":"x",` + one + `}`},
		{name: "create bad currency", method: "POST", path: base, body: `{"currency":"us",` + one + `}`},
		{name: "create bad rate", method: "POST", path: base, body: `{"currency":"USD","exchange_rate":"x",` + one + `}`},
		{name: "create rate missing", method: "POST", path: base, body: `{"currency":"USD",` + one + `}`},
		{name: "create rate too big", method: "POST", path: base, body: `{"currency":"USD","exchange_rate":"99999999",` + one + `}`},
		{name: "update no tenant", method: "PUT", path: base + "/" + id, body: `{}`, noTenant: true},
		{name: "update bad id", method: "PUT", path: base + "/x", body: `{}`},
		{name: "update bad json", method: "PUT", path: base + "/" + id, body: `{`},
		{name: "update bad qty", method: "PUT", path: base + "/" + id, body: `{"items":[{"product_id":"` + p + `","qty":"x"}]}`},
		{name: "update empty items", method: "PUT", path: base + "/" + id, body: `{"items":[]}`},
		{name: "update not found", method: "PUT", path: base + "/" + id, body: `{` + one + `}`},
		{name: "update approved", method: "PUT", path: base + "/" + f.poID.String(), body: `{` + one + `}`},
		{name: "approve no tenant", method: "POST", path: base + "/" + id + "/approve", noTenant: true},
		{name: "approve bad id", method: "POST", path: base + "/x/approve"},
		{name: "approve not found", method: "POST", path: base + "/" + id + "/approve", body: `{}`},
		{name: "approve bad paid", method: "POST", path: base + "/" + id + "/approve", body: `{"paid_amount":"x"}`},
		{name: "cancel no tenant", method: "POST", path: base + "/" + id + "/cancel", noTenant: true},
		{name: "cancel bad id", method: "POST", path: base + "/x/cancel"},
		{name: "cancel not found", method: "POST", path: base + "/" + id + "/cancel"},
		{name: "cancel approved", method: "POST", path: base + "/" + f.poID.String() + "/cancel"},
		{name: "restore no tenant", method: "POST", path: base + "/" + id + "/restore", noTenant: true},
		{name: "restore bad id", method: "POST", path: base + "/x/restore"},
		{name: "restore not found", method: "POST", path: base + "/" + id + "/restore"},
		{name: "restore approved", method: "POST", path: base + "/" + f.poID.String() + "/restore"},
		{name: "get no tenant", method: "GET", path: base + "/" + id, noTenant: true},
		{name: "get bad id", method: "GET", path: base + "/x"},
		{name: "get not found", method: "GET", path: base + "/" + id},
		{name: "list no tenant", method: "GET", path: base, noTenant: true},
		{name: "list bad status", method: "GET", path: base + "?status=zzz"},
		{name: "return no tenant", method: "POST", path: base + "/" + id + "/return", body: `{}`, noTenant: true},
		{name: "return bad id", method: "POST", path: base + "/x/return", body: `{}`},
		{name: "return bad json", method: "POST", path: base + "/" + id + "/return", body: `{`},
		{name: "return empty", method: "POST", path: base + "/" + id + "/return", body: `{"items":[]}`},
		{name: "return not found", method: "POST", path: base + "/" + id + "/return", body: `{"items":[{"bill_item_id":"` + id + `","qty":"1"}]}`},
	}
	runZhCases(t, f.router, cases)
}

func TestSaleHandler_ErrorMessagesAreChinese(t *testing.T) {
	f := newReturnHTTPFixture()
	id := uuid.New().String()
	p := shProductID.String()
	one := `"items":[{"product_id":"` + p + `","qty":"1"}]`
	const base = "/api/v1/sale-bills"
	cases := []zhCase{
		{name: "quick no tenant", method: "POST", path: base + "/quick-checkout", body: `{}`, noTenant: true},
		{name: "quick bad json", method: "POST", path: base + "/quick-checkout", body: `{`},
		{name: "quick no items", method: "POST", path: base + "/quick-checkout", body: `{"items":[]}`},
		{name: "quick long remark", method: "POST", path: base + "/quick-checkout", body: `{"remark":"` + strings.Repeat("x", 201) + `",` + one + `}`},
		{name: "quick bad paid", method: "POST", path: base + "/quick-checkout", body: `{"paid_amount":"x",` + one + `}`},
		{name: "quick bad discount", method: "POST", path: base + "/quick-checkout", body: `{"discount_amount":"-1",` + one + `}`},
		{name: "quick bad partner", method: "POST", path: base + "/quick-checkout", body: `{"partner_id":"x",` + one + `}`},
		{name: "quick bad qty", method: "POST", path: base + "/quick-checkout", body: `{"items":[{"product_id":"` + p + `","qty":"x"}]}`},
		{name: "quick bad method", method: "POST", path: base + "/quick-checkout", body: `{"payment_method":"bitcoin",` + one + `}`},
		{name: "create no tenant", method: "POST", path: base, body: `{}`, noTenant: true},
		{name: "create bad json", method: "POST", path: base, body: `{`},
		{name: "create no items", method: "POST", path: base, body: `{"items":[]}`},
		{name: "create long remark", method: "POST", path: base, body: `{"remark":"` + strings.Repeat("x", 501) + `",` + one + `}`},
		{name: "create bad qty", method: "POST", path: base, body: `{"items":[{"product_id":"` + p + `","qty":"x"}]}`},
		{name: "create bad partner", method: "POST", path: base, body: `{"partner_id":"x",` + one + `}`},
		{name: "create bad shipping", method: "POST", path: base, body: `{"shipping_fee":"x",` + one + `}`},
		{name: "update no tenant", method: "PUT", path: base + "/" + id, body: `{}`, noTenant: true},
		{name: "update bad id", method: "PUT", path: base + "/x", body: `{}`},
		{name: "update bad json", method: "PUT", path: base + "/" + id, body: `{`},
		{name: "update bad qty", method: "PUT", path: base + "/" + id, body: `{"items":[{"product_id":"` + p + `","qty":"x"}]}`},
		{name: "update not found", method: "PUT", path: base + "/" + id, body: `{` + one + `}`},
		{name: "approve no tenant", method: "POST", path: base + "/" + id + "/approve", noTenant: true},
		{name: "approve bad id", method: "POST", path: base + "/x/approve"},
		{name: "approve bad body", method: "POST", path: base + "/" + id + "/approve", body: `{`},
		{name: "approve bad paid", method: "POST", path: base + "/" + id + "/approve", body: `{"paid_amount":"x"}`},
		{name: "approve not found", method: "POST", path: base + "/" + id + "/approve", body: `{}`},
		{name: "cancel no tenant", method: "POST", path: base + "/" + id + "/cancel", noTenant: true},
		{name: "cancel bad id", method: "POST", path: base + "/x/cancel"},
		{name: "cancel not found", method: "POST", path: base + "/" + id + "/cancel"},
		{name: "cancel approved", method: "POST", path: base + "/" + f.saleID.String() + "/cancel"},
		{name: "get no tenant", method: "GET", path: base + "/" + id, noTenant: true},
		{name: "get bad id", method: "GET", path: base + "/x"},
		{name: "get not found", method: "GET", path: base + "/" + id},
		{name: "list no tenant", method: "GET", path: base, noTenant: true},
		{name: "list bad status", method: "GET", path: base + "?status=zzz"},
		{name: "summary no tenant", method: "GET", path: base + "/pos-summary", noTenant: true},
		{name: "return no tenant", method: "POST", path: base + "/" + id + "/return", body: `{}`, noTenant: true},
		{name: "return bad id", method: "POST", path: base + "/x/return", body: `{}`},
		{name: "return bad json", method: "POST", path: base + "/" + id + "/return", body: `{`},
		{name: "return empty", method: "POST", path: base + "/" + id + "/return", body: `{"items":[]}`},
		{name: "return bad refund", method: "POST", path: base + "/" + f.saleID.String() + "/return", body: `{"items":[{"bill_item_id":"` + f.lineID.String() + `","qty":"1"}],"refund_amount":"x"}`},
	}
	runZhCases(t, f.router, cases)
}
