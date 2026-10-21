package bill_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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
	heads          map[uuid.UUID]*domain.BillHead
	items          map[uuid.UUID][]*domain.BillItem
	listResult     []domain.BillHead
	listTotal      int64
	updatePaidErr  error
	noDefaultWH    bool // true: tenant has no default warehouse
	missingPartner map[uuid.UUID]struct{}
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

func (m *shMockBillRepo) SummarizeBills(_ context.Context, _ appbill.BillListFilter) (appbill.BillListSummary, error) {
	return appbill.BillListSummary{}, nil
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

func (m *shMockBillRepo) ProductExists(_ context.Context, _, _ uuid.UUID) (bool, error) {
	return true, nil
}

func (m *shMockBillRepo) WarehouseExists(_ context.Context, _, _ uuid.UUID) (bool, error) {
	return true, nil
}

func (m *shMockBillRepo) PartnerExists(_ context.Context, _, id uuid.UUID) (bool, error) {
	_, missing := m.missingPartner[id]
	return !missing, nil
}

func (m *shMockBillRepo) DefaultWarehouseID(_ context.Context, _ uuid.UUID) (uuid.UUID, bool, error) {
	if m.noDefaultWH {
		return uuid.Nil, false, nil
	}
	return shWarehouseID, true, nil
}

func (m *shMockBillRepo) SetBillWarehouse(_ context.Context, _ *sql.Tx, _, billID, warehouseID uuid.UUID) error {
	if h, ok := m.heads[billID]; ok {
		h.WarehouseID = &warehouseID
	}
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

type shMockUnitRepo struct{ err error }

func (m *shMockUnitRepo) GetConversionFactor(_ context.Context, _, _ uuid.UUID) (decimal.Decimal, error) {
	if m.err != nil {
		return decimal.Zero, m.err
	}
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
	return buildSaleHandlerWithUnits(repo, stockUC, &shMockUnitRepo{}, payRec, payRepo)
}

func buildSaleHandlerWithUnits(repo *shMockBillRepo, stockUC *shMockStockUC, unitRepo *shMockUnitRepo, payRec *shMockPaymentRecorder, payRepo *shMockPaymentRepo) *handlerbill.SaleHandler {
	createUC := appbill.NewCreateSaleUseCase(repo)
	approveUC := appbill.NewApproveSaleUseCase(repo, stockUC, unitRepo, payRec)
	cancelUC := appbill.NewCancelPurchaseUseCase(repo)
	listPaymentsUC := apppayment.NewListPaymentsUseCase(payRepo)
	quickCheckoutUC := appbill.NewQuickCheckoutUseCase(repo, approveUC)
	// ListPurchasesUseCase is used for listing by type; we pass a fake one
	listUC := appbill.NewListPurchasesUseCase(repo)
	return handlerbill.NewSaleHandler(createUC, approveUC, cancelUC, listUC, repo, quickCheckoutUC, listPaymentsUC)
}

func newSaleRouter(h *handlerbill.SaleHandler) *gin.Engine {
	r := gin.New()
	r.Use(injectTenantFromHeader)
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

// TestSaleHandler_Approve_InsufficientStock_ResponseHasDetailsArray verifies that the 422 body
// contains a "details" array (not legacy scalar fields) so the FE can highlight all short SKUs.
func TestSaleHandler_Approve_InsufficientStock_ResponseHasDetailsArray(t *testing.T) {
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
		failErr: &appstock.InsufficientStockError{
			ProductID: shProductID,
			Available: decimal.Zero,
			Requested: decimal.NewFromFloat(5),
		},
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

	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "insufficient_stock" {
		t.Errorf("error field = %v, want insufficient_stock", body["error"])
	}
	details, ok := body["details"].([]any)
	if !ok {
		t.Fatalf("details field missing or not array; body = %v", body)
	}
	if len(details) != 1 {
		t.Errorf("details length = %d, want 1", len(details))
	}
	if len(details) > 0 {
		entry, ok := details[0].(map[string]any)
		if !ok {
			t.Fatalf("details[0] is not object: %T", details[0])
		}
		if entry["product_id"] != shProductID.String() {
			t.Errorf("details[0].product_id = %v, want %s", entry["product_id"], shProductID)
		}
		if _, ok := entry["available_qty"]; !ok {
			t.Error("details[0].available_qty missing")
		}
		if _, ok := entry["requested_qty"]; !ok {
			t.Error("details[0].requested_qty missing")
		}
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
		BillType:    domain.BillTypeSale,
		SubType:     domain.BillSubTypeSale,
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

func seedSHDraftSale(repo *shMockBillRepo, warehouse *uuid.UUID, unitID *uuid.UUID) uuid.UUID {
	billID := uuid.New()
	repo.heads[billID] = &domain.BillHead{
		ID:          billID,
		TenantID:    shTenantID,
		Status:      domain.StatusDraft,
		BillType:    domain.BillTypeSale,
		WarehouseID: warehouse,
		TotalAmount: decimal.NewFromFloat(100),
		CreatedAt:   time.Now(),
	}
	repo.items[billID] = []*domain.BillItem{
		{ID: uuid.New(), TenantID: shTenantID, HeadID: billID, ProductID: shProductID, UnitID: unitID, Qty: decimal.NewFromFloat(5), UnitPrice: decimal.NewFromFloat(20)},
	}
	return billID
}

func approveSHSale(t *testing.T, h *handlerbill.SaleHandler, billID uuid.UUID) (int, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/sale-bills/"+billID.String()+"/approve", nil)
	req.Header.Set("X-Tenant-ID", tenantHeader(shTenantID))
	newSaleRouter(h).ServeHTTP(w, req)
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	return w.Code, resp
}

// 没选仓库且没有默认仓：审核必须是可读的 422，不能 500，也不能动库存。
func TestSaleHandler_Approve_NoWarehouseNoDefault_Returns422WarehouseRequired(t *testing.T) {
	repo := newSHMockBillRepo()
	repo.noDefaultWH = true
	billID := seedSHDraftSale(repo, nil, nil)
	stockUC := &shMockStockUC{}

	code, resp := approveSHSale(t, buildSaleHandlerForTest(repo, stockUC, &shMockPaymentRecorder{}, &shMockPaymentRepo{}), billID)

	if code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", code)
	}
	if resp["error"] != "warehouse_required" {
		t.Errorf("error = %v, want warehouse_required", resp["error"])
	}
	if stockUC.calls != 0 {
		t.Errorf("stock calls = %d, want 0", stockUC.calls)
	}
	if repo.heads[billID].Status != domain.StatusDraft {
		t.Errorf("status changed to %d, want draft", repo.heads[billID].Status)
	}
}

// 没选仓库但有默认仓：审核成功，并把默认仓写回单据。
func TestSaleHandler_Approve_NoWarehouseWithDefault_Returns200AndWritesBack(t *testing.T) {
	repo := newSHMockBillRepo()
	billID := seedSHDraftSale(repo, nil, nil)

	code, _ := approveSHSale(t, buildSaleHandlerForTest(repo, &shMockStockUC{}, &shMockPaymentRecorder{}, &shMockPaymentRepo{}), billID)

	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if wh := repo.heads[billID].WarehouseID; wh == nil || *wh != shWarehouseID {
		t.Errorf("bill warehouse = %v, want %s", wh, shWarehouseID)
	}
}

// 单位没有换算关系：422 invalid_unit_for_product，不能 500。
func TestSaleHandler_Approve_InvalidUnit_Returns422(t *testing.T) {
	repo := newSHMockBillRepo()
	wh := shWarehouseID
	unit := uuid.New()
	billID := seedSHDraftSale(repo, &wh, &unit)
	units := &shMockUnitRepo{err: appbill.ErrInvalidUnitForProduct}

	code, resp := approveSHSale(t, buildSaleHandlerWithUnits(repo, &shMockStockUC{}, units, &shMockPaymentRecorder{}, &shMockPaymentRepo{}), billID)

	if code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", code)
	}
	if resp["error"] != "invalid_unit_for_product" {
		t.Errorf("error = %v, want invalid_unit_for_product", resp["error"])
	}
}

// ----- R2: paid amount / partner guards -----

func seedSaleDraftForApprove(repo *shMockBillRepo, total int64) uuid.UUID {
	billID := uuid.New()
	repo.heads[billID] = &domain.BillHead{
		ID: billID, TenantID: shTenantID, Status: domain.StatusDraft, BillType: domain.BillTypeSale,
		TotalAmount: decimal.NewFromInt(total), CreatedAt: time.Now(),
	}
	repo.items[billID] = []*domain.BillItem{
		{ID: uuid.New(), TenantID: shTenantID, HeadID: billID, ProductID: shProductID, Qty: decimal.NewFromFloat(1), UnitPrice: decimal.NewFromInt(total)},
	}
	return billID
}

func postApprove(r *gin.Engine, billID uuid.UUID, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/sale-bills/"+billID.String()+"/approve", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", tenantHeader(shTenantID))
	r.ServeHTTP(w, req)
	return w
}

func TestSaleHandler_Approve_PaidExceedsTotal_Returns422(t *testing.T) {
	repo := newSHMockBillRepo()
	stockUC := &shMockStockUC{}
	payRec := &shMockPaymentRecorder{}
	billID := seedSaleDraftForApprove(repo, 20)
	r := newSaleRouter(buildSaleHandlerForTest(repo, stockUC, payRec, &shMockPaymentRepo{}))

	w := postApprove(r, billID, `{"paid_amount":"999"}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "paid_exceeds_total" {
		t.Errorf("error = %v, want paid_exceeds_total", resp["error"])
	}
	if resp["message"] != "实收金额 999 超过单据合计 20" {
		t.Errorf("message = %v", resp["message"])
	}
	if stockUC.calls != 0 || len(payRec.recorded) != 0 {
		t.Errorf("no stock/payment may be written: stock=%d pay=%d", stockUC.calls, len(payRec.recorded))
	}
}

func TestSaleHandler_Approve_RepeatWithPayment_Returns409(t *testing.T) {
	repo := newSHMockBillRepo()
	billID := seedSaleDraftForApprove(repo, 20)
	r := newSaleRouter(buildSaleHandlerForTest(repo, &shMockStockUC{}, &shMockPaymentRecorder{}, &shMockPaymentRepo{}))

	if w := postApprove(r, billID, ``); w.Code != http.StatusOK {
		t.Fatalf("first approve status = %d; body: %s", w.Code, w.Body.String())
	}
	w := postApprove(r, billID, `{"paid_amount":"10"}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("repeat with payment status = %d, want 409; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "bill_already_approved" {
		t.Errorf("error = %v, want bill_already_approved", resp["error"])
	}
	if w := postApprove(r, billID, `{"paid_amount":"0"}`); w.Code != http.StatusOK {
		t.Errorf("repeat with zero payment status = %d, want 200", w.Code)
	}
}

func TestSaleHandler_Approve_BadPaidAmount_Returns400(t *testing.T) {
	repo := newSHMockBillRepo()
	billID := seedSaleDraftForApprove(repo, 20)
	r := newSaleRouter(buildSaleHandlerForTest(repo, &shMockStockUC{}, &shMockPaymentRecorder{}, &shMockPaymentRepo{}))

	for _, body := range []string{`{"paid_amount":"-5"}`, `{"paid_amount":"abc"}`, `{"paid_amount":`} {
		if w := postApprove(r, billID, body); w.Code != http.StatusBadRequest {
			t.Errorf("body %s: status = %d, want 400; resp: %s", body, w.Code, w.Body.String())
		}
	}
}

func postQuickCheckout(r *gin.Engine, body map[string]any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/sale-bills/quick-checkout", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", tenantHeader(shTenantID))
	r.ServeHTTP(w, req)
	return w
}

func quickCheckoutBody(paid string, extra map[string]any) map[string]any {
	body := map[string]any{
		"payment_method": "cash",
		"paid_amount":    paid,
		"items": []map[string]any{
			{"product_id": shProductID.String(), "warehouse_id": shWarehouseID.String(), "qty": "4", "unit_price": "20", "line_no": 1},
		},
	}
	for k, v := range extra {
		body[k] = v
	}
	return body
}

func TestSaleHandler_QuickCheckout_Change_ReturnsChangeAmount(t *testing.T) {
	repo := newSHMockBillRepo()
	payRec := &shMockPaymentRecorder{}
	r := newSaleRouter(buildSaleHandlerForTest(repo, &shMockStockUC{}, payRec, &shMockPaymentRepo{}))

	w := postQuickCheckout(r, quickCheckoutBody("100", nil))
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["change_amount"] != "20" {
		t.Errorf("change_amount = %v, want \"20\"", resp["change_amount"])
	}
	if resp["receivable_amount"] != "0" {
		t.Errorf("receivable_amount = %v, want \"0\"", resp["receivable_amount"])
	}
}

func TestSaleHandler_QuickCheckout_PartnerID(t *testing.T) {
	repo := newSHMockBillRepo()
	r := newSaleRouter(buildSaleHandlerForTest(repo, &shMockStockUC{}, &shMockPaymentRecorder{}, &shMockPaymentRepo{}))

	partnerID := uuid.New()
	w := postQuickCheckout(r, quickCheckoutBody("10", map[string]any{"partner_id": partnerID.String()}))
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	billID, _ := uuid.Parse(resp["bill_id"].(string))
	if p := repo.heads[billID].PartnerID; p == nil || *p != partnerID {
		t.Errorf("bill partner_id = %v, want %s", p, partnerID)
	}

	if w := postQuickCheckout(r, quickCheckoutBody("10", map[string]any{"partner_id": "not-a-uuid"})); w.Code != http.StatusBadRequest {
		t.Errorf("malformed partner_id status = %d, want 400", w.Code)
	}

	missing := uuid.New()
	repo.missingPartner = map[uuid.UUID]struct{}{missing: {}}
	if w := postQuickCheckout(r, quickCheckoutBody("10", map[string]any{"partner_id": missing.String()})); w.Code != http.StatusBadRequest {
		t.Errorf("unknown partner status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
}

func TestSaleHandler_QuickCheckout_NilPartnerAndBadPaidAmount(t *testing.T) {
	repo := newSHMockBillRepo()
	r := newSaleRouter(buildSaleHandlerForTest(repo, &shMockStockUC{}, &shMockPaymentRecorder{}, &shMockPaymentRepo{}))

	// The zero UUID is treated as "no partner": no FK write, no 4xx/5xx.
	w := postQuickCheckout(r, quickCheckoutBody("10", map[string]any{"partner_id": uuid.Nil.String()}))
	if w.Code != http.StatusCreated {
		t.Fatalf("nil partner status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	billID, _ := uuid.Parse(resp["bill_id"].(string))
	if p := repo.heads[billID].PartnerID; p != nil {
		t.Errorf("bill partner_id = %v, want nil", p)
	}

	for _, bad := range []string{"abc", "-5"} {
		if w := postQuickCheckout(r, quickCheckoutBody(bad, nil)); w.Code != http.StatusBadRequest {
			t.Errorf("paid_amount %q status = %d, want 400; body: %s", bad, w.Code, w.Body.String())
		}
	}
}

func TestSaleHandler_QuickCheckout_CreditWithoutPartner_Returns400(t *testing.T) {
	repo := newSHMockBillRepo()
	r := newSaleRouter(buildSaleHandlerForTest(repo, &shMockStockUC{}, &shMockPaymentRecorder{}, &shMockPaymentRepo{}))

	for _, extra := range []map[string]any{
		{"payment_method": "credit", "customer_name": "张三"},
		{"payment_method": "credit", "partner_id": uuid.Nil.String()},
	} {
		w := postQuickCheckout(r, quickCheckoutBody("0", extra))
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "赊账必须选择客户") {
			t.Errorf("%v: status = %d, want 400; body: %s", extra, w.Code, w.Body.String())
		}
	}
	extra := map[string]any{"payment_method": "credit", "partner_id": uuid.New().String()}
	if w := postQuickCheckout(r, quickCheckoutBody("0", extra)); w.Code != http.StatusCreated {
		t.Errorf("credit with partner status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
}

func TestSaleHandler_QuickCheckout_Remark_KeptOnBill(t *testing.T) {
	repo := newSHMockBillRepo()
	r := newSaleRouter(buildSaleHandlerForTest(repo, &shMockStockUC{}, &shMockPaymentRecorder{}, &shMockPaymentRepo{}))

	remarkOf := func(extra map[string]any) string {
		w := postQuickCheckout(r, quickCheckoutBody("80", extra))
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d; body: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		id, _ := uuid.Parse(resp["bill_id"].(string))
		return repo.heads[id].Remark
	}
	if got := remarkOf(map[string]any{"remark": "明天送货"}); got != "明天送货" {
		t.Errorf("remark = %q", got)
	}
	if got := remarkOf(map[string]any{"partner_id": uuid.New().String(), "remark": "含税"}); got != "含税" {
		t.Errorf("credit-style remark = %q", got)
	}
	if got := remarkOf(map[string]any{"customer_name": "微信流水尾号 1234", "remark": "送货上门"}); got != "微信流水尾号 1234；送货上门" {
		t.Errorf("merged remark = %q", got)
	}
}

// TestSaleHandler_List_InvalidParams_Returns400 verifies bad filters are rejected, not ignored.
func TestSaleHandler_List_InvalidParams_Returns400(t *testing.T) {
	repo := newSHMockBillRepo()
	h := buildSaleHandlerForTest(repo, &shMockStockUC{}, &shMockPaymentRecorder{}, &shMockPaymentRepo{})
	r := newSaleRouter(h)
	for _, qs := range []string{"status=abc", "date_to=bad", "date_from=2026-11-02&date_to=2026-11-01", "payment_status=zzz"} {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/api/v1/sale-bills?"+qs, nil)
		req.Header.Set("X-Tenant-ID", tenantHeader(shTenantID))
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400; body: %s", qs, w.Code, w.Body.String())
		}
	}
}

// TestSaleHandler_List_KeepsReceivableAndAddsSettlement verifies the sale list fields.
func TestSaleHandler_List_KeepsReceivableAndAddsSettlement(t *testing.T) {
	repo := newSHMockBillRepo()
	repo.listResult = []domain.BillHead{{
		ID: uuid.New(), TenantID: shTenantID, Status: domain.StatusApproved, PartnerName: "老王",
		TotalAmount: decimal.NewFromInt(100), PaidAmount: decimal.NewFromInt(100),
	}}
	repo.listTotal = 1
	h := buildSaleHandlerForTest(repo, &shMockStockUC{}, &shMockPaymentRecorder{}, &shMockPaymentRepo{})
	r := newSaleRouter(h)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/sale-bills?status=2", nil)
	req.Header.Set("X-Tenant-ID", tenantHeader(shTenantID))
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Items   []map[string]any `json:"items"`
		Summary map[string]any   `json:"summary"`
	}
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Items) != 1 {
		t.Fatalf("items = %d", len(resp.Items))
	}
	it := resp.Items[0]
	if it["partner_name"] != "老王" || it["payment_status"] != "paid" || it["unpaid_amount"] != "0" || it["receivable_amount"] != "0" {
		t.Errorf("item = %v", it)
	}
	if resp.Summary == nil {
		t.Error("summary missing")
	}
}

// 收银台一键结账遇到没有仓库可用：同样是可读的 422，不能 500。
func TestSaleHandler_QuickCheckout_NoWarehouseNoDefault_Returns422(t *testing.T) {
	repo := newSHMockBillRepo()
	repo.noDefaultWH = true
	h := buildSaleHandlerForTest(repo, &shMockStockUC{}, &shMockPaymentRecorder{}, &shMockPaymentRepo{})
	b, _ := json.Marshal(map[string]any{
		"payment_method": "cash",
		"paid_amount":    "100",
		"items":          []map[string]any{{"product_id": shProductID.String(), "qty": "5", "unit_price": "20", "line_no": 1}},
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/sale-bills/quick-checkout", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", tenantHeader(shTenantID))
	newSaleRouter(h).ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "warehouse_required") {
		t.Errorf("body = %s, want warehouse_required", w.Body.String())
	}
}

// 收银台一键结账选了没有换算关系的单位：中文 422，不能 500。
func TestSaleHandler_QuickCheckout_InvalidUnit_Returns422(t *testing.T) {
	repo := newSHMockBillRepo()
	units := &shMockUnitRepo{err: appbill.ErrInvalidUnitForProduct}
	h := buildSaleHandlerWithUnits(repo, &shMockStockUC{}, units, &shMockPaymentRecorder{}, &shMockPaymentRepo{})
	b, _ := json.Marshal(map[string]any{
		"payment_method": "cash",
		"paid_amount":    "100",
		"items": []map[string]any{{
			"product_id": shProductID.String(), "warehouse_id": shWarehouseID.String(),
			"unit_id": uuid.New().String(), "qty": "5", "unit_price": "20", "line_no": 1,
		}},
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/sale-bills/quick-checkout", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", tenantHeader(shTenantID))
	newSaleRouter(h).ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "invalid_unit_for_product") || !strings.Contains(w.Body.String(), "换算关系") {
		t.Errorf("body = %s, want invalid_unit_for_product with Chinese message", w.Body.String())
	}
}

func TestSaleHandler_QuickCheckout_Discount_TotalMatchesPayment(t *testing.T) {
	repo := newSHMockBillRepo()
	r := newSaleRouter(buildSaleHandlerForTest(repo, &shMockStockUC{}, &shMockPaymentRecorder{}, &shMockPaymentRepo{}))

	// line total 4 x 20 = 80, discount 5 -> bill total 75, paid 75 -> nothing receivable.
	w := postQuickCheckout(r, quickCheckoutBody("75", map[string]any{"discount_amount": "5"}))
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["total_amount"] != "75" {
		t.Errorf("total_amount = %v, want \"75\"", resp["total_amount"])
	}
	if resp["receivable_amount"] != "0" {
		t.Errorf("receivable_amount = %v, want \"0\"", resp["receivable_amount"])
	}

	for _, d := range []string{"-1", "abc", "81"} {
		if w := postQuickCheckout(r, quickCheckoutBody("0", map[string]any{"discount_amount": d})); w.Code != http.StatusBadRequest {
			t.Errorf("discount %q: status = %d, want 400; body: %s", d, w.Code, w.Body.String())
		}
	}
}

// ----- POS shift summary -----

// shPOSRepo is a bill repo that also serves the POS summary read port.
type shPOSRepo struct {
	*shMockBillRepo
	out appbill.POSSummary
	got appbill.POSSummaryRange
}

func (m *shPOSRepo) POSSummary(_ context.Context, q appbill.POSSummaryRange) (appbill.POSSummary, error) {
	m.got = q
	return m.out, nil
}

func getPOSSummary(h *handlerbill.SaleHandler, query string, sub ...string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/sale-bills/pos-summary"+query, nil)
	req.Header.Set("X-Tenant-ID", tenantHeader(shTenantID))
	if len(sub) > 0 {
		req.Header.Set("X-Test-Sub", sub[0])
	}
	newSaleRouter(h).ServeHTTP(w, req)
	return w
}

func buildPOSHandler(repo appbill.BillRepo) *handlerbill.SaleHandler {
	return handlerbill.NewSaleHandler(nil, nil, nil, nil, repo, nil, nil)
}

func TestSaleHandler_POSSummary_OK(t *testing.T) {
	repo := &shPOSRepo{shMockBillRepo: newSHMockBillRepo(), out: appbill.POSSummary{
		Count: 25, TotalAmount: decimal.RequireFromString("1234.5"), PaidAmount: decimal.RequireFromString("1000"),
		CreditAmount: decimal.RequireFromString("234.5"),
		ByPayType: []appbill.POSPayTypeTotal{
			{PayType: "cash", Count: 15, Amount: decimal.RequireFromString("700")},
			{PayType: "wechat", Count: 10, Amount: decimal.RequireFromString("300")},
		},
		ReturnCount: 2, ReturnAmount: decimal.RequireFromString("88"), RefundCount: 3, RefundAmount: decimal.RequireFromString("12.5"),
	}}
	w := getPOSSummary(buildPOSHandler(repo), "?date=2026-10-10")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", w.Code, w.Body.String())
	}
	var got struct {
		Date         string `json:"date"`
		Count        int64  `json:"count"`
		TotalAmount  string `json:"total_amount"`
		PaidAmount   string `json:"paid_amount"`
		CreditAmount string `json:"credit_amount"`
		ReturnCount  int64  `json:"return_count"`
		RefundCount  int64  `json:"refund_count"`
		RefundAmount string `json:"refund_amount"`
		ReturnAmount string `json:"return_amount"`
		ByPayType    []struct {
			PayType string `json:"pay_type"`
			Count   int64  `json:"count"`
			Amount  string `json:"amount"`
		} `json:"by_pay_type"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Date != "2026-10-10" || got.Count != 25 || got.TotalAmount != "1234.50" || got.PaidAmount != "1000.00" ||
		got.CreditAmount != "234.50" || got.ReturnCount != 2 || got.RefundCount != 3 || got.RefundAmount != "12.50" || got.ReturnAmount != "88.00" ||
		len(got.ByPayType) != 2 || got.ByPayType[0].PayType != "cash" || got.ByPayType[0].Amount != "700.00" {
		t.Errorf("body = %s", w.Body.String())
	}
	// Beijing midnight is 16:00 UTC of the previous day.
	if repo.got.From.UTC().Format(time.RFC3339) != "2026-10-09T16:00:00Z" || repo.got.To.Sub(repo.got.From) != 24*time.Hour {
		t.Errorf("window = %v .. %v", repo.got.From, repo.got.To)
	}
}

func TestSaleHandler_POSSummary_MineNarrowsToSignedInPerson(t *testing.T) {
	repo := &shPOSRepo{shMockBillRepo: newSHMockBillRepo()}
	me := uuid.New()
	if w := getPOSSummary(buildPOSHandler(repo), "?mine=1", me.String()); w.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", w.Code, w.Body.String())
	}
	if repo.got.CreatorID == nil || *repo.got.CreatorID != me {
		t.Errorf("creator = %v, want %v", repo.got.CreatorID, me)
	}
	if w := getPOSSummary(buildPOSHandler(repo), "", me.String()); w.Code != http.StatusOK || repo.got.CreatorID != nil {
		t.Errorf("without mine the whole shop is summed: status %d creator %v", w.Code, repo.got.CreatorID)
	}
}

func TestSaleHandler_POSSummary_MineNeedsIdentity(t *testing.T) {
	repo := &shPOSRepo{shMockBillRepo: newSHMockBillRepo()}
	if w := getPOSSummary(buildPOSHandler(repo), "?mine=1"); w.Code != http.StatusBadRequest {
		t.Errorf("no identity: status = %d, want 400", w.Code)
	}
	if w := getPOSSummary(buildPOSHandler(repo), "?mine=yes", uuid.New().String()); w.Code != http.StatusBadRequest {
		t.Errorf("bad mine: status = %d, want 400", w.Code)
	}
}

func getPOSSummaryAs(h *handlerbill.SaleHandler, role, query string, sub ...string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/sale-bills/pos-summary"+query, nil)
	req.Header.Set("X-Tenant-ID", tenantHeader(shTenantID))
	req.Header.Set("X-Test-Role", role)
	if len(sub) > 0 {
		req.Header.Set("X-Test-Sub", sub[0])
	}
	newSaleRouter(h).ServeHTTP(w, req)
	return w
}

// A cashier cannot see the whole shop's takings: without mine, or with mine=0, the
// shift is still narrowed to their own bills.
func TestSaleHandler_POSSummary_CashierAlwaysOwn(t *testing.T) {
	me := uuid.New()
	for _, q := range []string{"", "?mine=0", "?mine=1"} {
		repo := &shPOSRepo{shMockBillRepo: newSHMockBillRepo()}
		if w := getPOSSummaryAs(buildPOSHandler(repo), "cashier", q, me.String()); w.Code != http.StatusOK {
			t.Fatalf("query %q: status = %d; body: %s", q, w.Code, w.Body.String())
		}
		if repo.got.CreatorID == nil || *repo.got.CreatorID != me {
			t.Errorf("query %q: creator = %v, want %v", q, repo.got.CreatorID, me)
		}
	}
}

// Whoever holds bill.return (boss, manager) still sees the whole shop by default.
func TestSaleHandler_POSSummary_ManagerKeepsShopWide(t *testing.T) {
	repo := &shPOSRepo{shMockBillRepo: newSHMockBillRepo()}
	if w := getPOSSummaryAs(buildPOSHandler(repo), "manager", "", uuid.New().String()); w.Code != http.StatusOK || repo.got.CreatorID != nil {
		t.Errorf("status %d creator %v, want 200 and nil", w.Code, repo.got.CreatorID)
	}
}

func TestSaleHandler_POSSummary_CashierNeedsIdentity(t *testing.T) {
	repo := &shPOSRepo{shMockBillRepo: newSHMockBillRepo()}
	w := getPOSSummaryAs(buildPOSHandler(repo), "cashier", "")
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "无法识别当前用户") {
		t.Errorf("status = %d body %s, want 400 identity message", w.Code, w.Body.String())
	}
}

func TestSaleHandler_POSSummary_BadMineValue(t *testing.T) {
	repo := &shPOSRepo{shMockBillRepo: newSHMockBillRepo()}
	for _, role := range []string{"cashier", "manager"} {
		w := getPOSSummaryAs(buildPOSHandler(repo), role, "?mine=x", uuid.New().String())
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "参数不正确") {
			t.Errorf("%s: status = %d body %s, want 400 bad parameter", role, w.Code, w.Body.String())
		}
	}
	w := getPOSSummary(buildPOSHandler(repo), "?mine=1")
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "无法识别当前用户") {
		t.Errorf("no identity: status = %d body %s", w.Code, w.Body.String())
	}
}

func TestSaleHandler_POSSummary_BadDate(t *testing.T) {
	repo := &shPOSRepo{shMockBillRepo: newSHMockBillRepo()}
	if w := getPOSSummary(buildPOSHandler(repo), "?date=10-10"); w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestSaleHandler_POSSummary_NotWired(t *testing.T) {
	if w := getPOSSummary(buildPOSHandler(newSHMockBillRepo()), ""); w.Code != http.StatusNotImplemented {
		t.Errorf("status = %d, want 501", w.Code)
	}
}

func TestSaleHandler_POSSummary_RouteBeatsIDParam(t *testing.T) {
	repo := &shPOSRepo{shMockBillRepo: newSHMockBillRepo()}
	if w := getPOSSummary(buildPOSHandler(repo), ""); w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (not parsed as a bill id)", w.Code)
	}
}

// A purchase bill must not be readable through the sale-bill detail route, or a
// clerk or cashier could read purchase prices from it.
func TestSaleHandler_GetByID_PurchaseBill_Returns404(t *testing.T) {
	repo := newSHMockBillRepo()
	billID := uuid.New()
	repo.heads[billID] = &domain.BillHead{
		ID:          billID,
		TenantID:    shTenantID,
		BillType:    domain.BillTypePurchase,
		SubType:     domain.BillSubTypePurchase,
		Status:      domain.StatusApproved,
		TotalAmount: decimal.NewFromFloat(100),
		CreatedAt:   time.Now(),
	}
	repo.items[billID] = []*domain.BillItem{{ID: uuid.New(), HeadID: billID, UnitPrice: decimal.NewFromFloat(1.1111)}}
	h := buildSaleHandlerForTest(repo, &shMockStockUC{}, &shMockPaymentRecorder{}, &shMockPaymentRepo{})
	r := newSaleRouter(h)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/sale-bills/"+billID.String(), nil)
	req.Header.Set("X-Tenant-ID", tenantHeader(shTenantID))
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "1.1111") {
		t.Error("purchase price leaked in response")
	}
}

func getSaleBillByHead(t *testing.T, head *domain.BillHead, price float64) *httptest.ResponseRecorder {
	t.Helper()
	repo := newSHMockBillRepo()
	head.ID = uuid.New()
	head.TenantID = shTenantID
	head.Status = domain.StatusApproved
	head.TotalAmount = decimal.NewFromFloat(price)
	head.CreatedAt = time.Now()
	repo.heads[head.ID] = head
	repo.items[head.ID] = []*domain.BillItem{{ID: uuid.New(), HeadID: head.ID, UnitPrice: decimal.NewFromFloat(price)}}
	h := buildSaleHandlerForTest(repo, &shMockStockUC{}, &shMockPaymentRecorder{}, &shMockPaymentRepo{})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/sale-bills/"+head.ID.String(), nil)
	req.Header.Set("X-Tenant-ID", tenantHeader(shTenantID))
	newSaleRouter(h).ServeHTTP(w, req)
	return w
}

// A supplier return is stored as an outbound bill (same bill_type as a sale)
// but carries purchase prices, so the sale-bill route must refuse it.
func TestSaleHandler_GetByID_PurchaseReturn_Returns404(t *testing.T) {
	w := getSaleBillByHead(t, &domain.BillHead{BillType: domain.BillTypeSale, SubType: domain.BillSubTypePurchaseReturn}, 7.77)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "7.77") {
		t.Error("purchase price leaked in response")
	}
}

// A sale return is an inbound bill; it must stay readable here so the return
// slip can be printed.
func TestSaleHandler_GetByID_SaleReturn_Returns200(t *testing.T) {
	w := getSaleBillByHead(t, &domain.BillHead{BillType: domain.BillTypePurchase, SubType: domain.BillSubTypeSaleReturn}, 20)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

// ----- POS shift summary: per-cashier split -----

type shCashierRepo struct {
	*shPOSRepo
	rows []appbill.POSCashierTotal
}

func (m *shCashierRepo) POSCashiers(_ context.Context, _ appbill.POSSummaryRange) ([]appbill.POSCashierTotal, error) {
	return m.rows, nil
}

type shNames map[uuid.UUID]string

func (n shNames) MemberNames(_ context.Context, _ uuid.UUID) (map[uuid.UUID]string, error) {
	return n, nil
}

func cashierFixture() (*shCashierRepo, handlerbill.MemberNamer, uuid.UUID, uuid.UUID, uuid.UUID) {
	a, b, gone := uuid.New(), uuid.New(), uuid.New()
	d := decimal.RequireFromString
	repo := &shCashierRepo{
		shPOSRepo: &shPOSRepo{shMockBillRepo: newSHMockBillRepo(), out: appbill.POSSummary{Count: 3, TotalAmount: d("600"), PaidAmount: d("500")}},
		rows: []appbill.POSCashierTotal{
			{CreatorID: a, Count: 1, TotalAmount: d("100"), PaidAmount: d("100"), RefundAmount: d("5")},
			{CreatorID: b, Count: 1, TotalAmount: d("300"), PaidAmount: d("300")},
			{CreatorID: gone, Count: 1, TotalAmount: d("200"), PaidAmount: d("100")},
		},
	}
	return repo, shNames{a: "老王", b: "小李"}, a, b, gone
}

type cashierResp struct {
	Cashiers []struct {
		Key          string `json:"key"`
		Name         string `json:"name"`
		Count        int64  `json:"count"`
		TotalAmount  string `json:"total_amount"`
		PaidAmount   string `json:"paid_amount"`
		RefundAmount string `json:"refund_amount"`
	} `json:"cashiers"`
}

func TestSaleHandler_POSSummary_OwnerSeesCashierSplit(t *testing.T) {
	repo, names, a, b, gone := cashierFixture()
	h := buildPOSHandler(repo).WithMemberNames(names)
	w := getPOSSummaryAs(h, "owner", "", uuid.New().String())
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", w.Code, w.Body.String())
	}
	var got cashierResp
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Cashiers) != 3 {
		t.Fatalf("cashiers = %s", w.Body.String())
	}
	sum := decimal.Zero
	byKey := map[string]string{}
	for _, c := range got.Cashiers {
		sum = sum.Add(decimal.RequireFromString(c.PaidAmount))
		byKey[c.Key] = c.Name
	}
	if !sum.Equal(decimal.RequireFromString("500")) {
		t.Errorf("paid sum = %s, want the shop's 500", sum)
	}
	if byKey[a.String()] != "老王" || byKey[b.String()] != "小李" || byKey[gone.String()] != "已移出的成员" {
		t.Errorf("names = %v", byKey)
	}
	if got.Cashiers[0].RefundAmount == "" {
		t.Errorf("refund_amount missing: %s", w.Body.String())
	}
}

func TestSaleHandler_POSSummary_OwnerPicksCashier(t *testing.T) {
	repo, names, a, _, _ := cashierFixture()
	h := buildPOSHandler(repo).WithMemberNames(names)
	w := getPOSSummaryAs(h, "owner", "?cashier="+a.String(), uuid.New().String())
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", w.Code, w.Body.String())
	}
	if repo.got.CreatorID == nil || *repo.got.CreatorID != a {
		t.Errorf("creator = %v, want %v", repo.got.CreatorID, a)
	}
	if strings.Contains(w.Body.String(), "cashiers") {
		t.Errorf("a personal shift has no split: %s", w.Body.String())
	}
}

func TestSaleHandler_POSSummary_CashierParamIgnoredWithoutReturnPerm(t *testing.T) {
	repo, names, _, b, _ := cashierFixture()
	h := buildPOSHandler(repo).WithMemberNames(names)
	me := uuid.New()
	w := getPOSSummaryAs(h, "cashier", "?cashier="+b.String(), me.String())
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", w.Code, w.Body.String())
	}
	if repo.got.CreatorID == nil || *repo.got.CreatorID != me {
		t.Errorf("creator = %v, want own %v", repo.got.CreatorID, me)
	}
	if strings.Contains(w.Body.String(), "cashiers") || strings.Contains(w.Body.String(), "老王") {
		t.Errorf("cashier role must not see the split: %s", w.Body.String())
	}
	// A malformed key is ignored for them too, not a 400.
	if w := getPOSSummaryAs(h, "cashier", "?cashier=zzz", me.String()); w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestSaleHandler_POSSummary_BadCashierKey(t *testing.T) {
	repo, names, _, _, _ := cashierFixture()
	h := buildPOSHandler(repo).WithMemberNames(names)
	w := getPOSSummaryAs(h, "owner", "?cashier=not-a-uuid", uuid.New().String())
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "收银员参数不正确") {
		t.Errorf("message not Chinese: %s", w.Body.String())
	}
}

func TestSaleHandler_POSSummary_NoSplitWhenNotWiredOrMine(t *testing.T) {
	repo, names, _, _, _ := cashierFixture()
	if w := getPOSSummaryAs(buildPOSHandler(repo), "owner", "", uuid.New().String()); w.Code != http.StatusOK || strings.Contains(w.Body.String(), "cashiers") {
		t.Errorf("names not wired: %d %s", w.Code, w.Body.String())
	}
	h := buildPOSHandler(repo).WithMemberNames(names)
	if w := getPOSSummaryAs(h, "owner", "?mine=1", uuid.New().String()); strings.Contains(w.Body.String(), "cashiers") {
		t.Errorf("mine=1 must not carry the split: %s", w.Body.String())
	}
}
