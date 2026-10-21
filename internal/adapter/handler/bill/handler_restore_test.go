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
		SubType:   domain.BillSubTypePurchase,
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
		SubType:   domain.BillSubTypePurchase,
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

// A status that cannot be restored is a state conflict (409), not a server error.
func TestBillHandler_RestorePurchase_Returns409OnInvalidStatus(t *testing.T) {
	repo := newMockBillRepo()
	tenantID, _ := uuid.Parse(devTenantID)
	billID := uuid.New()
	now := time.Now()
	repo.bills[billID] = &domain.BillHead{
		ID: billID, TenantID: tenantID, BillNo: "PO-20260101-0003", BillType: domain.BillTypePurchase, SubType: domain.BillSubTypePurchase,
		Status: domain.BillStatus(1), CreatorID: tenantID, BillDate: now, CreatedAt: now, UpdatedAt: now,
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

// A cancelled sale draft (出库) is not a purchase: the purchase restore route must not revive it.
func TestBillHandler_RestorePurchase_Returns404OnSaleBill(t *testing.T) {
	repo := newMockBillRepo()
	tenantID, _ := uuid.Parse(devTenantID)
	billID := uuid.New()
	now := time.Now()
	repo.bills[billID] = &domain.BillHead{
		ID: billID, TenantID: tenantID, BillNo: "SO-20260101-0001", BillType: domain.BillTypeSale,
		SubType: domain.BillSubTypeSale, Status: domain.StatusCancelled, CreatorID: tenantID,
		BillDate: now, CreatedAt: now, UpdatedAt: now,
	}

	r := newRouter(newTestHandler(repo))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/purchase-bills/"+billID.String()+"/restore", nil)
	req.Header.Set("X-Tenant-ID", devTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
	if repo.bills[billID].Status != domain.StatusCancelled {
		t.Errorf("sale bill must stay cancelled, got status %d", repo.bills[billID].Status)
	}
}

// Deciding whether a bill is a purchase needs only the head; restoring must not load the lines.
func TestBillHandler_RestorePurchase_ReadsHeadOnly(t *testing.T) {
	repo := newMockBillRepo()
	tenantID, _ := uuid.Parse(devTenantID)
	billID := uuid.New()
	now := time.Now()
	repo.bills[billID] = &domain.BillHead{
		ID: billID, TenantID: tenantID, BillNo: "PO-20260101-0009", BillType: domain.BillTypePurchase, SubType: domain.BillSubTypePurchase,
		Status: domain.StatusCancelled, CreatorID: tenantID, BillDate: now, CreatedAt: now, UpdatedAt: now,
	}

	r := newRouter(newTestHandler(repo))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/purchase-bills/"+billID.String()+"/restore", nil)
	req.Header.Set("X-Tenant-ID", devTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	if repo.itemReads != 0 {
		t.Errorf("restore read bill lines %d time(s), want 0", repo.itemReads)
	}
}
