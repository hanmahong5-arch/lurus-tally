package bill_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	handlerbill "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/bill"
	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
	domainstock "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// ----- shared mock repo for handler tests -----

type mockBillRepo struct {
	bills   map[uuid.UUID]*domain.BillHead
	items   map[uuid.UUID][]*domain.BillItem
	counter int
}

func newMockBillRepo() *mockBillRepo {
	return &mockBillRepo{
		bills: make(map[uuid.UUID]*domain.BillHead),
		items: make(map[uuid.UUID][]*domain.BillItem),
	}
}

func (m *mockBillRepo) WithTx(_ context.Context, fn func(tx *sql.Tx) error) error {
	return fn(nil)
}

func (m *mockBillRepo) CreateBill(_ context.Context, _ *sql.Tx, head *domain.BillHead, its []*domain.BillItem) error {
	m.bills[head.ID] = head
	m.items[head.ID] = its
	return nil
}

func (m *mockBillRepo) GetBillForUpdate(_ context.Context, _ *sql.Tx, _, billID uuid.UUID) (*domain.BillHead, error) {
	h, ok := m.bills[billID]
	if !ok {
		return nil, appbill.ErrBillNotFound
	}
	return h, nil
}

func (m *mockBillRepo) GetBill(_ context.Context, _, billID uuid.UUID) (*domain.BillHead, error) {
	h, ok := m.bills[billID]
	if !ok {
		return nil, appbill.ErrBillNotFound
	}
	return h, nil
}

func (m *mockBillRepo) GetBillItems(_ context.Context, _, billID uuid.UUID) ([]*domain.BillItem, error) {
	return m.items[billID], nil
}

func (m *mockBillRepo) UpdateBillStatus(_ context.Context, _ *sql.Tx, _, billID uuid.UUID, status domain.BillStatus, meta map[string]any) error {
	h, ok := m.bills[billID]
	if !ok {
		return appbill.ErrBillNotFound
	}
	h.Status = status
	if meta != nil {
		if at, ok2 := meta["approved_at"]; ok2 {
			t := at.(time.Time)
			h.ApprovedAt = &t
		}
		if by, ok2 := meta["approved_by"]; ok2 {
			id := by.(uuid.UUID)
			h.ApprovedBy = &id
		}
	}
	return nil
}

func (m *mockBillRepo) UpdateBill(_ context.Context, _ *sql.Tx, head *domain.BillHead, its []*domain.BillItem) error {
	m.bills[head.ID] = head
	m.items[head.ID] = its
	return nil
}

func (m *mockBillRepo) ListBills(_ context.Context, f appbill.BillListFilter) ([]domain.BillHead, int64, error) {
	var out []domain.BillHead
	for _, h := range m.bills {
		out = append(out, *h)
	}
	return out, int64(len(out)), nil
}

func (m *mockBillRepo) NextBillNo(_ context.Context, _ *sql.Tx, _ uuid.UUID, prefix string) (string, error) {
	m.counter++
	return fmt.Sprintf("%s-%s-%04d", prefix, time.Now().Format("20060102"), m.counter), nil
}

func (m *mockBillRepo) AcquireBillAdvisoryLock(_ context.Context, _ *sql.Tx, _, _ uuid.UUID) error {
	return nil
}

// ----- mock stock executor -----

type mockStockExecutor struct{}

func (m *mockStockExecutor) ExecuteInTx(_ context.Context, _ *sql.Tx, req appstock.RecordMovementRequest) (*domainstock.Snapshot, error) {
	return &domainstock.Snapshot{OnHandQty: req.Qty}, nil
}

// ----- mock product unit repo -----

type mockUnitRepo struct{}

func (m *mockUnitRepo) GetConversionFactor(_ context.Context, _, _ uuid.UUID) (decimal.Decimal, error) {
	return decimal.NewFromInt(1), nil
}

// ----- test router helper -----

