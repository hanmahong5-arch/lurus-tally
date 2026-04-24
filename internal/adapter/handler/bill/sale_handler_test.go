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
	apppayment "github.com/hanmahong5-arch/lurus-tally/internal/app/payment"
	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
	domainpayment "github.com/hanmahong5-arch/lurus-tally/internal/domain/payment"
	domainstock "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// ----- shared test fixtures -----

var (
	shTenantID    = uuid.New()
	shWarehouseID = uuid.New()
	shProductID   = uuid.New()
)

// ----- mock BillRepo (sale handler tests) -----

type shMockBillRepo struct {
	heads         map[uuid.UUID]*domain.BillHead
	items         map[uuid.UUID][]*domain.BillItem
	listResult    []domain.BillHead
	listTotal     int64
	updatePaidErr error
}

func newSHMockBillRepo() *shMockBillRepo {
	return &shMockBillRepo{
		heads: make(map[uuid.UUID]*domain.BillHead),
		items: make(map[uuid.UUID][]*domain.BillItem),
	}
}

func (m *shMockBillRepo) WithTx(_ context.Context, fn func(tx *sql.Tx) error) error {
	return fn(nil) //nolint:staticcheck
}

func (m *shMockBillRepo) CreateBill(_ context.Context, _ *sql.Tx, head *domain.BillHead, its []*domain.BillItem) error {
	m.heads[head.ID] = head
	m.items[head.ID] = its
	return nil
}

func (m *shMockBillRepo) GetBillForUpdate(_ context.Context, _ *sql.Tx, _, billID uuid.UUID) (*domain.BillHead, error) {
	h, ok := m.heads[billID]
	if !ok {
		return nil, appbill.ErrBillNotFound
	}
	return h, nil
}

func (m *shMockBillRepo) GetBill(_ context.Context, _, billID uuid.UUID) (*domain.BillHead, error) {
	h, ok := m.heads[billID]
	if !ok {
		return nil, appbill.ErrBillNotFound
	}
	return h, nil
}

func (m *shMockBillRepo) GetBillItems(_ context.Context, _, billID uuid.UUID) ([]*domain.BillItem, error) {
	return m.items[billID], nil
}

func (m *shMockBillRepo) UpdateBillStatus(_ context.Context, _ *sql.Tx, _, billID uuid.UUID, status domain.BillStatus, meta map[string]any) error {
	h, ok := m.heads[billID]
	if !ok {
		return appbill.ErrBillNotFound
	}
	h.Status = status
	if meta != nil {
		if at, ok := meta["approved_at"]; ok {
			t := at.(time.Time)
			h.ApprovedAt = &t
		}
		if by, ok := meta["approved_by"]; ok {
			id := by.(uuid.UUID)
			h.ApprovedBy = &id
		}
	}
	return nil
}

func (m *shMockBillRepo) UpdateBill(_ context.Context, _ *sql.Tx, head *domain.BillHead, its []*domain.BillItem) error {
	m.heads[head.ID] = head
	m.items[head.ID] = its
	return nil
}

func (m *shMockBillRepo) ListBills(_ context.Context, _ appbill.BillListFilter) ([]domain.BillHead, int64, error) {
	return m.listResult, m.listTotal, nil
}

func (m *shMockBillRepo) NextBillNo(_ context.Context, _ *sql.Tx, _ uuid.UUID, prefix string) (string, error) {
	return fmt.Sprintf("%s-%s-0001", prefix, time.Now().Format("20060102")), nil
}

func (m *shMockBillRepo) AcquireBillAdvisoryLock(_ context.Context, _ *sql.Tx, _, _ uuid.UUID) error {
	return nil
}

func (m *shMockBillRepo) UpdatePaidAmount(_ context.Context, _ *sql.Tx, _, billID uuid.UUID, paidAmount decimal.Decimal) error {
	if m.updatePaidErr != nil {
		return m.updatePaidErr
	}
	if h, ok := m.heads[billID]; ok {
		h.PaidAmount = paidAmount
	}
	return nil
}

var _ appbill.BillRepo = (*shMockBillRepo)(nil)

// ----- mock StockUC -----

type shMockStockUC struct {
	failErr error
	calls   int
}

func (m *shMockStockUC) ExecuteInTx(_ context.Context, _ *sql.Tx, req appstock.RecordMovementRequest) (*domainstock.Snapshot, error) {
	if m.failErr != nil {
		return nil, m.failErr
	}
	m.calls++
	return &domainstock.Snapshot{TenantID: req.TenantID, ProductID: req.ProductID}, nil
}

