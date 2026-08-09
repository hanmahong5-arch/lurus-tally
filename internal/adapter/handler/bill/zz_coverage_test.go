package bill

// This file adds unit + HTTP-level coverage for the bill handler package.
// It is deliberately written as an internal test file (package bill, not
// bill_test) so it can exercise the unexported parsing helpers directly
// (buildCreateRequest, buildCreateSaleRequest, parseSaleItems, resolveCreatorID,
// parseIntQuery, errWithField) in addition to full HTTP round trips.
//
// All fixtures/helpers below are prefixed zz to avoid colliding with
// identifiers in handler.go / sale_handler.go and in the sibling bill_test
// external test files that already exist in this directory.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
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

// ----- zz mock BillRepo -----

type zzMockBillRepo struct {
	bills   map[uuid.UUID]*domain.BillHead
	items   map[uuid.UUID][]*domain.BillItem
	counter int

	productExists      bool
	productExistsErr   error
	warehouseExists    bool
	warehouseExistsErr error

	nextBillNoErr       error
	acquireLockErr      error
	updateBillStatusErr error
	updateBillErr       error
	createBillErr       error
	getBillErr          error
	getBillForUpdateErr error
	getBillItemsErr     error
	updatePaidAmountErr error

	listResult []domain.BillHead
	listTotal  int64
	listErr    error
}

func zzNewMockBillRepo() *zzMockBillRepo {
	return &zzMockBillRepo{
		bills:           make(map[uuid.UUID]*domain.BillHead),
		items:           make(map[uuid.UUID][]*domain.BillItem),
		productExists:   true,
		warehouseExists: true,
	}
}

func (m *zzMockBillRepo) WithTx(_ context.Context, fn func(tx *sql.Tx) error) error {
	return fn(nil)
}

func (m *zzMockBillRepo) CreateBill(_ context.Context, _ *sql.Tx, head *domain.BillHead, items []*domain.BillItem) error {
	if m.createBillErr != nil {
		return m.createBillErr
	}
	m.bills[head.ID] = head
	m.items[head.ID] = items
	return nil
}

func (m *zzMockBillRepo) GetBillForUpdate(_ context.Context, _ *sql.Tx, _, billID uuid.UUID) (*domain.BillHead, error) {
	if m.getBillForUpdateErr != nil {
		return nil, m.getBillForUpdateErr
	}
	h, ok := m.bills[billID]
	if !ok {
		return nil, appbill.ErrBillNotFound
	}
	return h, nil
}

func (m *zzMockBillRepo) GetBill(_ context.Context, _, billID uuid.UUID) (*domain.BillHead, error) {
	if m.getBillErr != nil {
		return nil, m.getBillErr
	}
	h, ok := m.bills[billID]
	if !ok {
		return nil, appbill.ErrBillNotFound
	}
	return h, nil
}

func (m *zzMockBillRepo) GetBillItems(_ context.Context, _, billID uuid.UUID) ([]*domain.BillItem, error) {
	if m.getBillItemsErr != nil {
		return nil, m.getBillItemsErr
	}
	return m.items[billID], nil
}

func (m *zzMockBillRepo) UpdateBillStatus(_ context.Context, _ *sql.Tx, _, billID uuid.UUID, status domain.BillStatus, _ map[string]any) error {
	if m.updateBillStatusErr != nil {
		return m.updateBillStatusErr
	}
	h, ok := m.bills[billID]
	if !ok {
		return appbill.ErrBillNotFound
	}
	h.Status = status
	return nil
}

func (m *zzMockBillRepo) UpdateBill(_ context.Context, _ *sql.Tx, head *domain.BillHead, items []*domain.BillItem) error {
	if m.updateBillErr != nil {
		return m.updateBillErr
	}
	m.bills[head.ID] = head
	m.items[head.ID] = items
	return nil
}

func (m *zzMockBillRepo) ListBills(_ context.Context, _ appbill.BillListFilter) ([]domain.BillHead, int64, error) {
	if m.listErr != nil {
		return nil, 0, m.listErr
	}
	return m.listResult, m.listTotal, nil
}

func (m *zzMockBillRepo) NextBillNo(_ context.Context, _ *sql.Tx, _ uuid.UUID, prefix string) (string, error) {
	if m.nextBillNoErr != nil {
		return "", m.nextBillNoErr
	}
	m.counter++
	return fmt.Sprintf("%s-%s-%04d", prefix, time.Now().Format("20060102"), m.counter), nil
}

func (m *zzMockBillRepo) AcquireBillAdvisoryLock(_ context.Context, _ *sql.Tx, _, _ uuid.UUID) error {
	return m.acquireLockErr
}

func (m *zzMockBillRepo) ProductExists(_ context.Context, _, _ uuid.UUID) (bool, error) {
	return m.productExists, m.productExistsErr
}

func (m *zzMockBillRepo) WarehouseExists(_ context.Context, _, _ uuid.UUID) (bool, error) {
	return m.warehouseExists, m.warehouseExistsErr
}

func (m *zzMockBillRepo) UpdatePaidAmount(_ context.Context, _ *sql.Tx, _, billID uuid.UUID, paidAmount decimal.Decimal) error {
	if m.updatePaidAmountErr != nil {
		return m.updatePaidAmountErr
	}
	if h, ok := m.bills[billID]; ok {
		h.PaidAmount = paidAmount
	}
	return nil
}

var _ appbill.BillRepo = (*zzMockBillRepo)(nil)

// ----- zz mock StockMovementExecutor -----

type zzMockStockUC struct {
	err error
}

func (m *zzMockStockUC) ExecuteInTx(_ context.Context, _ *sql.Tx, req appstock.RecordMovementRequest) (*domainstock.Snapshot, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &domainstock.Snapshot{TenantID: req.TenantID, ProductID: req.ProductID}, nil
}

// ----- zz mock ProductUnitRepo -----

type zzMockUnitRepo struct {
	err error
}

func (m *zzMockUnitRepo) GetConversionFactor(_ context.Context, _, _ uuid.UUID) (decimal.Decimal, error) {
	if m.err != nil {
		return decimal.Zero, m.err
	}
	return decimal.NewFromInt(1), nil
}

// ----- zz mock PaymentRecorder -----

type zzMockPaymentRecorder struct{}

func (m *zzMockPaymentRecorder) Record(_ context.Context, _ *sql.Tx, _ *domainpayment.Payment) error {
	return nil
}

// ----- zz mock apppayment.PaymentRepo -----

type zzMockPaymentRepo struct {
	payments []*domainpayment.Payment
	listErr  error
}

func (m *zzMockPaymentRepo) Record(_ context.Context, _ *sql.Tx, p *domainpayment.Payment) error {
	m.payments = append(m.payments, p)
	return nil
}

func (m *zzMockPaymentRepo) ListByBill(_ context.Context, _, _ uuid.UUID) ([]*domainpayment.Payment, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.payments, nil
}

func (m *zzMockPaymentRepo) SumByBill(_ context.Context, _ *sql.Tx, _, _ uuid.UUID) (decimal.Decimal, error) {
	return decimal.Zero, nil
}

func (m *zzMockPaymentRepo) WithTx(_ context.Context, fn func(tx *sql.Tx) error) error {
	return fn(nil)
}

var _ apppayment.PaymentRepo = (*zzMockPaymentRepo)(nil)

// ----- builders -----

func zzNewPurchaseHandler(repo *zzMockBillRepo, stockUC appbill.StockMovementExecutor, unitRepo appbill.ProductUnitRepo) *Handler {
	return New(
		appbill.NewCreatePurchaseDraftUseCase(repo),
		appbill.NewUpdatePurchaseDraftUseCase(repo),
		appbill.NewApprovePurchaseUseCase(repo, stockUC, unitRepo),
		appbill.NewCancelPurchaseUseCase(repo),
		appbill.NewListPurchasesUseCase(repo),
		appbill.NewGetPurchaseUseCase(repo),
		appbill.NewRestorePurchaseUseCase(repo),
	)
}

