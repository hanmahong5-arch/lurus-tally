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
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
	"github.com/hanmahong5-arch/lurus-tally/internal/domain/team"
)

// injectTenantFromHeader is a test-only middleware that lets request fixtures
// keep using `req.Header.Set("X-Tenant-ID", …)` even though production handlers
// no longer trust that header. It mimics what AuthMiddleware does in prod:
// parse the value and place a uuid.UUID into the Gin context.
func injectTenantFromHeader(c *gin.Context) {
	if raw := c.GetHeader("X-Tenant-ID"); raw != "" {
		if id, err := uuid.Parse(raw); err == nil {
			c.Set(middleware.CtxKeyTenantID, id)
		}
	}
	// Role tests: X-Test-Role puts the shop role in the context and X-Test-Sub the
	// signed-in person, as ResolveShopRole and AuthMiddleware do in production.
	if role := c.GetHeader("X-Test-Role"); role != "" {
		c.Set(middleware.CtxKeyShopRole, team.Role(role))
	}
	if sub := c.GetHeader("X-Test-Sub"); sub != "" {
		c.Set(middleware.CtxKeyIDPSubject, sub)
	}
	c.Next()
}

func init() {
	gin.SetMode(gin.TestMode)
}

// ----- shared mock repo for handler tests -----