// ----- mock UnitRepo -----

type shMockUnitRepo struct{}

func (m *shMockUnitRepo) GetConversionFactor(_ context.Context, _, _ uuid.UUID) (decimal.Decimal, error) {
	return decimal.NewFromInt(1), nil
}

// ----- mock PaymentRecorder -----

type shMockPaymentRecorder struct {
	recorded []*domainpayment.Payment
}

func (m *shMockPaymentRecorder) Record(_ context.Context, _ *sql.Tx, p *domainpayment.Payment) error {
	m.recorded = append(m.recorded, p)
	return nil
}

// ----- mock ListPaymentsUseCase (via PaymentRepo) -----

type shMockPaymentRepo struct {
	payments []*domainpayment.Payment
}

func (m *shMockPaymentRepo) Record(_ context.Context, _ *sql.Tx, p *domainpayment.Payment) error {
	m.payments = append(m.payments, p)
	return nil
}

func (m *shMockPaymentRepo) ListByBill(_ context.Context, _, _ uuid.UUID) ([]*domainpayment.Payment, error) {
	if m.payments == nil {
		return []*domainpayment.Payment{}, nil
	}
	return m.payments, nil
}

func (m *shMockPaymentRepo) SumByBill(_ context.Context, _ *sql.Tx, _, _ uuid.UUID) (decimal.Decimal, error) {
	return decimal.Zero, nil
}

func (m *shMockPaymentRepo) WithTx(_ context.Context, fn func(tx *sql.Tx) error) error {
	return fn(nil) //nolint:staticcheck
}

var _ apppayment.PaymentRepo = (*shMockPaymentRepo)(nil)

// ----- helper to build a SaleHandler for tests -----

func buildSaleHandlerForTest(repo *shMockBillRepo, stockUC *shMockStockUC, payRec *shMockPaymentRecorder, payRepo *shMockPaymentRepo) *handlerbill.SaleHandler {
	createUC := appbill.NewCreateSaleUseCase(repo)
	approveUC := appbill.NewApproveSaleUseCase(repo, stockUC, &shMockUnitRepo{}, payRec)
	cancelUC := appbill.NewCancelPurchaseUseCase(repo)
	listPaymentsUC := apppayment.NewListPaymentsUseCase(payRepo)
	quickCheckoutUC := appbill.NewQuickCheckoutUseCase(repo, approveUC)
	// ListPurchasesUseCase is used for listing by type; we pass a fake one
	listUC := appbill.NewListPurchasesUseCase(repo)
	return handlerbill.NewSaleHandler(createUC, approveUC, cancelUC, listUC, repo, quickCheckoutUC, listPaymentsUC)
}

func newSaleRouter(h *handlerbill.SaleHandler) *gin.Engine {
	r := gin.New()
	api := r.Group("/api/v1")
	h.RegisterRoutes(api)
	return r
}

func tenantHeader(id uuid.UUID) string { return id.String() }

// ----- tests -----

// TestSaleHandler_CreateDraft_Returns201 verifies a valid create request returns 201.
func TestSaleHandler_CreateDraft_Returns201(t *testing.T) {
	repo := newSHMockBillRepo()
	h := buildSaleHandlerForTest(repo, &shMockStockUC{}, &shMockPaymentRecorder{}, &shMockPaymentRepo{})
	r := newSaleRouter(h)

	body := map[string]any{
		"items": []map[string]any{
			{"product_id": shProductID.String(), "warehouse_id": shWarehouseID.String(), "qty": "5", "unit_price": "20", "line_no": 1},
		},
	}
	b, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/sale-bills", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", tenantHeader(shTenantID))
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
}

