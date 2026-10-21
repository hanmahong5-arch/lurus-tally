package bill_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	handlerbill "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/bill"
	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	appsku "github.com/hanmahong5-arch/lurus-tally/internal/app/sku"
)

type stubDefaults struct{ skus []appsku.DefaultSKU }

func (s stubDefaults) ListDefaultSKUs(_ context.Context, _ uuid.UUID, ids []uuid.UUID) ([]appsku.DefaultSKU, error) {
	var out []appsku.DefaultSKU
	for _, k := range s.skus {
		for _, id := range ids {
			if k.ProductID == id {
				out = append(out, k)
			}
		}
	}
	return out, nil
}

type lastPriceItem struct {
	ProductID     string `json:"product_id"`
	UnitPrice     string `json:"unit_price"`
	UnitID        string `json:"unit_id"`
	UnitName      string `json:"unit_name"`
	BaseUnitPrice string `json:"base_unit_price"`
	Factor        string `json:"factor"`
	Source        string `json:"source"`
	BillDate      string `json:"bill_date"`
}

// A partner with no history gets the product's default price; a zero default is left out.
func TestLastPricesHandler_DefaultPriceFallback(t *testing.T) {
	hist, none, zero := uuid.New(), uuid.New(), uuid.New()
	repo := &stubLastPriceRepo{rows: []appbill.LastPrice{{
		ProductID: hist, UnitPrice: decimal.RequireFromString("9.8"), UnitName: "株", BillDate: time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC),
	}}}
	defs := stubDefaults{skus: []appsku.DefaultSKU{
		{ProductID: hist, RetailPrice: decimal.NewFromInt(99), PurchasePrice: decimal.NewFromInt(99)},
		{ProductID: none, RetailPrice: decimal.RequireFromString("12.5"), PurchasePrice: decimal.NewFromInt(3), UnitName: "件"},
		{ProductID: zero, RetailPrice: decimal.Zero, PurchasePrice: decimal.Zero},
	}}
	h := newTestHandler(&mockBillRepo{}).WithLastPrices(appbill.NewLastPricesUseCase(repo).WithDefaultPrices(defs))
	r := lastPricesRouter(h)
	ids := hist.String() + "," + none.String() + "," + zero.String()
	for typ, want := range map[string]string{"sale": "12.50", "purchase": "3.00"} {
		w := getLastPrices(r, "?type="+typ+"&partner_id="+uuid.NewString()+"&product_ids="+ids)
		var body struct {
			Items []lastPriceItem `json:"items"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if len(body.Items) != 2 {
			t.Fatalf("%s items = %+v", typ, body.Items)
		}
		got := map[string]lastPriceItem{}
		for _, it := range body.Items {
			got[it.ProductID] = it
		}
		if got[hist.String()].Source != "last" || got[hist.String()].UnitPrice != "9.80" {
			t.Errorf("%s history = %+v", typ, got[hist.String()])
		}
		d := got[none.String()]
		if d.Source != "default" || d.UnitPrice != want || d.BillDate != "" || d.UnitName != "件" {
			t.Errorf("%s default = %+v want %s", typ, d, want)
		}
	}
}

// The last line was billed per carton (x12): the response carries the carton price, the
// unit and the per-base-unit price so a client never mixes the two.
func TestLastPricesHandler_CrossUnit(t *testing.T) {
	pid, carton := uuid.New(), uuid.New()
	repo := &stubLastPriceRepo{rows: []appbill.LastPrice{{
		ProductID: pid, UnitPrice: decimal.RequireFromString("42"), UnitID: carton, UnitName: "箱",
		Factor: decimal.NewFromInt(12), BillDate: time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC),
	}}}
	h := newTestHandler(&mockBillRepo{}).WithLastPrices(appbill.NewLastPricesUseCase(repo))
	w := getLastPrices(lastPricesRouter(h), "?type=purchase&partner_id="+uuid.NewString()+"&product_ids="+pid.String())
	var body struct {
		Items []lastPriceItem `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || len(body.Items) != 1 {
		t.Fatalf("body %s err %v", w.Body.String(), err)
	}
	it := body.Items[0]
	if it.UnitPrice != "42.00" || it.UnitName != "箱" || it.UnitID != carton.String() || it.Factor != "12" || it.BaseUnitPrice != "3.50" {
		t.Fatalf("item = %+v", it)
	}
}

type stubLastPriceRepo struct {
	rows      []appbill.LastPrice
	got       appbill.LastPriceQuery
	called    bool
	baseUnits map[uuid.UUID]string
}

func (s *stubLastPriceRepo) BaseUnitNames(_ context.Context, _ uuid.UUID, _ []uuid.UUID) (map[uuid.UUID]string, error) {
	return s.baseUnits, nil
}

func (s *stubLastPriceRepo) LastPrices(_ context.Context, q appbill.LastPriceQuery) ([]appbill.LastPrice, error) {
	s.called = true
	s.got = q
	return s.rows, nil
}

func lastPricesRouter(h *handlerbill.Handler) *gin.Engine {
	r := gin.New()
	r.Use(injectTenantFromHeader)
	h.RegisterRoutes(r.Group("/api/v1"))
	return r
}

func getLastPrices(r *gin.Engine, query string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/last-prices"+query, nil)
	req.Header.Set("X-Tenant-ID", devTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestLastPricesHandler_OK(t *testing.T) {
	pid, partner := uuid.New(), uuid.New()
	d := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	repo := &stubLastPriceRepo{rows: []appbill.LastPrice{{
		ProductID: pid, UnitPrice: decimal.RequireFromString("9.8"), UnitName: "株", BillDate: d,
	}}}
	h := newTestHandler(&mockBillRepo{}).WithLastPrices(appbill.NewLastPricesUseCase(repo))
	w := getLastPrices(lastPricesRouter(h), "?type=sale&partner_id="+partner.String()+"&product_ids="+pid.String()+","+pid.String())
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	if repo.got.SubType != "销售" || repo.got.PartnerID != partner || len(repo.got.ProductIDs) != 1 {
		t.Fatalf("query = %+v", repo.got)
	}
	var body struct {
		Items []struct {
			ProductID string `json:"product_id"`
			UnitPrice string `json:"unit_price"`
			UnitName  string `json:"unit_name"`
			BillDate  string `json:"bill_date"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 1 || body.Items[0].ProductID != pid.String() || body.Items[0].UnitPrice != "9.80" ||
		body.Items[0].UnitName != "株" || body.Items[0].BillDate != "2026-09-28" {
		t.Fatalf("items = %+v", body.Items)
	}
}

func TestLastPricesHandler_PurchaseType(t *testing.T) {
	repo := &stubLastPriceRepo{}
	h := newTestHandler(&mockBillRepo{}).WithLastPrices(appbill.NewLastPricesUseCase(repo))
	w := getLastPrices(lastPricesRouter(h), "?type=purchase&partner_id="+uuid.NewString()+"&product_ids="+uuid.NewString())
	if w.Code != http.StatusOK || repo.got.SubType != "采购" {
		t.Fatalf("status %d subtype %q", w.Code, repo.got.SubType)
	}
}

func TestLastPricesHandler_NoPartnerSkipsQuery(t *testing.T) {
	repo := &stubLastPriceRepo{}
	h := newTestHandler(&mockBillRepo{}).WithLastPrices(appbill.NewLastPricesUseCase(repo))
	w := getLastPrices(lastPricesRouter(h), "?type=sale&product_ids="+uuid.NewString())
	if w.Code != http.StatusOK || repo.called {
		t.Fatalf("status %d called %v", w.Code, repo.called)
	}
	if w.Body.String() != `{"items":[]}` {
		t.Fatalf("body %s", w.Body.String())
	}
}

// A walk-in sale has no customer: the default price still comes back, never a history row.
func TestLastPricesHandler_NoPartnerGetsDefaultPrice(t *testing.T) {
	pid := uuid.New()
	repo := &stubLastPriceRepo{}
	defs := stubDefaults{skus: []appsku.DefaultSKU{{ProductID: pid, RetailPrice: decimal.RequireFromString("12.5"), UnitName: "件"}}}
	h := newTestHandler(&mockBillRepo{}).WithLastPrices(appbill.NewLastPricesUseCase(repo).WithDefaultPrices(defs))
	w := getLastPrices(lastPricesRouter(h), "?type=sale&product_ids="+pid.String())
	var body struct {
		Items []lastPriceItem `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || len(body.Items) != 1 {
		t.Fatalf("body %s err %v", w.Body.String(), err)
	}
	if repo.called || body.Items[0].Source != "default" || body.Items[0].UnitPrice != "12.50" {
		t.Fatalf("called %v item %+v", repo.called, body.Items[0])
	}
}

// A default SKU with no unit name falls back to the product's base unit name.
func TestLastPricesHandler_DefaultUnitNameFallsBackToBaseUnit(t *testing.T) {
	pid, named := uuid.New(), uuid.New()
	repo := &stubLastPriceRepo{baseUnits: map[uuid.UUID]string{pid: "件", named: "盒"}}
	defs := stubDefaults{skus: []appsku.DefaultSKU{
		{ProductID: pid, RetailPrice: decimal.RequireFromString("12.5")},
		{ProductID: named, RetailPrice: decimal.RequireFromString("3"), UnitName: "袋"},
	}}
	h := newTestHandler(&mockBillRepo{}).WithLastPrices(appbill.NewLastPricesUseCase(repo).WithDefaultPrices(defs))
	w := getLastPrices(lastPricesRouter(h), "?type=sale&partner_id="+uuid.NewString()+"&product_ids="+pid.String()+","+named.String())
	var body struct {
		Items []lastPriceItem `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || len(body.Items) != 2 {
		t.Fatalf("body %s err %v", w.Body.String(), err)
	}
	got := map[string]string{}
	for _, it := range body.Items {
		got[it.ProductID] = it.UnitName
	}
	if got[pid.String()] != "件" || got[named.String()] != "袋" {
		t.Fatalf("unit names = %v", got)
	}
}

func TestLastPricesHandler_Validation(t *testing.T) {
	repo := &stubLastPriceRepo{}
	h := newTestHandler(&mockBillRepo{}).WithLastPrices(appbill.NewLastPricesUseCase(repo))
	r := lastPricesRouter(h)
	p := uuid.NewString()
	for _, q := range []string{
		"?type=other&partner_id=" + p + "&product_ids=" + p,
		"?type=sale&partner_id=bad&product_ids=" + p,
		"?type=sale&partner_id=" + p + "&product_ids=bad",
	} {
		if w := getLastPrices(r, q); w.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d", q, w.Code)
		}
	}
}

func TestLastPricesHandler_NotConfigured(t *testing.T) {
	h := newTestHandler(&mockBillRepo{})
	if w := getLastPrices(lastPricesRouter(h), "?type=sale"); w.Code != http.StatusNotImplemented {
		t.Fatalf("status %d", w.Code)
	}
}

func TestLastPricesHandler_TooManyProducts(t *testing.T) {
	repo := &stubLastPriceRepo{}
	h := newTestHandler(&mockBillRepo{}).WithLastPrices(appbill.NewLastPricesUseCase(repo))
	ids := make([]string, 201)
	for i := range ids {
		ids[i] = uuid.NewString()
	}
	q := "?type=sale&partner_id=" + uuid.NewString() + "&product_ids=" + strings.Join(ids, ",")
	if w := getLastPrices(lastPricesRouter(h), q); w.Code != http.StatusBadRequest {
		t.Fatalf("status %d", w.Code)
	}
}