func newTestHandler(repo *mockBillRepo) *handlerbill.Handler {
	// stockUC and unitRepo are not needed for most handler tests
	// but we wire stubs to keep tests isolated
	return handlerbill.New(
		appbill.NewCreatePurchaseDraftUseCase(repo),
		appbill.NewUpdatePurchaseDraftUseCase(repo),
		appbill.NewApprovePurchaseUseCase(repo, nil, nil), // approve stubs handled per test
		appbill.NewCancelPurchaseUseCase(repo),
		appbill.NewListPurchasesUseCase(repo),
		appbill.NewGetPurchaseUseCase(repo),
	)
}

func newRouter(h *handlerbill.Handler) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	api := r.Group("/api/v1")
	h.RegisterRoutes(api)
	return r
}

const devTenantID = "11111111-1111-1111-1111-111111111111"

func validCreateBody() []byte {
	body := map[string]any{
		"warehouse_id": uuid.New().String(),
		"bill_date":    time.Now().UTC().Format(time.RFC3339),
		"items": []map[string]any{
			{"product_id": uuid.New().String(), "qty": "10", "unit_price": "8.00", "line_no": 1},
			{"product_id": uuid.New().String(), "qty": "5", "unit_price": "20.00", "line_no": 2},
			{"product_id": uuid.New().String(), "qty": "3", "unit_price": "15.00", "line_no": 3},
		},
	}
	b, _ := json.Marshal(body)
	return b
}

// TestBillHandler_CreatePurchase_Returns201 verifies that a valid POST returns 201.
func TestBillHandler_CreatePurchase_Returns201(t *testing.T) {
	repo := newMockBillRepo()
	r := newRouter(newTestHandler(repo))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/purchase-bills", bytes.NewReader(validCreateBody()))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", devTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["bill_id"] == nil || resp["bill_id"] == "" {
		t.Error("bill_id missing in response")
	}
	if resp["bill_no"] == nil || resp["bill_no"] == "" {
		t.Error("bill_no missing in response")
	}
}

// TestBillHandler_ListPurchases_ReturnsPaginatedResult verifies GET returns 200 with items.
func TestBillHandler_ListPurchases_ReturnsPaginatedResult(t *testing.T) {
	repo := newMockBillRepo()
	// Seed one bill.
	billID := uuid.New()
	tenantID, _ := uuid.Parse(devTenantID)
	now := time.Now()
	repo.bills[billID] = &domain.BillHead{
		ID: billID, TenantID: tenantID, BillNo: "PO-20260423-0001",
		BillType: domain.BillTypePurchase, Status: domain.StatusDraft,
		CreatorID: tenantID, BillDate: now, CreatedAt: now, UpdatedAt: now,
	}

	r := newRouter(newTestHandler(repo))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/purchase-bills?page=1&size=20", nil)
	req.Header.Set("X-Tenant-ID", devTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["total"] == nil {
		t.Error("total missing")
	}
}

// TestBillHandler_CancelApproved_Returns422 verifies that cancelling an approved bill
// returns 422 with the correct error code.
func TestBillHandler_CancelApproved_Returns422(t *testing.T) {
	repo := newMockBillRepo()
	billID := uuid.New()
	tenantID, _ := uuid.Parse(devTenantID)
	now := time.Now()
	repo.bills[billID] = &domain.BillHead{
		ID: billID, TenantID: tenantID, BillNo: "PO-20260423-0001",
		BillType: domain.BillTypePurchase, Status: domain.StatusApproved,
		CreatorID: tenantID, BillDate: now, CreatedAt: now, UpdatedAt: now,
	}

	r := newRouter(newTestHandler(repo))

	path := "/api/v1/purchase-bills/" + billID.String() + "/cancel"
	req := httptest.NewRequest(http.MethodPost, path, nil)
	req.Header.Set("X-Tenant-ID", devTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["error"] != "cannot_cancel_approved_bill" {
		t.Errorf("error code = %v, want cannot_cancel_approved_bill", resp["error"])
	}
}

// TestBillHandler_GetPurchase_NotFound_Returns404 verifies 404 for unknown bill.
func TestBillHandler_GetPurchase_NotFound_Returns404(t *testing.T) {
	repo := newMockBillRepo()
	r := newRouter(newTestHandler(repo))

	path := "/api/v1/purchase-bills/" + uuid.New().String()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("X-Tenant-ID", devTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
}
