package bill_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	handlerbill "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/bill"
	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

// approvePurchaseWithBody drives POST /purchase-bills/:id/approve on a 100-total draft
// with a payment recorder wired, returning the response and the recorder.
func approvePurchaseWithBody(t *testing.T, body string) (*httptest.ResponseRecorder, *shMockPaymentRecorder, *mockBillRepo, uuid.UUID) {
	t.Helper()
	repo := newMockBillRepo()
	wh := uuid.New()
	billID := uuid.New()
	repo.bills[billID] = &domain.BillHead{
		ID: billID, TenantID: uuid.MustParse(devTenantID), Status: domain.StatusDraft,
		BillType: domain.BillTypePurchase, WarehouseID: &wh,
		TotalAmount: decimal.NewFromInt(100), CreatedAt: time.Now(),
	}
	payRec := &shMockPaymentRecorder{}
	h := handlerbill.New(
		appbill.NewCreatePurchaseDraftUseCase(repo),
		appbill.NewUpdatePurchaseDraftUseCase(repo),
		appbill.NewApprovePurchaseUseCase(repo, &shMockStockUC{}, &shMockUnitRepo{}).WithPayments(payRec),
		appbill.NewCancelPurchaseUseCase(repo),
		appbill.NewListPurchasesUseCase(repo),
		appbill.NewGetPurchaseUseCase(repo),
		appbill.NewRestorePurchaseUseCase(repo),
	)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/purchase-bills/"+billID.String()+"/approve", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", devTenantID)
	newRouter(h).ServeHTTP(w, req)
	return w, payRec, repo, billID
}

// TestBillHandler_Approve_WithPayment_RecordsPayment verifies pay-on-approval books the payment.
func TestBillHandler_Approve_WithPayment_RecordsPayment(t *testing.T) {
	w, payRec, repo, billID := approvePurchaseWithBody(t, `{"paid_amount":"40","payment_method":"transfer"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	if len(payRec.recorded) != 1 || !payRec.recorded[0].Amount.Equal(decimal.NewFromInt(40)) {
		t.Fatalf("payments = %+v, want one of 40", payRec.recorded)
	}
	if repo.bills[billID].Status != domain.StatusApproved {
		t.Errorf("status = %d, want approved", repo.bills[billID].Status)
	}
}

// TestBillHandler_Approve_EmptyBody_StillApproves keeps the bodyless approve working.
func TestBillHandler_Approve_EmptyBody_StillApproves(t *testing.T) {
	w, payRec, _, _ := approvePurchaseWithBody(t, ``)
	if w.Code != http.StatusOK || len(payRec.recorded) != 0 {
		t.Fatalf("status = %d payments = %d; body: %s", w.Code, len(payRec.recorded), w.Body.String())
	}
}

// TestBillHandler_Approve_InvalidPayMethod_Returns400 verifies bitcoin is refused with 400.
func TestBillHandler_Approve_InvalidPayMethod_Returns400(t *testing.T) {
	w, payRec, repo, billID := approvePurchaseWithBody(t, `{"paid_amount":"40","payment_method":"bitcoin"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["message"] != "不支持的收付款方式，请从列表中选择" {
		t.Errorf("message = %v", resp["message"])
	}
	if len(payRec.recorded) != 0 || repo.bills[billID].Status != domain.StatusDraft {
		t.Errorf("must not write: payments=%d status=%d", len(payRec.recorded), repo.bills[billID].Status)
	}
}

// TestBillHandler_Approve_PaidExceedsTotal_Returns422 verifies paying more than the bill is refused.
func TestBillHandler_Approve_PaidExceedsTotal_Returns422(t *testing.T) {
	w, payRec, _, _ := approvePurchaseWithBody(t, `{"paid_amount":"101"}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "paid_exceeds_total" {
		t.Errorf("error = %v", resp["error"])
	}
	if len(payRec.recorded) != 0 {
		t.Errorf("payments = %d, want 0", len(payRec.recorded))
	}
}

// TestBillHandler_Approve_BadPaidAmount_Returns400 verifies malformed amounts are refused.
func TestBillHandler_Approve_BadPaidAmount_Returns400(t *testing.T) {
	for _, body := range []string{`{"paid_amount":"-5"}`, `{"paid_amount":"abc"}`, `{"paid_amount":`} {
		if w, _, _, _ := approvePurchaseWithBody(t, body); w.Code != http.StatusBadRequest {
			t.Errorf("body %s: status = %d, want 400; resp: %s", body, w.Code, w.Body.String())
		}
	}
}

// TestSaleHandler_Approve_InvalidPayMethod_Returns400 verifies bitcoin on sale approve is 400, not cash.
func TestSaleHandler_Approve_InvalidPayMethod_Returns400(t *testing.T) {
	repo := newSHMockBillRepo()
	payRec := &shMockPaymentRecorder{}
	billID := seedSaleDraftForApprove(repo, 20)
	r := newSaleRouter(buildSaleHandlerForTest(repo, &shMockStockUC{}, payRec, &shMockPaymentRepo{}))

	w := postApprove(r, billID, `{"paid_amount":"10","payment_method":"bitcoin"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["message"] != "不支持的收付款方式，请从列表中选择" {
		t.Errorf("message = %v", resp["message"])
	}
	if len(payRec.recorded) != 0 {
		t.Errorf("payments = %d, want 0", len(payRec.recorded))
	}
}

// TestSaleHandler_QuickCheckout_InvalidPayMethod_Returns400 verifies the same for quick checkout.
func TestSaleHandler_QuickCheckout_InvalidPayMethod_Returns400(t *testing.T) {
	repo := newSHMockBillRepo()
	payRec := &shMockPaymentRecorder{}
	r := newSaleRouter(buildSaleHandlerForTest(repo, &shMockStockUC{}, payRec, &shMockPaymentRepo{}))

	w := postQuickCheckout(r, quickCheckoutBody("80", map[string]any{"payment_method": "bitcoin"}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["message"] != "不支持的收付款方式，请从列表中选择" {
		t.Errorf("message = %v", resp["message"])
	}
	if len(payRec.recorded) != 0 {
		t.Errorf("payments = %d, want 0", len(payRec.recorded))
	}
}
