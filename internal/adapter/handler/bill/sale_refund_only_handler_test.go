package bill_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// 退货已入库、退款没记上：补记退款走同一个接口，不再退货，只写一笔负付款。
func TestSaleReturn_RefundOnlyBooksMissingRefund(t *testing.T) {
	f := newRefundHTTPFixture()
	rw := f.post(f.saleID, f.line(f.lineID, "2"))
	if rw.Code != http.StatusCreated {
		t.Fatalf("return: status = %d, body %s", rw.Code, rw.Body.String())
	}
	retID, _ := decode(t, rw)["bill_id"].(string)
	body := map[string]any{"refund_only": true, "refund_amount": "40", "refund_method": "cash", "return_bill_id": retID}
	w := f.post(f.saleID, body)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}
	if got := decode(t, w); got["refunded_amount"] != "40" {
		t.Errorf("body = %v, want refunded_amount 40", got)
	}
	if paid := f.repo.heads[f.saleID].PaidAmount; !paid.Equal(decimal.NewFromInt(60)) {
		t.Errorf("paid = %s, want 60", paid)
	}
}

// 补记退款不带金额，或超过上限：400 / 422，不写付款。
func TestSaleReturn_RefundOnlyRejectsBadAmounts(t *testing.T) {
	f := newRefundHTTPFixture()
	if w := f.post(f.saleID, map[string]any{"refund_only": true}); w.Code != http.StatusBadRequest {
		t.Errorf("no amount: status = %d, want 400; body %s", w.Code, w.Body.String())
	}
	w := f.post(f.saleID, map[string]any{"refund_only": true, "refund_amount": "40", "refund_method": "cash", "return_bill_id": uuid.New().String()})
	if w.Code != http.StatusBadRequest || decode(t, w)["error"] != "return_not_of_sale" {
		t.Errorf("unknown return: status = %d body %s, want 400 return_not_of_sale", w.Code, w.Body.String())
	}
	rw := f.post(f.saleID, f.line(f.lineID, "2"))
	retID, _ := decode(t, rw)["bill_id"].(string)
	w = f.post(f.saleID, map[string]any{"refund_only": true, "refund_amount": "12.345", "return_bill_id": retID})
	if w.Code != http.StatusBadRequest {
		t.Errorf("3 decimals: status = %d, want 400; body %s", w.Code, w.Body.String())
	}
	body := f.line(f.lineID, "1")
	body["refund_amount"] = "12.345"
	if w := f.post(f.saleID, body); w.Code != http.StatusBadRequest {
		t.Errorf("return with 3 decimal refund: status = %d, want 400; body %s", w.Code, w.Body.String())
	}
	w = f.post(f.saleID, map[string]any{"refund_only": true, "refund_amount": "90", "refund_method": "cash", "return_bill_id": retID})
	if w.Code != http.StatusUnprocessableEntity || decode(t, w)["error"] != "refund_exceeds_paid" {
		t.Errorf("no return yet: status = %d body %s, want 422 refund_exceeds_paid", w.Code, w.Body.String())
	}
	if paid := f.repo.heads[f.saleID].PaidAmount; !paid.Equal(decimal.NewFromInt(100)) {
		t.Errorf("paid = %s, want 100", paid)
	}
}

// 两人同时点补记退款，拿不到单据锁：409 并给出中文提示，不是 500。
func TestSaleReturn_RefundOnlyLockBusy_Returns409(t *testing.T) {
	f := newRefundHTTPFixture()
	rw := f.post(f.saleID, f.line(f.lineID, "2"))
	if rw.Code != http.StatusCreated {
		t.Fatalf("return: status = %d, body %s", rw.Code, rw.Body.String())
	}
	retID, _ := decode(t, rw)["bill_id"].(string)
	f.repo.lockErr = errors.New("lock timeout")
	w := f.post(f.saleID, map[string]any{"refund_only": true, "refund_amount": "40", "refund_method": "cash", "return_bill_id": retID})
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409, body %s", w.Code, w.Body.String())
	}
}
