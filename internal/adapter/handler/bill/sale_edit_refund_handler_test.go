package bill_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	handlerbill "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/bill"
	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

func putSale(f *returnHTTPFixture, id uuid.UUID, body map[string]any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPut, "/api/v1/sale-bills/"+id.String(), bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", tenantHeader(shTenantID))
	f.router.ServeHTTP(w, req)
	return w
}

// 销售草稿可以改：PUT 换掉明细、重算合计，单号不变。
func TestSaleUpdate_DraftIsEditable(t *testing.T) {
	f := newReturnHTTPFixture()
	f.repo.heads[f.saleID].Status = domain.StatusDraft
	f.repo.heads[f.saleID].PaidAmount = decimal.Zero

	w := putSale(f, f.saleID, map[string]any{
		"items": []map[string]any{{"product_id": shProductID.String(), "qty": "4", "unit_price": "12.5", "line_no": 1}},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}
	head := f.repo.heads[f.saleID]
	if !head.TotalAmount.Equal(decimal.NewFromInt(50)) || head.BillNo != "SO-20261010-0001" {
		t.Errorf("head total=%s no=%s, want 50 and the same number", head.TotalAmount, head.BillNo)
	}
	if items := f.repo.items[f.saleID]; len(items) != 1 || !items[0].Qty.Equal(decimal.NewFromInt(4)) {
		t.Errorf("items = %+v, want one line of 4", items)
	}
}

// 已审核的销售单不能改：422，数据不动。
func TestSaleUpdate_ApprovedIs422(t *testing.T) {
	f := newReturnHTTPFixture()
	w := putSale(f, f.saleID, map[string]any{
		"items": []map[string]any{{"product_id": shProductID.String(), "qty": "4", "unit_price": "1", "line_no": 1}},
	})
	if w.Code != http.StatusUnprocessableEntity || decode(t, w)["error"] != "invalid_bill_status" {
		t.Fatalf("status = %d body %s, want 422 invalid_bill_status", w.Code, w.Body.String())
	}
}

func TestSaleUpdate_BadInput(t *testing.T) {
	f := newReturnHTTPFixture()
	f.repo.heads[f.saleID].Status = domain.StatusDraft
	for name, body := range map[string]map[string]any{
		"no items": {"items": []map[string]any{}},
		"bad qty":  {"items": []map[string]any{{"product_id": shProductID.String(), "qty": "0", "unit_price": "1"}}},
		"bad id":   {"items": []map[string]any{{"product_id": "x", "qty": "1", "unit_price": "1"}}},
	} {
		if w := putSale(f, f.saleID, body); w.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400; body %s", name, w.Code, w.Body.String())
		}
	}
	w := putSale(f, uuid.New(), map[string]any{
		"items": []map[string]any{{"product_id": shProductID.String(), "qty": "1", "unit_price": "1"}},
	})
	if w.Code != http.StatusNotFound {
		t.Errorf("unknown bill: status = %d, want 404", w.Code)
	}
}

// newRefundHTTPFixture is the return fixture with refunds wired in.
func newRefundHTTPFixture() *returnHTTPFixture {
	f := newReturnHTTPFixture()
	createUC := appbill.NewCreateSaleUseCase(f.repo)
	approveUC := appbill.NewApproveSaleUseCase(f.repo, f.stockUC, &shMockUnitRepo{}, &shMockPaymentRecorder{})
	retUC := appbill.NewReturnSaleUseCase(f.repo,
		appbill.NewCreateReturnBillUseCase(f.repo),
		appbill.NewApproveReturnBillUseCase(f.repo, f.stockUC))
	sh := handlerbill.NewSaleHandler(createUC, approveUC, appbill.NewCancelPurchaseUseCase(f.repo),
		appbill.NewListPurchasesUseCase(f.repo), f.repo, appbill.NewQuickCheckoutUseCase(f.repo, approveUC), nil)
	sh.WithReturn(retUC).WithRefunds(appbill.NewSaleRefundUseCase(f.repo, retUC, &shMockPaymentRecorder{}))
	f.router = newSaleRouter(sh)
	return f
}

// 退 2 件（40）并退现金 40：201，回执带 refunded_amount，已收降到 60。
func TestSaleReturn_WithRefund(t *testing.T) {
	f := newRefundHTTPFixture()
	body := f.line(f.lineID, "2")
	body["refund_amount"], body["refund_method"] = "40", "cash"
	w := f.post(f.saleID, body)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}
	got := decode(t, w)
	if got["refunded_amount"] != "40" || got["refund_error"] != nil {
		t.Errorf("body = %v, want refunded_amount 40 and no refund_error", got)
	}
	if paid := f.repo.heads[f.saleID].PaidAmount; !paid.Equal(decimal.NewFromInt(60)) {
		t.Errorf("paid = %s, want 60", paid)
	}
}

// 退款超过实收净额：422 refund_exceeds_paid，中文提示最多能退多少，库存不动。
func TestSaleReturn_RefundTooLarge_Returns422(t *testing.T) {
	f := newRefundHTTPFixture()
	body := f.line(f.lineID, "2")
	body["refund_amount"], body["refund_method"] = "50", "cash"
	w := f.post(f.saleID, body)
	if w.Code != http.StatusUnprocessableEntity || decode(t, w)["error"] != "refund_exceeds_paid" {
		t.Fatalf("status = %d body %s, want 422 refund_exceeds_paid", w.Code, w.Body.String())
	}
	if f.stockUC.calls != 0 {
		t.Errorf("stock moved for a rejected refund")
	}
}

func TestSaleReturn_BadRefundInput_Returns400(t *testing.T) {
	f := newRefundHTTPFixture()
	for name, extra := range map[string]map[string]any{
		"not a number": {"refund_amount": "abc"},
		"negative":     {"refund_amount": "-5"},
		"credit":       {"refund_amount": "10", "refund_method": "credit"},
		"unknown":      {"refund_amount": "10", "refund_method": "bitcoin"},
	} {
		body := f.line(f.lineID, "1")
		for k, v := range extra {
			body[k] = v
		}
		if w := f.post(f.saleID, body); w.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400; body %s", name, w.Code, w.Body.String())
		}
	}
	if f.stockUC.calls != 0 {
		t.Errorf("stock moved on rejected input")
	}
}

// 采购草稿改单时没传日期：保留原单据日期，不能被改成"现在"。
func TestPurchaseUpdate_OmittedDateKeepsOriginal(t *testing.T) {
	f := newPOHTTPFixture(true)
	orig := time.Date(2026, 9, 1, 3, 0, 0, 0, time.UTC)
	f.repo.heads[f.poID].Status = domain.StatusDraft
	f.repo.heads[f.poID].BillDate = orig
	w := f.do(http.MethodPut, "/api/v1/purchase-bills/"+f.poID.String(), map[string]any{
		"items": []map[string]any{{"product_id": shProductID.String(), "qty": "2", "unit_price": "5", "line_no": 1}},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}
	if got := f.repo.heads[f.poID].BillDate; !got.Equal(orig) {
		t.Errorf("bill date = %v, want the original %v", got, orig)
	}
}