func zzNewSaleHandler(
	repo *zzMockBillRepo,
	stockUC appbill.StockMovementExecutor,
	unitRepo appbill.ProductUnitRepo,
	payRec appbill.PaymentRecorder,
	payRepo apppayment.PaymentRepo,
) *SaleHandler {
	approveUC := appbill.NewApproveSaleUseCase(repo, stockUC, unitRepo, payRec)
	return NewSaleHandler(
		appbill.NewCreateSaleUseCase(repo),
		approveUC,
		appbill.NewCancelPurchaseUseCase(repo),
		appbill.NewListPurchasesUseCase(repo),
		repo,
		appbill.NewQuickCheckoutUseCase(repo, approveUC),
		apppayment.NewListPaymentsUseCase(payRepo),
	)
}

// zzTenantMW mimics AuthMiddleware: reads X-Tenant-ID / X-IDP-Sub test headers
// and injects them under the real middleware context keys. It intentionally
// never reads X-User-ID — production code must not trust that header either.
func zzTenantMW(c *gin.Context) {
	if raw := c.GetHeader("X-Tenant-ID"); raw != "" {
		if id, err := uuid.Parse(raw); err == nil {
			c.Set(middleware.CtxKeyTenantID, id)
		}
	}
	if sub := c.GetHeader("X-IDP-Sub"); sub != "" {
		c.Set(middleware.CtxKeyIDPSubject, sub)
	}
	c.Next()
}

func zzNewRouter(h *Handler) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(zzTenantMW)
	api := r.Group("/api/v1")
	h.RegisterRoutes(api)
	return r
}

func zzNewSaleRouter(h *SaleHandler) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(zzTenantMW)
	api := r.Group("/api/v1")
	h.RegisterRoutes(api)
	return r
}

func zzDoJSON(t *testing.T, r *gin.Engine, method, path string, body any, tenantID, sub string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if tenantID != "" {
		req.Header.Set("X-Tenant-ID", tenantID)
	}
	if sub != "" {
		req.Header.Set("X-IDP-Sub", sub)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func zzDoRaw(t *testing.T, r *gin.Engine, method, path, rawBody, tenantID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(rawBody))
	req.Header.Set("Content-Type", "application/json")
	if tenantID != "" {
		req.Header.Set("X-Tenant-ID", tenantID)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func zzWantStatus(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Errorf("status = %d, want %d; body: %s", w.Code, want, w.Body.String())
	}
}

func zzDecodeJSON(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v; body: %s", err, w.Body.String())
	}
	return resp
}

func zzValidPurchaseItem() map[string]any {
	return map[string]any{"product_id": uuid.New().String(), "qty": "2", "unit_price": "5.00", "line_no": 1}
}

func zzValidSaleItem() map[string]any {
	return map[string]any{"product_id": uuid.New().String(), "warehouse_id": uuid.New().String(), "qty": "2", "unit_price": "5.00", "line_no": 1}
}

// ============================================================================
// Direct unit tests for unexported helpers (resolveCreatorID, buildCreateRequest,
// buildCreateSaleRequest, parseSaleItems, errWithField).
// ============================================================================

func TestZZ_ResolveCreatorID(t *testing.T) {
	t.Run("missing key falls back to Nil", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		if got := resolveCreatorID(c); got != uuid.Nil {
			t.Errorf("got %v, want Nil", got)
		}
	})
	t.Run("non-string value falls back to Nil", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set(middleware.CtxKeyIDPSubject, 12345) // spoof attempt with wrong type
		if got := resolveCreatorID(c); got != uuid.Nil {
			t.Errorf("got %v, want Nil", got)
		}
	})
	t.Run("invalid uuid string falls back to Nil", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set(middleware.CtxKeyIDPSubject, "not-a-uuid")
		if got := resolveCreatorID(c); got != uuid.Nil {
			t.Errorf("got %v, want Nil", got)
		}
	})
	t.Run("valid uuid subject is trusted", func(t *testing.T) {
		id := uuid.New()
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set(middleware.CtxKeyIDPSubject, id.String())
		if got := resolveCreatorID(c); got != id {
			t.Errorf("got %v, want %v", got, id)
		}
	})
}

func TestZZ_FieldError(t *testing.T) {
	err := errWithField("qty", "must be positive")
	if err.Error() != "qty: must be positive" {
		t.Errorf("Error() = %q, want %q", err.Error(), "qty: must be positive")
	}
}

func TestZZ_BuildCreateRequest_FieldErrors(t *testing.T) {
	tenantID := uuid.New()
	creatorID := uuid.New()
	validItem := func() itemInput {
		return itemInput{ProductID: uuid.New().String(), Qty: "1", UnitPrice: "1"}
	}

	cases := []struct {
		name      string
		req       createRequest
		wantField string
	}{
		{"bad partner_id", createRequest{PartnerID: "nope", Items: []itemInput{validItem()}}, "partner_id"},
		{"bad warehouse_id", createRequest{WarehouseID: "nope", Items: []itemInput{validItem()}}, "warehouse_id"},
		{"bad bill_date", createRequest{BillDate: "not-rfc3339", Items: []itemInput{validItem()}}, "bill_date"},
		{"bad shipping_fee decimal", createRequest{ShippingFee: "abc", Items: []itemInput{validItem()}}, "shipping_fee"},
		{"bad tax_amount decimal", createRequest{TaxAmount: "abc", Items: []itemInput{validItem()}}, "tax_amount"},
		{"bad item product_id", createRequest{Items: []itemInput{{ProductID: "nope", Qty: "1"}}}, "items[0].product_id"},
		{"zero qty rejected", createRequest{Items: []itemInput{{ProductID: uuid.New().String(), Qty: "0"}}}, "items[0].qty"},
		{"negative qty rejected", createRequest{Items: []itemInput{{ProductID: uuid.New().String(), Qty: "-1"}}}, "items[0].qty"},
		{"non-decimal qty rejected", createRequest{Items: []itemInput{{ProductID: uuid.New().String(), Qty: "abc"}}}, "items[0].qty"},
		{"negative unit_price rejected", createRequest{Items: []itemInput{{ProductID: uuid.New().String(), Qty: "1", UnitPrice: "-5"}}}, "items[0].unit_price"},
		{"non-decimal unit_price rejected", createRequest{Items: []itemInput{{ProductID: uuid.New().String(), Qty: "1", UnitPrice: "abc"}}}, "items[0].unit_price"},
		{"bad unit_id", createRequest{Items: []itemInput{{ProductID: uuid.New().String(), Qty: "1", UnitID: "nope"}}}, "items[0].unit_id"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := buildCreateRequest(tenantID, creatorID, tc.req)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			wantPrefix := tc.wantField + ":"
			if !strings.HasPrefix(err.Error(), wantPrefix) {
				t.Errorf("error = %q, want prefix %q", err.Error(), wantPrefix)
			}
		})
	}
}

