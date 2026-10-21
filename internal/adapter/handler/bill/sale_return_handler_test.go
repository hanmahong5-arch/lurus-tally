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
	apppayment "github.com/hanmahong5-arch/lurus-tally/internal/app/payment"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

// retMockRepo adds the link and returned-quantity capabilities the production repo has.
type retMockRepo struct {
	*shMockBillRepo
	links map[uuid.UUID]uuid.UUID
	// itemLinks maps a return line id to the sale line it gives back.
	itemLinks map[uuid.UUID]uuid.UUID
	// lockErr, when set, makes the sale's advisory lock unobtainable.
	lockErr error
}

func (r *retMockRepo) AcquireBillAdvisoryLock(_ context.Context, _ *sql.Tx, _, _ uuid.UUID) error {
	return r.lockErr
}

func (r *retMockRepo) SetLinkBill(_ context.Context, _ *sql.Tx, _, billID, linked uuid.UUID) error {
	r.links[billID] = linked
	return nil
}

func (r *retMockRepo) LinkedBillID(_ context.Context, _, billID uuid.UUID) (uuid.UUID, bool, error) {
	id, ok := r.links[billID]
	return id, ok, nil
}

func (r *retMockRepo) SetLinkItems(_ context.Context, _ *sql.Tx, _, _ uuid.UUID, links map[uuid.UUID]uuid.UUID) error {
	for k, v := range links {
		r.itemLinks[k] = v
	}
	return nil
}

func (r *retMockRepo) ReturnLinks(_ context.Context, _, billID uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	out := map[uuid.UUID]uuid.UUID{}
	for _, it := range r.items[billID] {
		if l, ok := r.itemLinks[it.ID]; ok {
			out[it.ID] = l
		}
	}
	return out, nil
}

func (r *retMockRepo) ReturnedQtyByLine(_ context.Context, _, saleID uuid.UUID) (map[uuid.UUID]decimal.Decimal, error) {
	out := map[uuid.UUID]decimal.Decimal{}
	for _, id := range r.approvedReturnsOf(saleID) {
		for _, it := range r.items[id] {
			if l, ok := r.itemLinks[it.ID]; ok {
				out[l] = out[l].Add(it.Qty)
			}
		}
	}
	return out, nil
}

func (r *retMockRepo) approvedReturnsOf(saleID uuid.UUID) []uuid.UUID {
	var ids []uuid.UUID
	for id, h := range r.heads {
		if h.SubType == domain.BillSubTypeSaleReturn && h.Status == domain.StatusApproved && r.links[id] == saleID {
			ids = append(ids, id)
		}
	}
	return ids
}

func (r *retMockRepo) ReturnedQty(_ context.Context, _, saleID uuid.UUID) ([]appbill.ReturnedLine, error) {
	var out []appbill.ReturnedLine
	for _, id := range r.approvedReturnsOf(saleID) {
		for _, it := range r.items[id] {
			out = append(out, appbill.ReturnedLine{ProductID: it.ProductID, UnitID: it.UnitID, Qty: it.Qty})
		}
	}
	return out, nil
}

func (r *retMockRepo) ReturnedAmount(_ context.Context, _, saleID uuid.UUID) (decimal.Decimal, error) {
	var sum decimal.Decimal
	for _, id := range r.approvedReturnsOf(saleID) {
		sum = sum.Add(r.heads[id].TotalAmount)
	}
	return sum, nil
}

type returnHTTPFixture struct {
	repo    *retMockRepo
	stockUC *shMockStockUC
	router  http.Handler
	saleID  uuid.UUID
	lineID  uuid.UUID
}

