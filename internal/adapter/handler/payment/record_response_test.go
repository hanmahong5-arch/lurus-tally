package payment_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

func postPayment(t *testing.T, billType domain.BillType, total decimal.Decimal, body map[string]any) (*httptest.ResponseRecorder, *phMockPaymentRepo) {
	t.Helper()
	billReader := newPHMockBillReader()
	payRepo := &phMockPaymentRepo{}
	billReader.bills[phBillID] = &domain.BillHead{
		ID: phBillID, TenantID: phTenantID, BillType: billType,
		Status: domain.StatusApproved, TotalAmount: total, CreatedAt: time.Now(),
	}
	r := newPaymentRouter(buildPaymentHandler(billReader, payRepo))
	b, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/payments", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", phTenantID.String())
	r.ServeHTTP(w, req)
	return w, payRepo
}

// TestPaymentHandler_Record_ReturnsFullPayment verifies 201 carries the persisted
// payment so the client never has to assemble the numbers itself.
func TestPaymentHandler_Record_ReturnsFullPayment(t *testing.T) {
	w, payRepo := postPayment(t, domain.BillTypeSale, decimal.NewFromInt(500), map[string]any{
		"bill_id": phBillID.String(), "amount": "120.5", "payment_method": "wechat", "remark": "定金",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["id"] != payRepo.recorded[0].ID.String() {
		t.Errorf("id = %v, want %s", resp["id"], payRepo.recorded[0].ID)
	}
	if resp["bill_id"] != phBillID.String() || resp["pay_type"] != "wechat" || resp["amount"] != "120.5" || resp["remark"] != "定金" {
		t.Errorf("resp = %v", resp)
	}
	if d, _ := resp["pay_date"].(string); d == "" {
		t.Errorf("pay_date missing: %v", resp)
	}
}

// TestPaymentHandler_Record_ExceedsReceivable_Returns422Chinese verifies the over-payment
// refusal is 422 with a plain Chinese message and no English fragments.
func TestPaymentHandler_Record_ExceedsReceivable_Returns422Chinese(t *testing.T) {
	w, payRepo := postPayment(t, domain.BillTypeSale, decimal.NewFromInt(100), map[string]any{
		"bill_id": phBillID.String(), "amount": "150",
	})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["message"] != "本次 ¥150.00 超过待收 ¥100.00" {
		t.Errorf("message = %v", resp["message"])
	}
	if len(payRepo.recorded) != 0 {
		t.Errorf("payments = %d, want 0", len(payRepo.recorded))
	}
}

// TestPaymentHandler_Record_ValidationMessagesAreChinese verifies the 400 paths carry no
// English text a shop owner would see.
func TestPaymentHandler_Record_ValidationMessagesAreChinese(t *testing.T) {
	latin := regexp.MustCompile(`[A-Za-z]{3,}`)
	cases := map[string]map[string]any{
		"bad bill id": {"bill_id": "x", "amount": "10"},
		"zero amount": {"bill_id": phBillID.String(), "amount": "0"},
		"huge amount": {"bill_id": phBillID.String(), "amount": "99999999999"},
		"bad method":  {"bill_id": phBillID.String(), "amount": "10", "payment_method": "bitcoin"},
	}
	for name, body := range cases {
		w, _ := postPayment(t, domain.BillTypeSale, decimal.NewFromInt(100), body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, w.Code)
			continue
		}
		var resp map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if msg, _ := resp["message"].(string); msg == "" || latin.MatchString(msg) {
			t.Errorf("%s: message %q must be non-empty Chinese", name, msg)
		}
	}
}

// TestPaymentHandler_Record_UnknownBill_Returns404 verifies a missing bill is a plain 404.
func TestPaymentHandler_Record_UnknownBill_Returns404(t *testing.T) {
	billReader := newPHMockBillReader()
	r := newPaymentRouter(buildPaymentHandler(billReader, &phMockPaymentRepo{}))
	b, _ := json.Marshal(map[string]any{"bill_id": phBillID.String(), "amount": "10"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/payments", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", phTenantID.String())
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
}

// TestPaymentHandler_Record_DraftBill_Returns422 verifies recording against a draft says so in Chinese.
func TestPaymentHandler_Record_DraftBill_Returns422(t *testing.T) {
	billReader := newPHMockBillReader()
	billReader.bills[phBillID] = &domain.BillHead{ID: phBillID, TenantID: phTenantID, Status: domain.StatusDraft, TotalAmount: decimal.NewFromInt(10)}
	r := newPaymentRouter(buildPaymentHandler(billReader, &phMockPaymentRepo{}))
	b, _ := json.Marshal(map[string]any{"bill_id": phBillID.String(), "amount": "5"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/payments", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", phTenantID.String())
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", w.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["message"] != "单据还没有审核，不能登记收付款" {
		t.Errorf("message = %v", resp["message"])
	}
}

func TestPaymentHandler_Record_CreditMethod_Returns400(t *testing.T) {
	w, payRepo := postPayment(t, domain.BillTypeSale, decimal.NewFromInt(100), map[string]any{
		"bill_id": phBillID.String(), "amount": "10", "payment_method": "credit",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
	if len(payRepo.recorded) != 0 {
		t.Errorf("payments = %d, want 0", len(payRepo.recorded))
	}
}

func TestPaymentHandler_Record_MoreThanTwoDecimals_Returns400(t *testing.T) {
	for _, amt := range []string{"0.123", "1e2", "10.005"} {
		w, payRepo := postPayment(t, domain.BillTypeSale, decimal.NewFromInt(100), map[string]any{
			"bill_id": phBillID.String(), "amount": amt,
		})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400; body: %s", amt, w.Code, w.Body.String())
		}
		if len(payRepo.recorded) != 0 {
			t.Errorf("%s: payments = %d, want 0", amt, len(payRepo.recorded))
		}
	}
}