func TestZZ_BuildCreateRequest_Success(t *testing.T) {
	tenantID := uuid.New()
	creatorID := uuid.New()
	partnerID := uuid.New()
	warehouseID := uuid.New()
	unitID := uuid.New()
	productID := uuid.New()

	req := createRequest{
		PartnerID:   partnerID.String(),
		WarehouseID: warehouseID.String(),
		BillDate:    "2026-01-02T03:04:05Z",
		ShippingFee: "12.50",
		TaxAmount:   "3.25",
		Remark:      "note",
		Items: []itemInput{
			{ProductID: productID.String(), UnitID: unitID.String(), Qty: "2.5", UnitPrice: "10.00", LineNo: 0},
			{ProductID: uuid.New().String(), Qty: "1", UnitPrice: "1", LineNo: 7},
		},
	}

	out, err := buildCreateRequest(tenantID, creatorID, req)
	if err != nil {
		t.Fatalf("buildCreateRequest: %v", err)
	}
	if out.TenantID != tenantID || out.CreatorID != creatorID {
		t.Errorf("tenant/creator mismatch: %+v", out)
	}
	if out.PartnerID == nil || *out.PartnerID != partnerID {
		t.Errorf("PartnerID = %v, want %v", out.PartnerID, partnerID)
	}
	if out.WarehouseID == nil || *out.WarehouseID != warehouseID {
		t.Errorf("WarehouseID = %v, want %v", out.WarehouseID, warehouseID)
	}
	wantDate, _ := time.Parse(time.RFC3339, "2026-01-02T03:04:05Z")
	if !out.BillDate.Equal(wantDate) {
		t.Errorf("BillDate = %v, want %v", out.BillDate, wantDate)
	}
	if !out.ShippingFee.Equal(decimal.RequireFromString("12.50")) {
		t.Errorf("ShippingFee = %v, want 12.50", out.ShippingFee)
	}
	if !out.TaxAmount.Equal(decimal.RequireFromString("3.25")) {
		t.Errorf("TaxAmount = %v, want 3.25", out.TaxAmount)
	}
	if len(out.Items) != 2 {
		t.Fatalf("len(Items) = %d, want 2", len(out.Items))
	}
	it0 := out.Items[0]
	if it0.ProductID != productID {
		t.Errorf("item[0].ProductID = %v, want %v", it0.ProductID, productID)
	}
	if it0.UnitID == nil || *it0.UnitID != unitID {
		t.Errorf("item[0].UnitID = %v, want %v", it0.UnitID, unitID)
	}
	if it0.LineNo != 1 {
		t.Errorf("item[0].LineNo = %d, want 1 (defaulted from index)", it0.LineNo)
	}
	if !it0.Qty.Equal(decimal.RequireFromString("2.5")) {
		t.Errorf("item[0].Qty = %v, want 2.5", it0.Qty)
	}
	if !it0.UnitPrice.Equal(decimal.RequireFromString("10.00")) {
		t.Errorf("item[0].UnitPrice = %v, want 10.00", it0.UnitPrice)
	}
	if out.Items[1].LineNo != 7 {
		t.Errorf("item[1].LineNo = %d, want 7 (explicit, kept as-is)", out.Items[1].LineNo)
	}
}

func TestZZ_BuildCreateRequest_DefaultsWhenFieldsEmpty(t *testing.T) {
	tenantID := uuid.New()
	creatorID := uuid.New()
	req := createRequest{
		Items: []itemInput{{ProductID: uuid.New().String(), Qty: "1"}},
	}
	before := time.Now().UTC()
	out, err := buildCreateRequest(tenantID, creatorID, req)
	after := time.Now().UTC()
	if err != nil {
		t.Fatalf("buildCreateRequest: %v", err)
	}
	if out.BillDate.Before(before) || out.BillDate.After(after) {
		t.Errorf("BillDate = %v, want between %v and %v (defaulted to now)", out.BillDate, before, after)
	}
	if !out.ShippingFee.IsZero() {
		t.Errorf("ShippingFee = %v, want zero when empty", out.ShippingFee)
	}
	if !out.TaxAmount.IsZero() {
		t.Errorf("TaxAmount = %v, want zero when empty", out.TaxAmount)
	}
	if !out.Items[0].UnitPrice.IsZero() {
		t.Errorf("UnitPrice = %v, want zero when empty", out.Items[0].UnitPrice)
	}
}

func TestZZ_BuildCreateSaleRequest_FieldErrors(t *testing.T) {
	tenantID := uuid.New()
	creatorID := uuid.New()
	validItem := func() saleItemInput {
		return saleItemInput{ProductID: uuid.New().String(), Qty: "1"}
	}

	cases := []struct {
		name      string
		req       createSaleRequest
		wantField string
	}{
		{"bad partner_id", createSaleRequest{PartnerID: "nope", Items: []saleItemInput{validItem()}}, "partner_id"},
		{"bad warehouse_id", createSaleRequest{WarehouseID: "nope", Items: []saleItemInput{validItem()}}, "warehouse_id"},
		{"bad bill_date", createSaleRequest{BillDate: "not-rfc3339", Items: []saleItemInput{validItem()}}, "bill_date"},
		{"bad shipping_fee", createSaleRequest{ShippingFee: "abc", Items: []saleItemInput{validItem()}}, "shipping_fee"},
		{"bad tax_amount", createSaleRequest{TaxAmount: "abc", Items: []saleItemInput{validItem()}}, "tax_amount"},
		{"bad item warehouse_id propagates from parseSaleItems", createSaleRequest{Items: []saleItemInput{{ProductID: uuid.New().String(), WarehouseID: "nope", Qty: "1"}}}, "items[0].warehouse_id"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := buildCreateSaleRequest(tenantID, creatorID, tc.req)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			wantPrefix := tc.wantField + ":"
			if !strings.HasPrefix(err.Error(), wantPrefix) {
				t.Errorf("error = %q, want prefix %q", err.Error(), wantPrefix)
			}
		})
	}
}

func TestZZ_BuildCreateSaleRequest_Success(t *testing.T) {
	tenantID := uuid.New()
	creatorID := uuid.New()
	req := createSaleRequest{
		ShippingFee: "1.00",
		TaxAmount:   "2.00",
		Items: []saleItemInput{
			{ProductID: uuid.New().String(), WarehouseID: uuid.New().String(), Qty: "3", UnitPrice: "4.00"},
		},
	}
	out, err := buildCreateSaleRequest(tenantID, creatorID, req)
	if err != nil {
		t.Fatalf("buildCreateSaleRequest: %v", err)
	}
	if out.TenantID != tenantID || out.CreatorID != creatorID {
		t.Errorf("tenant/creator mismatch: %+v", out)
	}
	if len(out.Items) != 1 {
		t.Fatalf("len(Items) = %d, want 1", len(out.Items))
	}
}

func TestZZ_ParseSaleItems_FieldErrors(t *testing.T) {
	cases := []struct {
		name      string
		items     []saleItemInput
		wantField string
	}{
		{"bad product_id", []saleItemInput{{ProductID: "nope", Qty: "1"}}, "items[0].product_id"},
		{"zero qty", []saleItemInput{{ProductID: uuid.New().String(), Qty: "0"}}, "items[0].qty"},
		{"negative qty", []saleItemInput{{ProductID: uuid.New().String(), Qty: "-1"}}, "items[0].qty"},
		{"negative unit_price", []saleItemInput{{ProductID: uuid.New().String(), Qty: "1", UnitPrice: "-1"}}, "items[0].unit_price"},
		{"bad unit_id", []saleItemInput{{ProductID: uuid.New().String(), Qty: "1", UnitID: "nope"}}, "items[0].unit_id"},
		{"bad warehouse_id", []saleItemInput{{ProductID: uuid.New().String(), Qty: "1", WarehouseID: "nope"}}, "items[0].warehouse_id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseSaleItems(tc.items)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			wantPrefix := tc.wantField + ":"
			if !strings.HasPrefix(err.Error(), wantPrefix) {
				t.Errorf("error = %q, want prefix %q", err.Error(), wantPrefix)
			}
		})
	}
}

func TestZZ_ParseSaleItems_LineNoDefaultsFromIndex(t *testing.T) {
	items, err := parseSaleItems([]saleItemInput{
		{ProductID: uuid.New().String(), Qty: "1"},
		{ProductID: uuid.New().String(), Qty: "1", LineNo: 9},
	})
	if err != nil {
		t.Fatalf("parseSaleItems: %v", err)
	}
	if items[0].LineNo != 1 {
		t.Errorf("items[0].LineNo = %d, want 1", items[0].LineNo)
	}
	if items[1].LineNo != 9 {
		t.Errorf("items[1].LineNo = %d, want 9", items[1].LineNo)
	}
}

// ============================================================================
// Purchase Handler HTTP-level tests
// ============================================================================