type mockBillRepo struct {
	bills   map[uuid.UUID]*domain.BillHead
	items   map[uuid.UUID][]*domain.BillItem
	counter int
	// itemReads counts GetBillItems calls, so a test can prove a path read only the head.
	itemReads int
	// defaultWH is returned by DefaultWarehouseID; nil = tenant has no default warehouse.
	defaultWH *uuid.UUID
	// lastFilter records the filter passed to SummarizeBills.
	lastFilter appbill.BillListFilter
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
	m.itemReads++
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

func (m *mockBillRepo) SummarizeBills(_ context.Context, f appbill.BillListFilter) (appbill.BillListSummary, error) {
	m.lastFilter = f
	var s appbill.BillListSummary
	for _, h := range m.bills {
		s.Count++
		s.TotalAmount = s.TotalAmount.Add(h.TotalAmount)
		s.PaidAmount = s.PaidAmount.Add(h.PaidAmount)
	}
	return s, nil
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

func (m *mockBillRepo) ProductExists(_ context.Context, _, _ uuid.UUID) (bool, error) {
	return true, nil
}

func (m *mockBillRepo) WarehouseExists(_ context.Context, _, _ uuid.UUID) (bool, error) {
	return true, nil
}

func (m *mockBillRepo) PartnerExists(_ context.Context, _, _ uuid.UUID) (bool, error) {
	return true, nil
}

func (m *mockBillRepo) DefaultWarehouseID(_ context.Context, _ uuid.UUID) (uuid.UUID, bool, error) {
	if m.defaultWH == nil {
		return uuid.Nil, false, nil
	}
	return *m.defaultWH, true, nil
}

func (m *mockBillRepo) SetBillWarehouse(_ context.Context, _ *sql.Tx, _, billID, warehouseID uuid.UUID) error {
	if h, ok := m.bills[billID]; ok {
		h.WarehouseID = &warehouseID
	}
	return nil
}

func (m *mockBillRepo) UpdatePaidAmount(_ context.Context, _ *sql.Tx, _, _ uuid.UUID, _ decimal.Decimal) error {
	return nil
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
		appbill.NewRestorePurchaseUseCase(repo),
	)
}

func newRouter(h *handlerbill.Handler) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(injectTenantFromHeader)
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

// TestBillHandler_CreatePurchase_RemarkTooLong_Returns400 verifies C4:
// a remark exceeding 500 characters is rejected with 400.
func TestBillHandler_CreatePurchase_RemarkTooLong_Returns400(t *testing.T) {
	repo := newMockBillRepo()
	r := newRouter(newTestHandler(repo))

	longRemark := make([]byte, 501)
	for i := range longRemark {
		longRemark[i] = 'x'
	}

	body := map[string]any{
		"warehouse_id": uuid.New().String(),
		"remark":       string(longRemark),
		"items": []map[string]any{
			{"product_id": uuid.New().String(), "qty": "1", "unit_price": "10", "line_no": 1},
		},
	}
	b, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/purchase-bills", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", devTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for over-length remark; body: %s", w.Code, w.Body.String())
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

func approvePurchaseViaHTTP(t *testing.T, repo *mockBillRepo, stockUC *shMockStockUC, units *shMockUnitRepo, warehouse *uuid.UUID, unitID *uuid.UUID) (int, map[string]any, uuid.UUID) {
	t.Helper()
	billID := uuid.New()
	repo.bills[billID] = &domain.BillHead{
		ID:          billID,
		TenantID:    uuid.MustParse(devTenantID),
		Status:      domain.StatusDraft,
		BillType:    domain.BillTypePurchase,
		WarehouseID: warehouse,
		TotalAmount: decimal.NewFromInt(100),
		CreatedAt:   time.Now(),
	}
	repo.items[billID] = []*domain.BillItem{
		{ID: uuid.New(), HeadID: billID, ProductID: uuid.New(), UnitID: unitID, Qty: decimal.NewFromInt(5), UnitPrice: decimal.NewFromInt(20)},
	}
	h := handlerbill.New(
		appbill.NewCreatePurchaseDraftUseCase(repo),
		appbill.NewUpdatePurchaseDraftUseCase(repo),
		appbill.NewApprovePurchaseUseCase(repo, stockUC, units),
		appbill.NewCancelPurchaseUseCase(repo),
		appbill.NewListPurchasesUseCase(repo),
		appbill.NewGetPurchaseUseCase(repo),
		appbill.NewRestorePurchaseUseCase(repo),
	)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/purchase-bills/"+billID.String()+"/approve", nil)
	req.Header.Set("X-Tenant-ID", devTenantID)
	newRouter(h).ServeHTTP(w, req)
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	return w.Code, resp, billID
}

// 采购单没选仓库且没有默认仓：审核返回 422 warehouse_required，不动库存。
func TestBillHandler_Approve_NoWarehouseNoDefault_Returns422(t *testing.T) {
	repo := newMockBillRepo()
	stockUC := &shMockStockUC{}

	code, resp, _ := approvePurchaseViaHTTP(t, repo, stockUC, &shMockUnitRepo{}, nil, nil)

	if code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", code)
	}
	if resp["error"] != "warehouse_required" {
		t.Errorf("error = %v, want warehouse_required", resp["error"])
	}
	if stockUC.calls != 0 {
		t.Errorf("stock calls = %d, want 0", stockUC.calls)
	}
}

// 采购单没选仓库但有默认仓：审核 200，单据回写默认仓。
func TestBillHandler_Approve_NoWarehouseWithDefault_Returns200(t *testing.T) {
	repo := newMockBillRepo()
	def := uuid.New()
	repo.defaultWH = &def

	code, _, billID := approvePurchaseViaHTTP(t, repo, &shMockStockUC{}, &shMockUnitRepo{}, nil, nil)

	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if wh := repo.bills[billID].WarehouseID; wh == nil || *wh != def {
		t.Errorf("bill warehouse = %v, want %s", wh, def)
	}
}

// 行上选的单位没有换算关系：422 invalid_unit_for_product，不能 500。
func TestBillHandler_Approve_InvalidUnit_Returns422(t *testing.T) {
	repo := newMockBillRepo()
	wh := uuid.New()
	unit := uuid.New()

	code, resp, _ := approvePurchaseViaHTTP(t, repo, &shMockStockUC{}, &shMockUnitRepo{err: appbill.ErrInvalidUnitForProduct}, &wh, &unit)

	if code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", code)
	}
	if resp["error"] != "invalid_unit_for_product" {
		t.Errorf("error = %v, want invalid_unit_for_product", resp["error"])
	}
}