// TestSaleHandler_Approve_Returns200 verifies approve on a draft bill returns 200.
func TestSaleHandler_Approve_Returns200(t *testing.T) {
	repo := newSHMockBillRepo()
	now := time.Now()
	billID := uuid.New()
	repo.heads[billID] = &domain.BillHead{
		ID:          billID,
		TenantID:    shTenantID,
		Status:      domain.StatusDraft,
		BillType:    domain.BillTypeSale,
		TotalAmount: decimal.NewFromFloat(100),
		CreatedAt:   now,
	}
	repo.items[billID] = []*domain.BillItem{
		{ID: uuid.New(), TenantID: shTenantID, HeadID: billID, ProductID: shProductID, Qty: decimal.NewFromFloat(5), UnitPrice: decimal.NewFromFloat(20)},
	}

	h := buildSaleHandlerForTest(repo, &shMockStockUC{}, &shMockPaymentRecorder{}, &shMockPaymentRepo{})
	r := newSaleRouter(h)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/sale-bills/"+billID.String()+"/approve", nil)
	req.Header.Set("X-Tenant-ID", tenantHeader(shTenantID))
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

// TestSaleHandler_Approve_InsufficientStock_Returns422 verifies insufficient stock returns 422.
func TestSaleHandler_Approve_InsufficientStock_Returns422(t *testing.T) {
	repo := newSHMockBillRepo()
	now := time.Now()
	billID := uuid.New()
	repo.heads[billID] = &domain.BillHead{
		ID:          billID,
		TenantID:    shTenantID,
		Status:      domain.StatusDraft,
		BillType:    domain.BillTypeSale,
		TotalAmount: decimal.NewFromFloat(100),
		CreatedAt:   now,
	}
	repo.items[billID] = []*domain.BillItem{
		{ID: uuid.New(), TenantID: shTenantID, HeadID: billID, ProductID: shProductID, Qty: decimal.NewFromFloat(5)},
	}

	stockUC := &shMockStockUC{
		failErr: &appstock.InsufficientStockError{Available: decimal.Zero, Requested: decimal.NewFromFloat(5)},
	}
	h := buildSaleHandlerForTest(repo, stockUC, &shMockPaymentRecorder{}, &shMockPaymentRepo{})
	r := newSaleRouter(h)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/sale-bills/"+billID.String()+"/approve", nil)
	req.Header.Set("X-Tenant-ID", tenantHeader(shTenantID))
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422; body: %s", w.Code, w.Body.String())
	}
}

// TestSaleHandler_QuickCheckout_Returns201 verifies the POS quick checkout endpoint.
func TestSaleHandler_QuickCheckout_Returns201(t *testing.T) {
	repo := newSHMockBillRepo()
	h := buildSaleHandlerForTest(repo, &shMockStockUC{}, &shMockPaymentRecorder{}, &shMockPaymentRepo{})
	r := newSaleRouter(h)

	body := map[string]any{
		"payment_method": "cash",
		"paid_amount":    "100",
		"items": []map[string]any{
			{"product_id": shProductID.String(), "warehouse_id": shWarehouseID.String(), "qty": "5", "unit_price": "20", "line_no": 1},
		},
	}
	b, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/sale-bills/quick-checkout", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", tenantHeader(shTenantID))
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
}

// TestSaleHandler_List_Returns200 verifies the list endpoint returns 200.
func TestSaleHandler_List_Returns200(t *testing.T) {
	repo := newSHMockBillRepo()
	repo.listResult = []domain.BillHead{{ID: uuid.New(), TenantID: shTenantID}}
	repo.listTotal = 1
	h := buildSaleHandlerForTest(repo, &shMockStockUC{}, &shMockPaymentRecorder{}, &shMockPaymentRepo{})
	r := newSaleRouter(h)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/sale-bills", nil)
	req.Header.Set("X-Tenant-ID", tenantHeader(shTenantID))
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

// TestSaleHandler_GetByID_Returns200WithPayments verifies the detail endpoint includes payments.
func TestSaleHandler_GetByID_Returns200WithPayments(t *testing.T) {
	repo := newSHMockBillRepo()
	now := time.Now()
	billID := uuid.New()
	repo.heads[billID] = &domain.BillHead{
		ID:          billID,
		TenantID:    shTenantID,
		Status:      domain.StatusApproved,
		TotalAmount: decimal.NewFromFloat(100),
		CreatedAt:   now,
	}
	repo.items[billID] = []*domain.BillItem{}

	payRepo := &shMockPaymentRepo{
		payments: []*domainpayment.Payment{
			{ID: uuid.New(), BillID: billID, Amount: decimal.NewFromFloat(100), PayType: domainpayment.PayTypeCash},
		},
	}
	h := buildSaleHandlerForTest(repo, &shMockStockUC{}, &shMockPaymentRecorder{}, payRepo)
	r := newSaleRouter(h)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/sale-bills/"+billID.String(), nil)
	req.Header.Set("X-Tenant-ID", tenantHeader(shTenantID))
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if _, ok := resp["payments"]; !ok {
		t.Error("response missing 'payments' field")
	}
	if _, ok := resp["receivable_amount"]; !ok {
		t.Error("response missing 'receivable_amount' field")
	}
}