func TestZZ_Handler_Create(t *testing.T) {
	t.Run("tenant nil returns 401", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewRouter(zzNewPurchaseHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}))
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/purchase-bills", map[string]any{"items": []map[string]any{zzValidPurchaseItem()}}, "", "")
		zzWantStatus(t, w, http.StatusUnauthorized)
	})

	t.Run("malformed json returns 400", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewRouter(zzNewPurchaseHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}))
		w := zzDoRaw(t, r, http.MethodPost, "/api/v1/purchase-bills", "{not-json", uuid.New().String())
		zzWantStatus(t, w, http.StatusBadRequest)
	})

	t.Run("empty items returns 400", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewRouter(zzNewPurchaseHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}))
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/purchase-bills", map[string]any{"items": []map[string]any{}}, uuid.New().String(), "")
		zzWantStatus(t, w, http.StatusBadRequest)
	})

	t.Run("buildCreateRequest field error returns 400", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewRouter(zzNewPurchaseHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}))
		body := map[string]any{"partner_id": "nope", "items": []map[string]any{zzValidPurchaseItem()}}
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/purchase-bills", body, uuid.New().String(), "")
		zzWantStatus(t, w, http.StatusBadRequest)
	})

	t.Run("usecase ErrValidation (cross-tenant product) returns 400", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		repo.productExists = false
		r := zzNewRouter(zzNewPurchaseHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}))
		body := map[string]any{"items": []map[string]any{zzValidPurchaseItem()}}
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/purchase-bills", body, uuid.New().String(), "")
		zzWantStatus(t, w, http.StatusBadRequest)
	})

	t.Run("internal error returns 5xx", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		repo.nextBillNoErr = errors.New("db unavailable")
		r := zzNewRouter(zzNewPurchaseHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}))
		body := map[string]any{"items": []map[string]any{zzValidPurchaseItem()}}
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/purchase-bills", body, uuid.New().String(), "")
		if w.Code < 500 {
			t.Errorf("status = %d, want >=500; body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("creator falls back to tenant when subject absent", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		tenantID := uuid.New()
		r := zzNewRouter(zzNewPurchaseHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}))
		body := map[string]any{"items": []map[string]any{zzValidPurchaseItem()}}
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/purchase-bills", body, tenantID.String(), "")
		zzWantStatus(t, w, http.StatusCreated)
		resp := zzDecodeJSON(t, w)
		billID, err := uuid.Parse(resp["bill_id"].(string))
		if err != nil {
			t.Fatalf("bill_id: %v", err)
		}
		head, ok := repo.bills[billID]
		if !ok {
			t.Fatalf("bill not persisted")
		}
		if head.CreatorID != tenantID {
			t.Errorf("CreatorID = %v, want fallback tenantID %v", head.CreatorID, tenantID)
		}
	})

	t.Run("creator trusts only IDP subject, never a spoofable header", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		tenantID := uuid.New()
		subjectID := uuid.New()
		r := zzNewRouter(zzNewPurchaseHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}))
		body := map[string]any{"items": []map[string]any{zzValidPurchaseItem()}}
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/purchase-bills", body, tenantID.String(), subjectID.String())
		zzWantStatus(t, w, http.StatusCreated)
		resp := zzDecodeJSON(t, w)
		billID, err := uuid.Parse(resp["bill_id"].(string))
		if err != nil {
			t.Fatalf("bill_id: %v", err)
		}
		head := repo.bills[billID]
		if head.CreatorID != subjectID {
			t.Errorf("CreatorID = %v, want trusted subject %v", head.CreatorID, subjectID)
		}
	})
}

func TestZZ_Handler_Update(t *testing.T) {
	t.Run("tenant nil returns 401 (checked before id)", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewRouter(zzNewPurchaseHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}))
		w := zzDoJSON(t, r, http.MethodPut, "/api/v1/purchase-bills/not-even-a-uuid", nil, "", "")
		zzWantStatus(t, w, http.StatusUnauthorized)
	})

	t.Run("invalid bill id returns 400", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewRouter(zzNewPurchaseHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}))
		w := zzDoJSON(t, r, http.MethodPut, "/api/v1/purchase-bills/not-a-uuid", map[string]any{"items": []map[string]any{zzValidPurchaseItem()}}, uuid.New().String(), "")
		zzWantStatus(t, w, http.StatusBadRequest)
	})

	t.Run("malformed json returns 400", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewRouter(zzNewPurchaseHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}))
		w := zzDoRaw(t, r, http.MethodPut, "/api/v1/purchase-bills/"+uuid.New().String(), "{bad", uuid.New().String())
		zzWantStatus(t, w, http.StatusBadRequest)
	})

	t.Run("buildCreateRequest field error returns 400", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewRouter(zzNewPurchaseHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}))
		body := map[string]any{"warehouse_id": "nope", "items": []map[string]any{zzValidPurchaseItem()}}
		w := zzDoJSON(t, r, http.MethodPut, "/api/v1/purchase-bills/"+uuid.New().String(), body, uuid.New().String(), "")
		zzWantStatus(t, w, http.StatusBadRequest)
	})

	t.Run("bill not found returns 404", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewRouter(zzNewPurchaseHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}))
		body := map[string]any{"items": []map[string]any{zzValidPurchaseItem()}}
		w := zzDoJSON(t, r, http.MethodPut, "/api/v1/purchase-bills/"+uuid.New().String(), body, uuid.New().String(), "")
		zzWantStatus(t, w, http.StatusNotFound)
	})

	t.Run("approved bill returns 422 invalid_bill_status (only draft can be updated)", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		tenantID := uuid.New()
		billID := uuid.New()
		repo.bills[billID] = &domain.BillHead{ID: billID, TenantID: tenantID, Status: domain.StatusApproved, BillType: domain.BillTypePurchase}
		r := zzNewRouter(zzNewPurchaseHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}))
		body := map[string]any{"items": []map[string]any{zzValidPurchaseItem()}}
		w := zzDoJSON(t, r, http.MethodPut, "/api/v1/purchase-bills/"+billID.String(), body, tenantID.String(), "")
		zzWantStatus(t, w, http.StatusUnprocessableEntity)
	})

	t.Run("draft bill update succeeds with 200", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		tenantID := uuid.New()
		billID := uuid.New()
		repo.bills[billID] = &domain.BillHead{ID: billID, TenantID: tenantID, Status: domain.StatusDraft, BillType: domain.BillTypePurchase}
		r := zzNewRouter(zzNewPurchaseHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}))
		body := map[string]any{"items": []map[string]any{zzValidPurchaseItem()}}
		w := zzDoJSON(t, r, http.MethodPut, "/api/v1/purchase-bills/"+billID.String(), body, tenantID.String(), "")
		zzWantStatus(t, w, http.StatusOK)
	})
}