// TestBillHandler_CreatePurchase_Currency verifies currency/exchange_rate reach the use case
// and invalid combinations are rejected with 400 rather than booked at 1:1.
func TestBillHandler_CreatePurchase_Currency(t *testing.T) {
	cases := []struct {
		name     string
		extra    map[string]any
		wantCode int
		wantCur  string
		wantRate string
		wantTot  string
	}{
		{"usd with rate", map[string]any{"currency": "USD", "exchange_rate": "7.2"}, http.StatusCreated, "USD", "7.2", "72"},
		{"usd without rate", map[string]any{"currency": "USD"}, http.StatusBadRequest, "", "", ""},
		{"usd zero rate", map[string]any{"currency": "USD", "exchange_rate": "0"}, http.StatusBadRequest, "", "", ""},
		{"lowercase currency", map[string]any{"currency": "usd", "exchange_rate": "7.2"}, http.StatusBadRequest, "", "", ""},
		{"bad rate", map[string]any{"currency": "USD", "exchange_rate": "x"}, http.StatusBadRequest, "", "", ""},
		{"rate too large", map[string]any{"currency": "USD", "exchange_rate": "99999999999999"}, http.StatusBadRequest, "", "", ""},
		{"rate too many decimals", map[string]any{"currency": "USD", "exchange_rate": "7.123456789"}, http.StatusBadRequest, "", "", ""},
		{"rate trailing zeros", map[string]any{"currency": "USD", "exchange_rate": "7.2000000000"}, http.StatusCreated, "USD", "7.2", "72"},
		{"cny no rate", map[string]any{"currency": "CNY"}, http.StatusCreated, "CNY", "1", "10"},
		{"default", nil, http.StatusCreated, "CNY", "1", "10"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newMockBillRepo()
			r := newRouter(newTestHandler(repo))
			body := map[string]any{
				"warehouse_id": uuid.New().String(),
				"items":        []map[string]any{{"product_id": uuid.New().String(), "qty": "1", "unit_price": "10", "line_no": 1}},
			}
			for k, v := range tc.extra {
				body[k] = v
			}
			b, _ := json.Marshal(body)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/purchase-bills", bytes.NewReader(b))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Tenant-ID", devTenantID)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d; body: %s", w.Code, tc.wantCode, w.Body.String())
			}
			if tc.wantCode != http.StatusCreated {
				return
			}
			if len(repo.bills) != 1 {
				t.Fatalf("bills = %d, want 1", len(repo.bills))
			}
			for _, h := range repo.bills {
				if h.Currency != tc.wantCur || h.ExchangeRateVal.String() != tc.wantRate || h.TotalAmount.String() != tc.wantTot {
					t.Errorf("head currency/rate/total = %s/%s/%s, want %s/%s/%s",
						h.Currency, h.ExchangeRateVal, h.TotalAmount, tc.wantCur, tc.wantRate, tc.wantTot)
				}
			}
		})
	}
}

// TestBillHandler_ListPurchases_InvalidParams_Returns400 verifies bad filters are rejected, not ignored.
func TestBillHandler_ListPurchases_InvalidParams_Returns400(t *testing.T) {
	r := newRouter(newTestHandler(newMockBillRepo()))
	for _, qs := range []string{"status=abc", "date_from=2026-02-30", "partner_id=x", "payment_status=open&status=0", "mine=1"} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/purchase-bills?"+qs, nil)
		req.Header.Set("X-Tenant-ID", devTenantID)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400; body: %s", qs, w.Code, w.Body.String())
			continue
		}
		var resp map[string]any
		_ = json.NewDecoder(w.Body).Decode(&resp)
		if resp["error"] != "validation_error" || resp["message"] == "" {
			t.Errorf("%s: unexpected body %v", qs, resp)
		}
	}
}