// newReturnHTTPFixture seeds an approved sale of 5 units at 20.00 (total 100, paid 100).
func newReturnHTTPFixture() *returnHTTPFixture {
	repo := &retMockRepo{shMockBillRepo: newSHMockBillRepo(), links: map[uuid.UUID]uuid.UUID{}, itemLinks: map[uuid.UUID]uuid.UUID{}}
	stockUC := &shMockStockUC{}
	saleID, lineID := uuid.New(), uuid.New()
	wh := shWarehouseID
	repo.heads[saleID] = &domain.BillHead{
		ID: saleID, TenantID: shTenantID, BillNo: "SO-20261010-0001",
		BillType: domain.BillTypeSale, SubType: domain.BillSubTypeSale, Status: domain.StatusApproved,
		WarehouseID: &wh, TotalAmount: decimal.NewFromInt(100), PaidAmount: decimal.NewFromInt(100),
	}
	repo.items[saleID] = []*domain.BillItem{{
		ID: lineID, TenantID: shTenantID, HeadID: saleID, ProductID: shProductID, LineNo: 1,
		Qty: decimal.NewFromInt(5), UnitPrice: decimal.NewFromInt(20), LineAmount: decimal.NewFromInt(100),
	}}

	createUC := appbill.NewCreateSaleUseCase(repo)
	approveUC := appbill.NewApproveSaleUseCase(repo, stockUC, &shMockUnitRepo{}, &shMockPaymentRecorder{})
	sh := handlerbill.NewSaleHandler(createUC, approveUC, appbill.NewCancelPurchaseUseCase(repo),
		appbill.NewListPurchasesUseCase(repo), repo, appbill.NewQuickCheckoutUseCase(repo, approveUC),
		apppayment.NewListPaymentsUseCase(&shMockPaymentRepo{}))
	sh.WithReturn(appbill.NewReturnSaleUseCase(repo,
		appbill.NewCreateReturnBillUseCase(repo),
		appbill.NewApproveReturnBillUseCase(repo, stockUC)))
	return &returnHTTPFixture{repo: repo, stockUC: stockUC, router: newSaleRouter(sh), saleID: saleID, lineID: lineID}
}

func (f *returnHTTPFixture) post(saleID uuid.UUID, body map[string]any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/sale-bills/"+saleID.String()+"/return", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", tenantHeader(shTenantID))
	f.router.ServeHTTP(w, req)
	return w
}

func (f *returnHTTPFixture) line(id uuid.UUID, qty string) map[string]any {
	return map[string]any{"items": []map[string]any{{"bill_item_id": id.String(), "qty": qty}}}
}

func decode(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode %q: %v", w.Body.String(), err)
	}
	return m
}

// 原单 5 件退 2 件：201、库存加 2、详情显示已退 2；再退 4 件 422 并给出中文提示。
func TestSaleReturn_PartialThenOverReturn(t *testing.T) {
	f := newReturnHTTPFixture()

	w := f.post(f.saleID, f.line(f.lineID, "2"))
	if w.Code != http.StatusCreated {
		t.Fatalf("return 2: status = %d, body %s", w.Code, w.Body.String())
	}
	if got := decode(t, w)["return_amount"]; got != "40" {
		t.Errorf("return_amount = %v, want 40", got)
	}
	if f.stockUC.calls != 1 {
		t.Errorf("stock movements = %d, want 1", f.stockUC.calls)
	}

	// detail: returned qty on the line, receivable reduced, paid beyond the net is refund due
	gw := httptest.NewRecorder()
	greq, _ := http.NewRequest(http.MethodGet, "/api/v1/sale-bills/"+f.saleID.String(), nil)
	greq.Header.Set("X-Tenant-ID", tenantHeader(shTenantID))
	f.router.ServeHTTP(gw, greq)
	if gw.Code != http.StatusOK {
		t.Fatalf("get: status = %d, body %s", gw.Code, gw.Body.String())
	}
	detail := decode(t, gw)
	items := detail["items"].([]any)
	if got := items[0].(map[string]any)["returned_qty"]; got != "2" {
		t.Errorf("returned_qty = %v, want 2", got)
	}
	if detail["returned_amount"] != "40" || detail["receivable_amount"] != "0" || detail["refund_due_amount"] != "40" {
		t.Errorf("figures = returned %v receivable %v refund_due %v, want 40 / 0 / 40",
			detail["returned_amount"], detail["receivable_amount"], detail["refund_due_amount"])
	}

	w = f.post(f.saleID, f.line(f.lineID, "4"))
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("return 4: status = %d, want 422; body %s", w.Code, w.Body.String())
	}
	body := decode(t, w)
	if body["error"] != "return_exceeds_sold" || !strings.Contains(body["message"].(string), "最多还能退 3") {
		t.Errorf("body = %v, want return_exceeds_sold naming the 3 left", body)
	}
	if f.stockUC.calls != 1 {
		t.Errorf("stock movements = %d after a rejected return, want 1", f.stockUC.calls)
	}
}

func TestSaleReturn_DraftSale_Returns422(t *testing.T) {
	f := newReturnHTTPFixture()
	f.repo.heads[f.saleID].Status = domain.StatusDraft
	w := f.post(f.saleID, f.line(f.lineID, "1"))
	if w.Code != http.StatusUnprocessableEntity || decode(t, w)["error"] != "sale_not_returnable" {
		t.Fatalf("status = %d body %s, want 422 sale_not_returnable", w.Code, w.Body.String())
	}
}