func TestZZ_Handler_Approve(t *testing.T) {
	t.Run("tenant nil returns 401", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewRouter(zzNewPurchaseHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}))
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/purchase-bills/"+uuid.New().String()+"/approve", nil, "", "")
		zzWantStatus(t, w, http.StatusUnauthorized)
	})

	t.Run("invalid id returns 400", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewRouter(zzNewPurchaseHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}))
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/purchase-bills/not-a-uuid/approve", nil, uuid.New().String(), "")
		zzWantStatus(t, w, http.StatusBadRequest)
	})

	t.Run("bill not found returns 404", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewRouter(zzNewPurchaseHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}))
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/purchase-bills/"+uuid.New().String()+"/approve", nil, uuid.New().String(), "")
		zzWantStatus(t, w, http.StatusNotFound)
	})

	t.Run("cancelled bill cannot transition to approved: 422", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		tenantID := uuid.New()
		billID := uuid.New()
		repo.bills[billID] = &domain.BillHead{ID: billID, TenantID: tenantID, Status: domain.StatusCancelled, BillType: domain.BillTypePurchase}
		r := zzNewRouter(zzNewPurchaseHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}))
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/purchase-bills/"+billID.String()+"/approve", nil, tenantID.String(), "")
		zzWantStatus(t, w, http.StatusUnprocessableEntity)
	})

	t.Run("concurrent approval conflict returns 409", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		repo.acquireLockErr = errors.New("advisory lock busy")
		tenantID := uuid.New()
		billID := uuid.New()
		repo.bills[billID] = &domain.BillHead{ID: billID, TenantID: tenantID, Status: domain.StatusDraft, BillType: domain.BillTypePurchase}
		r := zzNewRouter(zzNewPurchaseHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}))
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/purchase-bills/"+billID.String()+"/approve", nil, tenantID.String(), "")
		zzWantStatus(t, w, http.StatusConflict)
	})

	t.Run("invalid unit for product returns 422", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		tenantID := uuid.New()
		billID := uuid.New()
		unitID := uuid.New()
		repo.bills[billID] = &domain.BillHead{ID: billID, TenantID: tenantID, Status: domain.StatusDraft, BillType: domain.BillTypePurchase}
		repo.items[billID] = []*domain.BillItem{{ID: uuid.New(), TenantID: tenantID, HeadID: billID, ProductID: uuid.New(), UnitID: &unitID, Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(1)}}
		r := zzNewRouter(zzNewPurchaseHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{err: appbill.ErrInvalidUnitForProduct}))
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/purchase-bills/"+billID.String()+"/approve", nil, tenantID.String(), "")
		zzWantStatus(t, w, http.StatusUnprocessableEntity)
	})

	t.Run("internal error returns 5xx", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		tenantID := uuid.New()
		billID := uuid.New()
		repo.bills[billID] = &domain.BillHead{ID: billID, TenantID: tenantID, Status: domain.StatusDraft, BillType: domain.BillTypePurchase}
		repo.items[billID] = []*domain.BillItem{{ID: uuid.New(), TenantID: tenantID, HeadID: billID, ProductID: uuid.New(), Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(1)}}
		r := zzNewRouter(zzNewPurchaseHandler(repo, &zzMockStockUC{err: errors.New("stock infra down")}, &zzMockUnitRepo{}))
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/purchase-bills/"+billID.String()+"/approve", nil, tenantID.String(), "")
		if w.Code < 500 {
			t.Errorf("status = %d, want >=500; body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("draft bill approves successfully: 200", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		tenantID := uuid.New()
		billID := uuid.New()
		repo.bills[billID] = &domain.BillHead{ID: billID, TenantID: tenantID, Status: domain.StatusDraft, BillType: domain.BillTypePurchase}
		repo.items[billID] = []*domain.BillItem{{ID: uuid.New(), TenantID: tenantID, HeadID: billID, ProductID: uuid.New(), Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(1)}}
		r := zzNewRouter(zzNewPurchaseHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}))
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/purchase-bills/"+billID.String()+"/approve", nil, tenantID.String(), "")
		zzWantStatus(t, w, http.StatusOK)
		if repo.bills[billID].Status != domain.StatusApproved {
			t.Errorf("status = %d, want Approved", repo.bills[billID].Status)
		}
	})
}

func TestZZ_Handler_Cancel(t *testing.T) {
	t.Run("tenant nil returns 401", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewRouter(zzNewPurchaseHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}))
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/purchase-bills/"+uuid.New().String()+"/cancel", nil, "", "")
		zzWantStatus(t, w, http.StatusUnauthorized)
	})

	t.Run("invalid id returns 400", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewRouter(zzNewPurchaseHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}))
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/purchase-bills/not-a-uuid/cancel", nil, uuid.New().String(), "")
		zzWantStatus(t, w, http.StatusBadRequest)
	})

	t.Run("bill not found returns 404", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewRouter(zzNewPurchaseHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}))
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/purchase-bills/"+uuid.New().String()+"/cancel", nil, uuid.New().String(), "")
		zzWantStatus(t, w, http.StatusNotFound)
	})

	t.Run("draft bill cancels successfully: 200", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		tenantID := uuid.New()
		billID := uuid.New()
		repo.bills[billID] = &domain.BillHead{ID: billID, TenantID: tenantID, Status: domain.StatusDraft, BillType: domain.BillTypePurchase}
		r := zzNewRouter(zzNewPurchaseHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}))
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/purchase-bills/"+billID.String()+"/cancel", nil, tenantID.String(), "")
		zzWantStatus(t, w, http.StatusOK)
	})

	t.Run("internal error returns 5xx", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		repo.getBillForUpdateErr = errors.New("connection reset")
		r := zzNewRouter(zzNewPurchaseHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}))
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/purchase-bills/"+uuid.New().String()+"/cancel", nil, uuid.New().String(), "")
		if w.Code < 500 {
			t.Errorf("status = %d, want >=500; body: %s", w.Code, w.Body.String())
		}
	})
}

func TestZZ_Handler_RestorePurchase_AdditionalBranches(t *testing.T) {
	t.Run("tenant nil returns 401", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewRouter(zzNewPurchaseHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}))
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/purchase-bills/"+uuid.New().String()+"/restore", nil, "", "")
		zzWantStatus(t, w, http.StatusUnauthorized)
	})

	t.Run("invalid id returns 400", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewRouter(zzNewPurchaseHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}))
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/purchase-bills/not-a-uuid/restore", nil, uuid.New().String(), "")
		zzWantStatus(t, w, http.StatusBadRequest)
	})

	t.Run("bill not found returns 404", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewRouter(zzNewPurchaseHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}))
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/purchase-bills/"+uuid.New().String()+"/restore", nil, uuid.New().String(), "")
		zzWantStatus(t, w, http.StatusNotFound)
	})
}

func TestZZ_Handler_List(t *testing.T) {
	t.Run("tenant nil returns 401", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewRouter(zzNewPurchaseHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}))
		w := zzDoJSON(t, r, http.MethodGet, "/api/v1/purchase-bills", nil, "", "")
		zzWantStatus(t, w, http.StatusUnauthorized)
	})

	t.Run("numeric status and valid partner_id set filters: 200", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewRouter(zzNewPurchaseHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}))
		w := zzDoJSON(t, r, http.MethodGet, "/api/v1/purchase-bills?status=2&partner_id="+uuid.New().String(), nil, uuid.New().String(), "")
		zzWantStatus(t, w, http.StatusOK)
	})

	t.Run("non-numeric status and invalid partner_id are ignored: 200", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewRouter(zzNewPurchaseHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}))
		w := zzDoJSON(t, r, http.MethodGet, "/api/v1/purchase-bills?status=abc&partner_id=not-a-uuid", nil, uuid.New().String(), "")
		zzWantStatus(t, w, http.StatusOK)
	})

	t.Run("internal error returns 5xx", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		repo.listErr = errors.New("db down")
		r := zzNewRouter(zzNewPurchaseHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}))
		w := zzDoJSON(t, r, http.MethodGet, "/api/v1/purchase-bills", nil, uuid.New().String(), "")
		if w.Code < 500 {
			t.Errorf("status = %d, want >=500; body: %s", w.Code, w.Body.String())
		}
	})
}

func TestZZ_Handler_Get(t *testing.T) {
	t.Run("tenant nil returns 401", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewRouter(zzNewPurchaseHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}))
		w := zzDoJSON(t, r, http.MethodGet, "/api/v1/purchase-bills/"+uuid.New().String(), nil, "", "")
		zzWantStatus(t, w, http.StatusUnauthorized)
	})

	t.Run("invalid id returns 400", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewRouter(zzNewPurchaseHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}))
		w := zzDoJSON(t, r, http.MethodGet, "/api/v1/purchase-bills/not-a-uuid", nil, uuid.New().String(), "")
		zzWantStatus(t, w, http.StatusBadRequest)
	})

	t.Run("success returns 200 with head and items", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		tenantID := uuid.New()
		billID := uuid.New()
		repo.bills[billID] = &domain.BillHead{ID: billID, TenantID: tenantID, Status: domain.StatusDraft, BillType: domain.BillTypePurchase}
		repo.items[billID] = []*domain.BillItem{}
		r := zzNewRouter(zzNewPurchaseHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}))
		w := zzDoJSON(t, r, http.MethodGet, "/api/v1/purchase-bills/"+billID.String(), nil, tenantID.String(), "")
		zzWantStatus(t, w, http.StatusOK)
	})

	t.Run("internal error (GetBillItems failure) returns 5xx", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		tenantID := uuid.New()
		billID := uuid.New()
		repo.bills[billID] = &domain.BillHead{ID: billID, TenantID: tenantID, Status: domain.StatusDraft, BillType: domain.BillTypePurchase}
		repo.getBillItemsErr = errors.New("read failure")
		r := zzNewRouter(zzNewPurchaseHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}))
		w := zzDoJSON(t, r, http.MethodGet, "/api/v1/purchase-bills/"+billID.String(), nil, tenantID.String(), "")
		if w.Code < 500 {
			t.Errorf("status = %d, want >=500; body: %s", w.Code, w.Body.String())
		}
	})
}

