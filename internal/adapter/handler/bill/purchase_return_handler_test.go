package bill_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	handlerbill "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/bill"
	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

// poHTTPRepo adds the link and returned-quantity capabilities the production repo has for
// purchase returns.
type poHTTPRepo struct {
	*shMockBillRepo
	links     map[uuid.UUID]uuid.UUID
	itemLinks map[uuid.UUID]uuid.UUID
}

func (r *poHTTPRepo) SetLinkBill(_ context.Context, _ *sql.Tx, _, billID, linked uuid.UUID) error {
	r.links[billID] = linked
	return nil
}

func (r *poHTTPRepo) SetLinkItems(_ context.Context, _ *sql.Tx, _, _ uuid.UUID, links map[uuid.UUID]uuid.UUID) error {
	for k, v := range links {
		r.itemLinks[k] = v
	}
	return nil
}

func (r *poHTTPRepo) PurchaseReturnedQtyByLine(_ context.Context, _, poID uuid.UUID) (map[uuid.UUID]decimal.Decimal, error) {
	out := map[uuid.UUID]decimal.Decimal{}
	for id, h := range r.heads {
		if h.SubType != domain.BillSubTypePurchaseReturn || h.Status != domain.StatusApproved || r.links[id] != poID {
			continue
		}
		for _, it := range r.items[id] {
			if l, ok := r.itemLinks[it.ID]; ok {
				out[l] = out[l].Add(it.Qty)
			}
		}
	}
	return out, nil
}

type poHTTPFixture struct {
	repo    *poHTTPRepo
	stockUC *shMockStockUC
	router  http.Handler
	poID    uuid.UUID
	lineID  uuid.UUID
}

// newPOHTTPFixture seeds an approved purchase of 10 units at 8.00 (total 80, nothing paid).
func newPOHTTPFixture(withReturn bool) *poHTTPFixture {
	repo := &poHTTPRepo{shMockBillRepo: newSHMockBillRepo(), links: map[uuid.UUID]uuid.UUID{}, itemLinks: map[uuid.UUID]uuid.UUID{}}
	stockUC := &shMockStockUC{}
	poID, lineID, wh := uuid.New(), uuid.New(), shWarehouseID
	repo.heads[poID] = &domain.BillHead{
		ID: poID, TenantID: shTenantID, BillNo: "PO-20261010-0001",
		BillType: domain.BillTypePurchase, SubType: domain.BillSubTypePurchase, Status: domain.StatusApproved,
		WarehouseID: &wh, TotalAmount: decimal.NewFromInt(80),
	}
	repo.items[poID] = []*domain.BillItem{{
		ID: lineID, TenantID: shTenantID, HeadID: poID, ProductID: shProductID, LineNo: 1,
		Qty: decimal.NewFromInt(10), UnitPrice: decimal.NewFromInt(8), LineAmount: decimal.NewFromInt(80),
	}}
	h := handlerbill.New(
		appbill.NewCreatePurchaseDraftUseCase(repo),
		appbill.NewUpdatePurchaseDraftUseCase(repo),
		appbill.NewApprovePurchaseUseCase(repo, stockUC, &shMockUnitRepo{}),
		appbill.NewCancelPurchaseUseCase(repo),
		appbill.NewListPurchasesUseCase(repo),
		appbill.NewGetPurchaseUseCase(repo),
		appbill.NewRestorePurchaseUseCase(repo),
	)
	if withReturn {
		h.WithPurchaseReturn(appbill.NewPurchaseReturnUseCase(repo, stockUC).WithPayments(&shMockPaymentRecorder{}))
	}
	return &poHTTPFixture{repo: repo, stockUC: stockUC, router: newRouter(h), poID: poID, lineID: lineID}
}

func (f *poHTTPFixture) do(method, path string, body map[string]any) *httptest.ResponseRecorder {
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(method, path, rdr)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", tenantHeader(shTenantID))
	f.router.ServeHTTP(w, req)
	return w
}

func (f *poHTTPFixture) ret(id uuid.UUID, line uuid.UUID, qty string) *httptest.ResponseRecorder {
	return f.do(http.MethodPost, "/api/v1/purchase-bills/"+id.String()+"/return", map[string]any{
		"items": []map[string]any{{"bill_item_id": line.String(), "qty": qty}},
	})
}

