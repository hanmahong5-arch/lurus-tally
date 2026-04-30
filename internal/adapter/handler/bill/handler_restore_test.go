package bill_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

// TestBillHandler_RestorePurchase_ReturnsOKOnCancelledBill verifies that restoring a cancelled
// bill returns HTTP 200 with status "draft".
func TestBillHandler_RestorePurchase_ReturnsOKOnCancelledBill(t *testing.T) {
	repo := newMockBillRepo()

	// Seed a cancelled bill.
	tenantID, _ := uuid.Parse(devTenantID)
	billID := uuid.New()
	now := time.Now()
	repo.bills[billID] = &domain.BillHead{
		ID:        billID,
		TenantID:  tenantID,
		BillNo:    "PO-20260101-0001",
		BillType:  domain.BillTypePurchase,
		Status:    domain.StatusCancelled,
		CreatorID: tenantID,
		BillDate:  now,
		CreatedAt: now,
		UpdatedAt: now,
	}

	r := newRouter(newTestHandler(repo))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/purchase-bills/"+billID.String()+"/restore", nil)
	req.Header.Set("X-Tenant-ID", devTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	if repo.bills[billID].Status != domain.StatusDraft {
		t.Errorf("bill status = %d, want %d (Draft)", repo.bills[billID].Status, domain.StatusDraft)
	}
}

// TestBillHandler_RestorePurchase_Returns409OnApprovedBill verifies that restoring an approved
// bill returns HTTP 409 Conflict.
func TestBillHandler_RestorePurchase_Returns409OnApprovedBill(t *testing.T) {
	repo := newMockBillRepo()

	// Seed an approved bill.
	tenantID, _ := uuid.Parse(devTenantID)
	billID := uuid.New()
	now := time.Now()
	repo.bills[billID] = &domain.BillHead{
		ID:        billID,
		TenantID:  tenantID,
		BillNo:    "PO-20260101-0002",
		BillType:  domain.BillTypePurchase,
		Status:    domain.StatusApproved,
		CreatorID: tenantID,
		BillDate:  now,
		CreatedAt: now,
		UpdatedAt: now,
	}

	r := newRouter(newTestHandler(repo))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/purchase-bills/"+billID.String()+"/restore", nil)
	req.Header.Set("X-Tenant-ID", devTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409; body: %s", w.Code, w.Body.String())
	}
}