// ============================================================================
// Sale Handler HTTP-level tests
// ============================================================================

func TestZZ_SaleHandler_Create(t *testing.T) {
	t.Run("tenant nil returns 401", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewSaleRouter(zzNewSaleHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}, &zzMockPaymentRecorder{}, &zzMockPaymentRepo{}))
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/sale-bills", map[string]any{"items": []map[string]any{zzValidSaleItem()}}, "", "")
		zzWantStatus(t, w, http.StatusUnauthorized)
	})

	t.Run("malformed json returns 400", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewSaleRouter(zzNewSaleHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}, &zzMockPaymentRecorder{}, &zzMockPaymentRepo{}))
		w := zzDoRaw(t, r, http.MethodPost, "/api/v1/sale-bills", "{bad", uuid.New().String())
		zzWantStatus(t, w, http.StatusBadRequest)
	})

	t.Run("empty items returns 400", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewSaleRouter(zzNewSaleHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}, &zzMockPaymentRecorder{}, &zzMockPaymentRepo{}))
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/sale-bills", map[string]any{"items": []map[string]any{}}, uuid.New().String(), "")
		zzWantStatus(t, w, http.StatusBadRequest)
	})

	t.Run("buildCreateSaleRequest field error returns 400", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewSaleRouter(zzNewSaleHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}, &zzMockPaymentRecorder{}, &zzMockPaymentRepo{}))
		body := map[string]any{"partner_id": "nope", "items": []map[string]any{zzValidSaleItem()}}
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/sale-bills", body, uuid.New().String(), "")
		zzWantStatus(t, w, http.StatusBadRequest)
	})

	t.Run("usecase ErrValidation (cross-tenant product) returns 400", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		repo.productExists = false
		r := zzNewSaleRouter(zzNewSaleHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}, &zzMockPaymentRecorder{}, &zzMockPaymentRepo{}))
		body := map[string]any{"items": []map[string]any{zzValidSaleItem()}}
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/sale-bills", body, uuid.New().String(), "")
		zzWantStatus(t, w, http.StatusBadRequest)
	})

	t.Run("internal error returns 5xx", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		repo.nextBillNoErr = errors.New("db down")
		r := zzNewSaleRouter(zzNewSaleHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}, &zzMockPaymentRecorder{}, &zzMockPaymentRepo{}))
		body := map[string]any{"items": []map[string]any{zzValidSaleItem()}}
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/sale-bills", body, uuid.New().String(), "")
		if w.Code < 500 {
			t.Errorf("status = %d, want >=500; body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("creator falls back to tenant when subject absent", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		tenantID := uuid.New()
		r := zzNewSaleRouter(zzNewSaleHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}, &zzMockPaymentRecorder{}, &zzMockPaymentRepo{}))
		body := map[string]any{"items": []map[string]any{zzValidSaleItem()}}
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/sale-bills", body, tenantID.String(), "")
		zzWantStatus(t, w, http.StatusCreated)
		resp := zzDecodeJSON(t, w)
		billID, _ := uuid.Parse(resp["bill_id"].(string))
		if repo.bills[billID].CreatorID != tenantID {
			t.Errorf("CreatorID = %v, want fallback tenantID %v", repo.bills[billID].CreatorID, tenantID)
		}
	})

	t.Run("creator trusts only IDP subject, spoofed X-User-ID header has no effect", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		tenantID := uuid.New()
		subjectID := uuid.New()
		spoofID := uuid.New()
		r := zzNewSaleRouter(zzNewSaleHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}, &zzMockPaymentRecorder{}, &zzMockPaymentRepo{}))

		body := map[string]any{"items": []map[string]any{zzValidSaleItem()}}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/sale-bills", bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Tenant-ID", tenantID.String())
		req.Header.Set("X-IDP-Sub", subjectID.String())
		req.Header.Set("X-User-ID", spoofID.String()) // production handler never reads this header
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		zzWantStatus(t, w, http.StatusCreated)
		resp := zzDecodeJSON(t, w)
		billID, _ := uuid.Parse(resp["bill_id"].(string))
		head := repo.bills[billID]
		if head.CreatorID != subjectID {
			t.Errorf("CreatorID = %v, want trusted subject %v", head.CreatorID, subjectID)
		}
		if head.CreatorID == spoofID {
			t.Errorf("CreatorID must never equal the spoofed X-User-ID header value")
		}
	})
}

func TestZZ_SaleHandler_Update_NotImplemented(t *testing.T) {
	repo := zzNewMockBillRepo()
	r := zzNewSaleRouter(zzNewSaleHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}, &zzMockPaymentRecorder{}, &zzMockPaymentRepo{}))
	w := zzDoJSON(t, r, http.MethodPut, "/api/v1/sale-bills/"+uuid.New().String(), map[string]any{}, uuid.New().String(), "")
	zzWantStatus(t, w, http.StatusNotImplemented)
	resp := zzDecodeJSON(t, w)
	if resp["error"] != "not_implemented" {
		t.Errorf("error = %v, want not_implemented", resp["error"])
	}
}