// TestBillHandler_ListPurchases_SummaryAndSettlement verifies derived fields and the summary block.
func TestBillHandler_ListPurchases_SummaryAndSettlement(t *testing.T) {
	repo := newMockBillRepo()
	id := uuid.New()
	tenantID, _ := uuid.Parse(devTenantID)
	now := time.Now()
	repo.bills[id] = &domain.BillHead{
		ID: id, TenantID: tenantID, BillNo: "PO-1", BillType: domain.BillTypePurchase,
		Status: domain.StatusApproved, PartnerName: "老张",
		TotalAmount: decimal.RequireFromString("100.50"), PaidAmount: decimal.RequireFromString("40"),
		BillDate: now, CreatedAt: now, UpdatedAt: now,
	}
	r := newRouter(newTestHandler(repo))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/purchase-bills?date_to=2026-10-31&payment_status=open", nil)
	req.Header.Set("X-Tenant-ID", devTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
		Sum   map[string]any   `json:"summary"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("items = %d", len(resp.Items))
	}
	it := resp.Items[0]
	if it["partner_name"] != "老张" || it["payment_status"] != "partial" || it["unpaid_amount"] != "60.5" {
		t.Errorf("item = %v", it)
	}
	if resp.Sum["count"] != float64(1) || resp.Sum["total_amount"] != "100.5" || resp.Sum["paid_amount"] != "40" {
		t.Errorf("summary = %v", resp.Sum)
	}
	f := repo.lastFilter
	if f.PaymentStatus != "open" || f.DateTo == nil || f.DateTo.Format(time.RFC3339) != "2026-11-01T00:00:00+08:00" {
		t.Errorf("filter not propagated: %+v", f)
	}
}

// TestBillHandler_UpdatePurchase_IgnoresCurrency verifies PUT does not validate the
// create-only currency fields: a currency without a rate must not turn into a 400.
func TestBillHandler_UpdatePurchase_IgnoresCurrency(t *testing.T) {
	repo := newMockBillRepo()
	r := newRouter(newTestHandler(repo))
	billID := uuid.New()
	repo.bills[billID] = &domain.BillHead{
		ID:              billID,
		TenantID:        uuid.MustParse(devTenantID),
		Status:          domain.StatusDraft,
		BillType:        domain.BillTypePurchase,
		Currency:        "USD",
		ExchangeRateVal: decimal.RequireFromString("7.2"),
		CreatedAt:       time.Now(),
	}
	body, _ := json.Marshal(map[string]any{
		"warehouse_id": uuid.New().String(),
		"currency":     "USD",
		"items":        []map[string]any{{"product_id": uuid.New().String(), "qty": "2", "unit_price": "100", "line_no": 1}},
	})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/purchase-bills/"+billID.String(), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", devTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

// approveZeroPriceDraft approves a draft whose single line has no price, with the given body.
func approveZeroPriceDraft(t *testing.T, body string) (*httptest.ResponseRecorder, *mockBillRepo, uuid.UUID) {
	t.Helper()
	return approveZeroPriceDraftNamed(t, body, nil)
}

// zpNamer answers every product id with the same name.
type zpNamer struct{ name string }

func (n zpNamer) ProductNames(_ context.Context, _ uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	m := make(map[uuid.UUID]string, len(ids))
	for _, id := range ids {
		m[id] = n.name
	}
	return m, nil
}

func approveZeroPriceDraftNamed(t *testing.T, body string, namer handlerbill.ProductNamer) (*httptest.ResponseRecorder, *mockBillRepo, uuid.UUID) {
	t.Helper()
	repo := newMockBillRepo()
	wh := uuid.New()
	billID := uuid.New()
	repo.bills[billID] = &domain.BillHead{
		ID: billID, TenantID: uuid.MustParse(devTenantID), Status: domain.StatusDraft,
		BillType: domain.BillTypePurchase, WarehouseID: &wh, CreatedAt: time.Now(),
	}
	repo.items[billID] = []*domain.BillItem{
		{ID: uuid.New(), HeadID: billID, ProductID: uuid.New(), LineNo: 2, Qty: decimal.NewFromInt(5), UnitPrice: decimal.Zero},
	}
	h := handlerbill.New(
		appbill.NewCreatePurchaseDraftUseCase(repo),
		appbill.NewUpdatePurchaseDraftUseCase(repo),
		appbill.NewApprovePurchaseUseCase(repo, &shMockStockUC{}, &shMockUnitRepo{}),
		appbill.NewCancelPurchaseUseCase(repo),
		appbill.NewListPurchasesUseCase(repo),
		appbill.NewGetPurchaseUseCase(repo),
		appbill.NewRestorePurchaseUseCase(repo),
	)
	if namer != nil {
		h = h.WithProductNames(namer)
	}
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/purchase-bills/"+billID.String()+"/approve", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", devTenantID)
	newRouter(h).ServeHTTP(w, req)
	return w, repo, billID
}

// 草稿里有进货价为 0 的行：审核返回 422 zero_price_lines，列出行号，单据仍是草稿。
func TestBillHandler_Approve_ZeroPriceLines_Returns422(t *testing.T) {
	w, repo, billID := approveZeroPriceDraft(t, ``)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "zero_price_lines" {
		t.Errorf("error = %v, want zero_price_lines", resp["error"])
	}
	if msg, _ := resp["message"].(string); !strings.Contains(msg, "第 2 行") {
		t.Errorf("message = %q, want it to name line 2", msg)
	}
	if lines, _ := resp["lines"].([]any); len(lines) != 1 {
		t.Errorf("lines = %v, want one entry", resp["lines"])
	}
	if repo.bills[billID].Status != domain.StatusDraft {
		t.Errorf("status = %d, want draft", repo.bills[billID].Status)
	}
}

// 能查到商品名时，提示和 lines 都带上商品名。
func TestBillHandler_Approve_ZeroPriceLines_NamesProducts(t *testing.T) {
	w, _, _ := approveZeroPriceDraftNamed(t, ``, zpNamer{name: "红枫"})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if msg, _ := resp["message"].(string); !strings.Contains(msg, "第 2 行「红枫」") {
		t.Errorf("message = %q, want it to name line 2 and 红枫", msg)
	}
	lines, _ := resp["lines"].([]any)
	if len(lines) != 1 || lines[0].(map[string]any)["product_name"] != "红枫" {
		t.Errorf("lines = %v, want product_name 红枫", resp["lines"])
	}
}

// 带 allow_zero_price=true 确认是赠品后，按 0 入账，审核返回 200。
func TestBillHandler_Approve_ZeroPriceLines_AllowedReturns200(t *testing.T) {
	w, repo, billID := approveZeroPriceDraft(t, `{"allow_zero_price":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	if repo.bills[billID].Status != domain.StatusApproved {
		t.Errorf("status = %d, want approved", repo.bills[billID].Status)
	}
}

// A bill that was already restored once cannot be restored again: 409 bill_already_restored, not 500.
func TestBillHandler_RestorePurchase_Returns409OnSecondRestore(t *testing.T) {
	repo := newMockBillRepo()
	tenantID, _ := uuid.Parse(devTenantID)
	billID := uuid.New()
	now := time.Now()
	repo.bills[billID] = &domain.BillHead{
		ID: billID, TenantID: tenantID, BillNo: "PO-20260101-0004", BillType: domain.BillTypePurchase,
		SubType: domain.BillSubTypePurchase, Status: domain.StatusCancelled, Revision: 1, CreatorID: tenantID,
		BillDate: now, CreatedAt: now, UpdatedAt: now,
	}

	r := newRouter(newTestHandler(repo))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/purchase-bills/"+billID.String()+"/restore", nil)
	req.Header.Set("X-Tenant-ID", devTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "bill_already_restored") || !strings.Contains(w.Body.String(), "请重新开一张") {
		t.Errorf("body = %s", w.Body.String())
	}
}

// A customer sale-return (入库) is not a purchase: the purchase restore route answers 404.
func TestBillHandler_RestorePurchase_Returns404OnSaleReturnBill(t *testing.T) {
	repo := newMockBillRepo()
	tenantID, _ := uuid.Parse(devTenantID)
	billID := uuid.New()
	now := time.Now()
	repo.bills[billID] = &domain.BillHead{
		ID: billID, TenantID: tenantID, BillNo: "SR-20260101-0001", BillType: domain.BillTypePurchase,
		SubType: domain.BillSubTypeSaleReturn, Status: domain.StatusCancelled, CreatorID: tenantID,
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
		t.Errorf("sale-return must stay cancelled, got %d", repo.bills[billID].Status)
	}
}