// 采购 10 件退 3 件：201、库存出一次、详情显示该行已退 3；再退 8 件 422 并提示最多还能退 7。
func TestPurchaseReturn_PartialThenOverReturn(t *testing.T) {
	f := newPOHTTPFixture(true)

	w := f.ret(f.poID, f.lineID, "3")
	if w.Code != http.StatusCreated {
		t.Fatalf("return 3: status = %d, body %s", w.Code, w.Body.String())
	}
	body := decode(t, w)
	if body["return_amount"] != "24" || body["payable_reduced"] != "24" || body["supplier_owes"] != "0" {
		t.Errorf("body = %v, want return_amount 24, payable_reduced 24, supplier_owes 0", body)
	}
	if f.stockUC.calls != 1 {
		t.Errorf("stock movements = %d, want 1", f.stockUC.calls)
	}

	gw := f.do(http.MethodGet, "/api/v1/purchase-bills/"+f.poID.String(), nil)
	if gw.Code != http.StatusOK {
		t.Fatalf("get: status = %d, body %s", gw.Code, gw.Body.String())
	}
	items := decode(t, gw)["items"].([]any)
	if got := items[0].(map[string]any)["returned_qty"]; got != "3" {
		t.Errorf("returned_qty = %v, want 3", got)
	}

	w = f.ret(f.poID, f.lineID, "8")
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("return 8: status = %d, want 422; body %s", w.Code, w.Body.String())
	}
	if b := decode(t, w); b["error"] != "return_exceeds_bought" || !strings.Contains(b["message"].(string), "最多还能退 7") {
		t.Errorf("body = %v, want return_exceeds_bought naming the 7 left", b)
	}
	if f.stockUC.calls != 1 {
		t.Errorf("stock movements = %d after a rejected return, want 1", f.stockUC.calls)
	}
}

func TestPurchaseReturn_DraftPurchase_Returns422(t *testing.T) {
	f := newPOHTTPFixture(true)
	f.repo.heads[f.poID].Status = domain.StatusDraft
	w := f.ret(f.poID, f.lineID, "1")
	if w.Code != http.StatusUnprocessableEntity || decode(t, w)["error"] != "purchase_not_returnable" {
		t.Fatalf("status = %d body %s, want 422 purchase_not_returnable", w.Code, w.Body.String())
	}
}

// 货已卖出去一部分，库存不够退：422 insufficient_stock，前端按商品名提示。
func TestPurchaseReturn_NotEnoughStock_Returns422(t *testing.T) {
	f := newPOHTTPFixture(true)
	f.stockUC.failErr = &appstock.InsufficientStockError{ProductID: shProductID, Available: decimal.NewFromInt(1), Requested: decimal.NewFromInt(3)}
	w := f.ret(f.poID, f.lineID, "3")
	if w.Code != http.StatusUnprocessableEntity || decode(t, w)["error"] != "insufficient_stock" {
		t.Fatalf("status = %d body %s, want 422 insufficient_stock", w.Code, w.Body.String())
	}
}

func TestPurchaseReturn_BadInput(t *testing.T) {
	f := newPOHTTPFixture(true)
	cases := map[string]map[string]any{
		"no items":     {"items": []map[string]any{}},
		"bad item id":  {"items": []map[string]any{{"bill_item_id": "nope", "qty": "1"}}},
		"zero qty":     {"items": []map[string]any{{"bill_item_id": f.lineID.String(), "qty": "0"}}},
		"foreign line": {"items": []map[string]any{{"bill_item_id": uuid.New().String(), "qty": "1"}}},
	}
	for name, body := range cases {
		w := f.do(http.MethodPost, "/api/v1/purchase-bills/"+f.poID.String()+"/return", body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400; body %s", name, w.Code, w.Body.String())
		}
	}
	if w := f.ret(uuid.New(), f.lineID, "1"); w.Code != http.StatusNotFound {
		t.Errorf("unknown purchase: status = %d, want 404", w.Code)
	}
	if f.stockUC.calls != 0 {
		t.Errorf("stock moved on rejected input")
	}
}

func TestPurchaseReturn_NotWired_Returns501(t *testing.T) {
	f := newPOHTTPFixture(false)
	if w := f.ret(f.poID, f.lineID, "1"); w.Code != http.StatusNotImplemented {
		t.Errorf("status = %d, want 501", w.Code)
	}
}

func TestPurchaseReturn_NoTenant_Returns401(t *testing.T) {
	f := newPOHTTPFixture(true)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/purchase-bills/"+f.poID.String()+"/return", strings.NewReader("{}"))
	f.router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}