func TestZZ_SaleHandler_Approve(t *testing.T) {
	t.Run("tenant nil returns 401", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewSaleRouter(zzNewSaleHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}, &zzMockPaymentRecorder{}, &zzMockPaymentRepo{}))
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/sale-bills/"+uuid.New().String()+"/approve", nil, "", "")
		zzWantStatus(t, w, http.StatusUnauthorized)
	})

	t.Run("invalid id returns 400", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewSaleRouter(zzNewSaleHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}, &zzMockPaymentRecorder{}, &zzMockPaymentRepo{}))
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/sale-bills/not-a-uuid/approve", nil, uuid.New().String(), "")
		zzWantStatus(t, w, http.StatusBadRequest)
	})

	t.Run("invalid paid_amount returns 400", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewSaleRouter(zzNewSaleHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}, &zzMockPaymentRecorder{}, &zzMockPaymentRepo{}))
		body := map[string]any{"paid_amount": "not-a-number"}
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/sale-bills/"+uuid.New().String()+"/approve", body, uuid.New().String(), "")
		zzWantStatus(t, w, http.StatusBadRequest)
	})

	t.Run("bill not found returns 404", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewSaleRouter(zzNewSaleHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}, &zzMockPaymentRecorder{}, &zzMockPaymentRepo{}))
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/sale-bills/"+uuid.New().String()+"/approve", nil, uuid.New().String(), "")
		zzWantStatus(t, w, http.StatusNotFound)
	})

	t.Run("cancelled bill cannot transition to approved: 422", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		tenantID := uuid.New()
		billID := uuid.New()
		repo.bills[billID] = &domain.BillHead{ID: billID, TenantID: tenantID, Status: domain.StatusCancelled, BillType: domain.BillTypeSale}
		r := zzNewSaleRouter(zzNewSaleHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}, &zzMockPaymentRecorder{}, &zzMockPaymentRepo{}))
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/sale-bills/"+billID.String()+"/approve", nil, tenantID.String(), "")
		zzWantStatus(t, w, http.StatusUnprocessableEntity)
	})

	t.Run("concurrent approval conflict returns 409", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		repo.acquireLockErr = errors.New("advisory lock busy")
		tenantID := uuid.New()
		billID := uuid.New()
		repo.bills[billID] = &domain.BillHead{ID: billID, TenantID: tenantID, Status: domain.StatusDraft, BillType: domain.BillTypeSale}
		r := zzNewSaleRouter(zzNewSaleHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}, &zzMockPaymentRecorder{}, &zzMockPaymentRepo{}))
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/sale-bills/"+billID.String()+"/approve", nil, tenantID.String(), "")
		zzWantStatus(t, w, http.StatusConflict)
	})

	t.Run("insufficient stock returns 422 with per-line details", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		tenantID := uuid.New()
		billID := uuid.New()
		productID := uuid.New()
		repo.bills[billID] = &domain.BillHead{ID: billID, TenantID: tenantID, Status: domain.StatusDraft, BillType: domain.BillTypeSale, TotalAmount: decimal.NewFromInt(100)}
		repo.items[billID] = []*domain.BillItem{{ID: uuid.New(), TenantID: tenantID, HeadID: billID, ProductID: productID, Qty: decimal.NewFromInt(5)}}
		stockUC := &zzMockStockUC{err: &appstock.InsufficientStockError{ProductID: productID, Available: decimal.Zero, Requested: decimal.NewFromInt(5)}}
		r := zzNewSaleRouter(zzNewSaleHandler(repo, stockUC, &zzMockUnitRepo{}, &zzMockPaymentRecorder{}, &zzMockPaymentRepo{}))
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/sale-bills/"+billID.String()+"/approve", nil, tenantID.String(), "")
		zzWantStatus(t, w, http.StatusUnprocessableEntity)
		resp := zzDecodeJSON(t, w)
		if resp["error"] != "insufficient_stock" {
			t.Errorf("error = %v, want insufficient_stock", resp["error"])
		}
		details, ok := resp["details"].([]any)
		if !ok || len(details) != 1 {
			t.Fatalf("details = %v, want array of length 1", resp["details"])
		}
	})

	t.Run("internal error (GetBillItems failure) returns 5xx", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		tenantID := uuid.New()
		billID := uuid.New()
		repo.bills[billID] = &domain.BillHead{ID: billID, TenantID: tenantID, Status: domain.StatusDraft, BillType: domain.BillTypeSale}
		repo.getBillItemsErr = errors.New("read failure")
		r := zzNewSaleRouter(zzNewSaleHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}, &zzMockPaymentRecorder{}, &zzMockPaymentRepo{}))
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/sale-bills/"+billID.String()+"/approve", nil, tenantID.String(), "")
		if w.Code < 500 {
			t.Errorf("status = %d, want >=500; body: %s", w.Code, w.Body.String())
		}
	})
}

func TestZZ_SaleHandler_Cancel(t *testing.T) {
	t.Run("tenant nil returns 401", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewSaleRouter(zzNewSaleHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}, &zzMockPaymentRecorder{}, &zzMockPaymentRepo{}))
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/sale-bills/"+uuid.New().String()+"/cancel", nil, "", "")
		zzWantStatus(t, w, http.StatusUnauthorized)
	})

	t.Run("invalid id returns 400", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewSaleRouter(zzNewSaleHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}, &zzMockPaymentRecorder{}, &zzMockPaymentRepo{}))
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/sale-bills/not-a-uuid/cancel", nil, uuid.New().String(), "")
		zzWantStatus(t, w, http.StatusBadRequest)
	})

	t.Run("bill not found returns 404", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewSaleRouter(zzNewSaleHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}, &zzMockPaymentRecorder{}, &zzMockPaymentRepo{}))
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/sale-bills/"+uuid.New().String()+"/cancel", nil, uuid.New().String(), "")
		zzWantStatus(t, w, http.StatusNotFound)
	})

	t.Run("approved bill cannot be cancelled directly: 422", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		tenantID := uuid.New()
		billID := uuid.New()
		repo.bills[billID] = &domain.BillHead{ID: billID, TenantID: tenantID, Status: domain.StatusApproved, BillType: domain.BillTypeSale}
		r := zzNewSaleRouter(zzNewSaleHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}, &zzMockPaymentRecorder{}, &zzMockPaymentRepo{}))
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/sale-bills/"+billID.String()+"/cancel", nil, tenantID.String(), "")
		zzWantStatus(t, w, http.StatusUnprocessableEntity)
		resp := zzDecodeJSON(t, w)
		if resp["error"] != "cannot_cancel_approved_bill" {
			t.Errorf("error = %v, want cannot_cancel_approved_bill", resp["error"])
		}
	})

	t.Run("draft bill cancels successfully: 200", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		tenantID := uuid.New()
		billID := uuid.New()
		repo.bills[billID] = &domain.BillHead{ID: billID, TenantID: tenantID, Status: domain.StatusDraft, BillType: domain.BillTypeSale}
		r := zzNewSaleRouter(zzNewSaleHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}, &zzMockPaymentRecorder{}, &zzMockPaymentRepo{}))
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/sale-bills/"+billID.String()+"/cancel", nil, tenantID.String(), "")
		zzWantStatus(t, w, http.StatusOK)
	})

	t.Run("internal error returns 5xx", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		repo.getBillForUpdateErr = errors.New("connection reset")
		r := zzNewSaleRouter(zzNewSaleHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}, &zzMockPaymentRecorder{}, &zzMockPaymentRepo{}))
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/sale-bills/"+uuid.New().String()+"/cancel", nil, uuid.New().String(), "")
		if w.Code < 500 {
			t.Errorf("status = %d, want >=500; body: %s", w.Code, w.Body.String())
		}
	})
}

func TestZZ_SaleHandler_List(t *testing.T) {
	t.Run("tenant nil returns 401", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewSaleRouter(zzNewSaleHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}, &zzMockPaymentRecorder{}, &zzMockPaymentRepo{}))
		w := zzDoJSON(t, r, http.MethodGet, "/api/v1/sale-bills", nil, "", "")
		zzWantStatus(t, w, http.StatusUnauthorized)
	})

	t.Run("numeric status and valid partner_id set filters: 200", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewSaleRouter(zzNewSaleHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}, &zzMockPaymentRecorder{}, &zzMockPaymentRepo{}))
		w := zzDoJSON(t, r, http.MethodGet, "/api/v1/sale-bills?status=2&partner_id="+uuid.New().String(), nil, uuid.New().String(), "")
		zzWantStatus(t, w, http.StatusOK)
	})

	t.Run("non-numeric status and invalid partner_id are ignored: 200", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewSaleRouter(zzNewSaleHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}, &zzMockPaymentRecorder{}, &zzMockPaymentRepo{}))
		w := zzDoJSON(t, r, http.MethodGet, "/api/v1/sale-bills?status=abc&partner_id=not-a-uuid", nil, uuid.New().String(), "")
		zzWantStatus(t, w, http.StatusOK)
	})

	t.Run("internal error returns 5xx", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		repo.listErr = errors.New("db down")
		r := zzNewSaleRouter(zzNewSaleHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}, &zzMockPaymentRecorder{}, &zzMockPaymentRepo{}))
		w := zzDoJSON(t, r, http.MethodGet, "/api/v1/sale-bills", nil, uuid.New().String(), "")
		if w.Code < 500 {
			t.Errorf("status = %d, want >=500; body: %s", w.Code, w.Body.String())
		}
	})
}