// 别家租户的明细 id 在本单里查不到：400，且不动库存。
func TestSaleReturn_ForeignItemID_Returns400(t *testing.T) {
	f := newReturnHTTPFixture()
	w := f.post(f.saleID, f.line(uuid.New(), "1"))
	if w.Code != http.StatusBadRequest || decode(t, w)["error"] != "invalid_bill_item" {
		t.Fatalf("status = %d body %s, want 400 invalid_bill_item", w.Code, w.Body.String())
	}
	if f.stockUC.calls != 0 {
		t.Errorf("stock moved for a foreign line")
	}
}

func TestSaleReturn_BadInput(t *testing.T) {
	f := newReturnHTTPFixture()
	cases := map[string]map[string]any{
		"no items":       {"items": []map[string]any{}},
		"bad item id":    {"items": []map[string]any{{"bill_item_id": "not-a-uuid", "qty": "1"}}},
		"zero qty":       f.line(f.lineID, "0"),
		"negative qty":   f.line(f.lineID, "-1"),
		"non-number qty": f.line(f.lineID, "abc"),
		"bad warehouse":  {"items": f.line(f.lineID, "1")["items"], "warehouse_id": "nope"},
	}
	for name, body := range cases {
		if w := f.post(f.saleID, body); w.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400; body %s", name, w.Code, w.Body.String())
		}
	}
	if w := f.post(uuid.New(), f.line(f.lineID, "1")); w.Code != http.StatusNotFound {
		t.Errorf("unknown sale: status = %d, want 404", w.Code)
	}
	if f.stockUC.calls != 0 {
		t.Errorf("stock moved on rejected input")
	}
}

func TestSaleReturn_NoTenant_Returns401(t *testing.T) {
	f := newReturnHTTPFixture()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/sale-bills/"+f.saleID.String()+"/return", strings.NewReader("{}"))
	f.router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

// 已审核单据取消时的提示要指向真实存在的退货接口。
func TestSaleCancel_Approved_PointsAtReturnEndpoint(t *testing.T) {
	f := newReturnHTTPFixture()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/sale-bills/"+f.saleID.String()+"/cancel", nil)
	req.Header.Set("X-Tenant-ID", tenantHeader(shTenantID))
	f.router.ServeHTTP(w, req)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body %s", w.Code, w.Body.String())
	}
	body := decode(t, w)
	if body["error"] != "cannot_cancel_approved_bill" || body["action"] != "POST /api/v1/sale-bills/"+f.saleID.String()+"/return" {
		t.Errorf("body = %v, want the cancel error pointing at the return endpoint", body)
	}
}

func (r *retMockRepo) WarehouseExists(_ context.Context, _, id uuid.UUID) (bool, error) {
	return id == shWarehouseID, nil
}

// 填了不存在的仓库：400 加中文文案，不带原始 UUID，不动库存。
func TestSaleReturn_UnknownWarehouse_Returns400InChinese(t *testing.T) {
	f := newReturnHTTPFixture()
	body := f.line(f.lineID, "1")
	body["warehouse_id"] = uuid.New().String()
	w := f.post(f.saleID, body)
	got := decode(t, w)
	if w.Code != http.StatusBadRequest || got["error"] != "warehouse_not_found" {
		t.Fatalf("status = %d body %s, want 400 warehouse_not_found", w.Code, w.Body.String())
	}
	if strings.Contains(got["message"].(string), "-") {
		t.Errorf("message %q leaks an id", got["message"])
	}
	if f.stockUC.calls != 0 {
		t.Errorf("stock moved for an unknown warehouse")
	}
}

// 可退数量除不尽时提示按 4 位小数截断，不能吐出一长串小数。
func TestSaleReturn_ExceedMessage_TruncatesLongDecimals(t *testing.T) {
	f := newReturnHTTPFixture()
	f.repo.items[f.saleID][0].Qty = decimal.RequireFromString("1.23456789")
	w := f.post(f.saleID, f.line(f.lineID, "2"))
	msg, _ := decode(t, w)["message"].(string)
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(msg, "最多还能退 1.2345") || strings.Contains(msg, "1.23456") {
		t.Fatalf("status = %d msg %q, want 422 with 1.2345", w.Code, msg)
	}
}
