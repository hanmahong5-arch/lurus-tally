package bill_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

func restoreSale(t *testing.T, repo *shMockBillRepo, id uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	r := newSaleRouter(buildSaleHandlerForTest(repo, &shMockStockUC{}, &shMockPaymentRecorder{}, &shMockPaymentRepo{}))
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/sale-bills/"+id.String()+"/restore", nil)
	req.Header.Set("X-Tenant-ID", tenantHeader(shTenantID))
	r.ServeHTTP(w, req)
	return w
}

// A cancelled sale draft goes back to draft: the "撤销" toast after cancelling answers 200.
func TestSaleHandler_Restore_Cancelled_Returns200(t *testing.T) {
	repo := newSHMockBillRepo()
	id := uuid.New()
	repo.heads[id] = &domain.BillHead{ID: id, TenantID: shTenantID, Status: domain.StatusCancelled, BillType: domain.BillTypeSale, SubType: domain.BillSubTypeSale}

	w := restoreSale(t, repo, id)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	if repo.heads[id].Status != domain.StatusDraft {
		t.Errorf("status = %v, want draft", repo.heads[id].Status)
	}
}

func TestSaleHandler_Restore_Approved_Returns409(t *testing.T) {
	repo := newSHMockBillRepo()
	id := uuid.New()
	repo.heads[id] = &domain.BillHead{ID: id, TenantID: shTenantID, Status: domain.StatusApproved, BillType: domain.BillTypeSale, SubType: domain.BillSubTypeSale}

	if w := restoreSale(t, repo, id); w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409; body: %s", w.Code, w.Body.String())
	}
}

// The sale route must not reach a purchase bill.
func TestSaleHandler_Restore_PurchaseBill_Returns404(t *testing.T) {
	repo := newSHMockBillRepo()
	id := uuid.New()
	repo.heads[id] = &domain.BillHead{ID: id, TenantID: shTenantID, Status: domain.StatusCancelled, BillType: domain.BillTypePurchase}

	w := restoreSale(t, repo, id)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
	if repo.heads[id].Status != domain.StatusCancelled {
		t.Errorf("purchase bill was touched")
	}
}

// A supplier return is stock-out too, but it is not a sale: 404.
func TestSaleHandler_Restore_PurchaseReturn_Returns404(t *testing.T) {
	repo := newSHMockBillRepo()
	id := uuid.New()
	repo.heads[id] = &domain.BillHead{ID: id, TenantID: shTenantID, Status: domain.StatusCancelled,
		BillType: domain.BillTypeSale, SubType: domain.BillSubTypePurchaseReturn}

	w := restoreSale(t, repo, id)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
	if repo.heads[id].Status != domain.StatusCancelled {
		t.Errorf("purchase return was touched")
	}
}

// A status that cannot be restored is a state conflict, not a server error.
func TestSaleHandler_Restore_InvalidStatus_Returns409(t *testing.T) {
	repo := newSHMockBillRepo()
	id := uuid.New()
	repo.heads[id] = &domain.BillHead{ID: id, TenantID: shTenantID, Status: domain.BillStatus(1),
		BillType: domain.BillTypeSale, SubType: domain.BillSubTypeSale}

	if w := restoreSale(t, repo, id); w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409; body: %s", w.Code, w.Body.String())
	}
}