func TestZZ_SaleHandler_Get(t *testing.T) {
	t.Run("tenant nil returns 401", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewSaleRouter(zzNewSaleHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}, &zzMockPaymentRecorder{}, &zzMockPaymentRepo{}))
		w := zzDoJSON(t, r, http.MethodGet, "/api/v1/sale-bills/"+uuid.New().String(), nil, "", "")
		zzWantStatus(t, w, http.StatusUnauthorized)
	})

	t.Run("invalid id returns 400", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewSaleRouter(zzNewSaleHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}, &zzMockPaymentRecorder{}, &zzMockPaymentRepo{}))
		w := zzDoJSON(t, r, http.MethodGet, "/api/v1/sale-bills/not-a-uuid", nil, uuid.New().String(), "")
		zzWantStatus(t, w, http.StatusBadRequest)
	})

	t.Run("bill not found returns 404", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewSaleRouter(zzNewSaleHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}, &zzMockPaymentRecorder{}, &zzMockPaymentRepo{}))
		w := zzDoJSON(t, r, http.MethodGet, "/api/v1/sale-bills/"+uuid.New().String(), nil, uuid.New().String(), "")
		zzWantStatus(t, w, http.StatusNotFound)
	})

	t.Run("GetBillItems failure returns 5xx", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		tenantID := uuid.New()
		billID := uuid.New()
		repo.bills[billID] = &domain.BillHead{ID: billID, TenantID: tenantID, Status: domain.StatusDraft, BillType: domain.BillTypeSale}
		repo.getBillItemsErr = errors.New("read failure")
		r := zzNewSaleRouter(zzNewSaleHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}, &zzMockPaymentRecorder{}, &zzMockPaymentRepo{}))
		w := zzDoJSON(t, r, http.MethodGet, "/api/v1/sale-bills/"+billID.String(), nil, tenantID.String(), "")
		if w.Code < 500 {
			t.Errorf("status = %d, want >=500; body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("listPaymentsUC failure returns 5xx", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		tenantID := uuid.New()
		billID := uuid.New()
		repo.bills[billID] = &domain.BillHead{ID: billID, TenantID: tenantID, Status: domain.StatusDraft, BillType: domain.BillTypeSale}
		repo.items[billID] = []*domain.BillItem{}
		payRepo := &zzMockPaymentRepo{listErr: errors.New("payments db down")}
		r := zzNewSaleRouter(zzNewSaleHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}, &zzMockPaymentRecorder{}, payRepo))
		w := zzDoJSON(t, r, http.MethodGet, "/api/v1/sale-bills/"+billID.String(), nil, tenantID.String(), "")
		if w.Code < 500 {
			t.Errorf("status = %d, want >=500; body: %s", w.Code, w.Body.String())
		}
	})
}

func TestZZ_SaleHandler_QuickCheckout(t *testing.T) {
	t.Run("tenant nil returns 401", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewSaleRouter(zzNewSaleHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}, &zzMockPaymentRecorder{}, &zzMockPaymentRepo{}))
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/sale-bills/quick-checkout", map[string]any{"items": []map[string]any{zzValidSaleItem()}}, "", "")
		zzWantStatus(t, w, http.StatusUnauthorized)
	})

	t.Run("malformed json returns 400", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewSaleRouter(zzNewSaleHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}, &zzMockPaymentRecorder{}, &zzMockPaymentRepo{}))
		w := zzDoRaw(t, r, http.MethodPost, "/api/v1/sale-bills/quick-checkout", "{bad", uuid.New().String())
		zzWantStatus(t, w, http.StatusBadRequest)
	})

	t.Run("empty items returns 400", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewSaleRouter(zzNewSaleHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}, &zzMockPaymentRecorder{}, &zzMockPaymentRepo{}))
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/sale-bills/quick-checkout", map[string]any{"items": []map[string]any{}}, uuid.New().String(), "")
		zzWantStatus(t, w, http.StatusBadRequest)
	})

	t.Run("bad item warehouse_id returns 400", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewSaleRouter(zzNewSaleHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}, &zzMockPaymentRecorder{}, &zzMockPaymentRepo{}))
		body := map[string]any{"items": []map[string]any{{"product_id": uuid.New().String(), "warehouse_id": "nope", "qty": "1"}}}
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/sale-bills/quick-checkout", body, uuid.New().String(), "")
		zzWantStatus(t, w, http.StatusBadRequest)
	})

	t.Run("usecase ErrValidation (cross-tenant product) returns 400", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		repo.productExists = false
		r := zzNewSaleRouter(zzNewSaleHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}, &zzMockPaymentRecorder{}, &zzMockPaymentRepo{}))
		body := map[string]any{"items": []map[string]any{zzValidSaleItem()}}
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/sale-bills/quick-checkout", body, uuid.New().String(), "")
		zzWantStatus(t, w, http.StatusBadRequest)
	})

	t.Run("insufficient stock returns 422 with details", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		productID := uuid.New()
		stockUC := &zzMockStockUC{err: &appstock.InsufficientStockError{ProductID: productID, Available: decimal.Zero, Requested: decimal.NewFromInt(2)}}
		r := zzNewSaleRouter(zzNewSaleHandler(repo, stockUC, &zzMockUnitRepo{}, &zzMockPaymentRecorder{}, &zzMockPaymentRepo{}))
		body := map[string]any{
			"payment_method": "cash",
			"paid_amount":    "0",
			"items": []map[string]any{
				{"product_id": productID.String(), "warehouse_id": uuid.New().String(), "qty": "2", "unit_price": "5"},
			},
		}
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/sale-bills/quick-checkout", body, uuid.New().String(), "")
		zzWantStatus(t, w, http.StatusUnprocessableEntity)
		resp := zzDecodeJSON(t, w)
		if resp["error"] != "insufficient_stock" {
			t.Errorf("error = %v, want insufficient_stock", resp["error"])
		}
	})

	t.Run("internal error returns 5xx", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		repo.nextBillNoErr = errors.New("db down")
		r := zzNewSaleRouter(zzNewSaleHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}, &zzMockPaymentRecorder{}, &zzMockPaymentRepo{}))
		body := map[string]any{"items": []map[string]any{zzValidSaleItem()}}
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/sale-bills/quick-checkout", body, uuid.New().String(), "")
		if w.Code < 500 {
			t.Errorf("status = %d, want >=500; body: %s", w.Code, w.Body.String())
		}
	})

	// Documents current (TODO-marked) behaviour: an unparsable paid_amount does
	// NOT surface as a 400 — it silently defaults to decimal.Zero. This test
	// asserts that documented behaviour directly (hand-computed expectation),
	// it does not read back the handler's own output as its oracle.
	t.Run("unparsable paid_amount silently defaults to zero (documented TODO)", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewSaleRouter(zzNewSaleHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}, &zzMockPaymentRecorder{}, &zzMockPaymentRepo{}))

		qty := decimal.RequireFromString("10")
		unitPrice := decimal.RequireFromString("9.99")
		wantTotal := qty.Mul(unitPrice).Round(4)
		wantReceivable := wantTotal // paid_amount defaults to zero, so receivable == total

		body := map[string]any{
			"paid_amount": "not-a-number",
			"items": []map[string]any{
				{"product_id": uuid.New().String(), "warehouse_id": uuid.New().String(), "qty": "10", "unit_price": "9.99"},
			},
		}
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/sale-bills/quick-checkout", body, uuid.New().String(), "")
		zzWantStatus(t, w, http.StatusCreated)
		resp := zzDecodeJSON(t, w)
		if resp["total_amount"] != wantTotal.String() {
			t.Errorf("total_amount = %v, want %v", resp["total_amount"], wantTotal.String())
		}
		if resp["receivable_amount"] != wantReceivable.String() {
			t.Errorf("receivable_amount = %v, want %v (paid_amount should have defaulted to zero)", resp["receivable_amount"], wantReceivable.String())
		}
	})

	t.Run("success returns 201", func(t *testing.T) {
		repo := zzNewMockBillRepo()
		r := zzNewSaleRouter(zzNewSaleHandler(repo, &zzMockStockUC{}, &zzMockUnitRepo{}, &zzMockPaymentRecorder{}, &zzMockPaymentRepo{}))
		body := map[string]any{
			"payment_method": "cash",
			"paid_amount":    "100",
			"items":          []map[string]any{zzValidSaleItem()},
		}
		w := zzDoJSON(t, r, http.MethodPost, "/api/v1/sale-bills/quick-checkout", body, uuid.New().String(), "")
		zzWantStatus(t, w, http.StatusCreated)
	})
}
