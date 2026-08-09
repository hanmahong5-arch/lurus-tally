package stock_test

// zz_coverage_test.go exercises the real stock.Handler (handler.go) end to end
// via gin.CreateTestContext + httptest — the pre-existing handler_test.go builds
// a parallel router with inline closures and never calls the actual Handler
// methods, which is why this package sat at 0% statement coverage despite
// having a test file. These tests wire real *appstock.RecordMovementUseCase /
// *appstock.GetSnapshotUseCase / *appstock.ListSnapshotsUseCase /
// *appstock.ListMovementsUseCase / *appreplenish.ListLowStockUseCase against
// small in-memory fakes, matching the concrete-struct constraints of
// stock.Handler.New.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/stock"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appreplenish "github.com/hanmahong5-arch/lurus-tally/internal/app/replenish"
	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// ---- fake StockRepo -------------------------------------------------------

// fakeStockRepo implements appstock.StockRepo entirely in memory. Transactions
// are no-ops (nil *sql.Tx is accepted by every method) so the handler tests
// never need a real database.
type fakeStockRepo struct {
	snapshot         *domain.Snapshot
	snapshots        []domain.Snapshot
	movements        []domain.Movement
	getSnapshotErr   error
	listSnapshotsErr error
	listMovementsErr error
	selectForUpdate  *domain.Snapshot
}

func (f *fakeStockRepo) GetSnapshot(_ context.Context, _, _, _ uuid.UUID) (*domain.Snapshot, error) {
	return f.snapshot, f.getSnapshotErr
}

func (f *fakeStockRepo) SelectForUpdate(_ context.Context, _ *sql.Tx, _, _, _ uuid.UUID) (*domain.Snapshot, error) {
	return f.selectForUpdate, nil
}

func (f *fakeStockRepo) UpsertSnapshot(_ context.Context, _ *sql.Tx, s *domain.Snapshot) error {
	f.snapshot = s
	return nil
}

func (f *fakeStockRepo) InsertMovement(_ context.Context, _ *sql.Tx, m *domain.Movement) error {
	f.movements = append(f.movements, *m)
	return nil
}

func (f *fakeStockRepo) ListMovements(_ context.Context, _ appstock.MovementFilter) ([]domain.Movement, error) {
	return f.movements, f.listMovementsErr
}

func (f *fakeStockRepo) ListSnapshots(_ context.Context, _ appstock.ListSnapshotsFilter) ([]domain.Snapshot, error) {
	return f.snapshots, f.listSnapshotsErr
}

func (f *fakeStockRepo) InsertLot(_ context.Context, _ *sql.Tx, _ *domain.Lot) error { return nil }

func (f *fakeStockRepo) ListActiveLots(_ context.Context, _ *sql.Tx, _, _, _ uuid.UUID) ([]domain.Lot, error) {
	return nil, nil
}

func (f *fakeStockRepo) UpdateLotQty(_ context.Context, _ *sql.Tx, _ uuid.UUID, _ decimal.Decimal) error {
	return nil
}

func (f *fakeStockRepo) AcquireAdvisoryLock(_ context.Context, _ *sql.Tx, _, _, _ uuid.UUID) error {
	return nil
}

func (f *fakeStockRepo) WithTx(_ context.Context, fn func(tx *sql.Tx) error) error {
	return fn(nil)
}

// ---- fake InventoryCalculator ---------------------------------------------

// fakeCalculator lets each test dictate exactly what ValidateMovement /
// ApplyMovement return, so RecordMovementUseCase.Execute's error branches
// (InsufficientStockError vs generic error vs success) are all reachable
// through the real handler.PostMovement code path.
type fakeCalculator struct {
	validateErr error
	applySnap   *domain.Snapshot
	applyErr    error
}

func (c *fakeCalculator) ValidateMovement(_ context.Context, _ *sql.Tx, _ *domain.Movement) error {
	return c.validateErr
}

func (c *fakeCalculator) ApplyMovement(_ context.Context, _ *sql.Tx, m *domain.Movement) (*domain.Snapshot, error) {
	if c.applyErr != nil {
		return nil, c.applyErr
	}
	if c.applySnap != nil {
		return c.applySnap, nil
	}
	return &domain.Snapshot{
		ID:          uuid.New(),
		TenantID:    m.TenantID,
		ProductID:   m.ProductID,
		WarehouseID: m.WarehouseID,
		OnHandQty:   m.QtyBase,
		UnitCost:    m.UnitCost,
	}, nil
}

func (c *fakeCalculator) Name() string { return "fake" }

// ---- fake SuggestionRepo (for ListLowStockUseCase) -------------------------

type fakeSuggestionRepo struct {
	rows []appreplenish.RawRow
	err  error
}

func (f *fakeSuggestionRepo) ListSuggestions(_ context.Context, _ uuid.UUID) ([]appreplenish.RawRow, error) {
	return f.rows, f.err
}

// ---- test harness -----------------------------------------------------

// buildHandler wires a real stock.Handler using the fakes above. Any argument
// left nil gets a zero-value fake so every route is independently testable
// without constructing unrelated use cases.
type harnessOpts struct {
	repo           *fakeStockRepo
	calc           *fakeCalculator
	suggestionRepo *appreplenish.SuggestionRepo // unused placeholder to keep signature simple
}

func buildHandler(repo *fakeStockRepo, calc *fakeCalculator, sugRepo appreplenish.SuggestionRepo) *stock.Handler {
	if repo == nil {
		repo = &fakeStockRepo{}
	}
	if calc == nil {
		calc = &fakeCalculator{}
	}
	if sugRepo == nil {
		sugRepo = &fakeSuggestionRepo{}
	}

	record := appstock.NewRecordMovementUseCase(repo, calc, nil, nil)
	getSnap := appstock.NewGetSnapshotUseCase(repo)
	listSnaps := appstock.NewListSnapshotsUseCase(repo)
	listMvs := appstock.NewListMovementsUseCase(repo)
	listLow := appreplenish.NewListLowStockUseCase(sugRepo)

	return stock.New(record, getSnap, listSnaps, listMvs, listLow)
}

// newCtx builds a gin.Context + ResponseRecorder for a request, optionally
// injecting the tenant ID the way AuthMiddleware does (c.Set with the real
// context key), so resolveTenantID (middleware.GetTenantID) sees it.
func newCtx(method, target string, body []byte, tenantID *uuid.UUID, params gin.Params) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, target, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	c.Request = req
	c.Params = params

	if tenantID != nil {
		c.Set(middleware.CtxKeyTenantID, *tenantID)
	}
	return c, w
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if w.Body.Len() == 0 {
		return m
	}
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode body: %v; raw=%s", err, w.Body.String())
	}
	return m
}

// ============================================================================
// ListLowStock
// ============================================================================

func TestListLowStock_NoTenant_Returns401(t *testing.T) {
	h := buildHandler(nil, nil, nil)
	c, w := newCtx(http.MethodGet, "/api/v1/stock/alerts/low-stock", nil, nil, nil)

	h.ListLowStock(c)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", w.Code, w.Body.String())
	}
}

func TestListLowStock_NilRows_SerialisesEmptyArrayNotNull(t *testing.T) {
	// business_invariant: nil rows from the use case must serialize as
	// items:[] count:0, never null (handler.go:73-76).
	tenantID := uuid.New()
	// fakeSuggestionRepo with zero rows -> ListLowStockUseCase.Execute returns
	// an empty (non-nil per its own construction) slice; we still assert the
	// wire contract holds regardless of the use case's internal nil-vs-empty
	// choice, since the handler explicitly guards for a nil result too.
	h := buildHandler(nil, nil, &fakeSuggestionRepo{rows: nil})
	c, w := newCtx(http.MethodGet, "/api/v1/stock/alerts/low-stock", nil, &tenantID, nil)

	h.ListLowStock(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"items":[]`)) {
		t.Errorf("body must contain items:[] literal, got %s", w.Body.String())
	}
	resp := decodeBody(t, w)
	if resp["count"] != float64(0) {
		t.Errorf("count = %v, want 0", resp["count"])
	}
	items, ok := resp["items"].([]any)
	if !ok {
		t.Fatalf("items missing or wrong type: %v", resp)
	}
	if len(items) != 0 {
		t.Errorf("items len = %d, want 0", len(items))
	}
}

func TestListLowStock_UseCaseError_Returns500(t *testing.T) {
	tenantID := uuid.New()
	h := buildHandler(nil, nil, &fakeSuggestionRepo{err: sql.ErrConnDone})
	c, w := newCtx(http.MethodGet, "/api/v1/stock/alerts/low-stock?limit=10", nil, &tenantID, nil)

	h.ListLowStock(c)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// ============================================================================
// PostMovement
// ============================================================================

func TestPostMovement_NoTenant_Returns401(t *testing.T) {
	h := buildHandler(nil, nil, nil)
	body := []byte(`{}`)
	c, w := newCtx(http.MethodPost, "/api/v1/stock/movements", body, nil, nil)

	h.PostMovement(c)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", w.Code, w.Body.String())
	}
}

func TestPostMovement_InvalidJSON_Returns400(t *testing.T) {
	tenantID := uuid.New()
	h := buildHandler(nil, nil, nil)
	c, w := newCtx(http.MethodPost, "/api/v1/stock/movements", []byte(`{not-json`), &tenantID, nil)

	h.PostMovement(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	resp := decodeBody(t, w)
	errMsg, _ := resp["error"].(string)
	if !bytes.Contains([]byte(errMsg), []byte("invalid request body")) {
		t.Errorf("error = %q, want prefix 'invalid request body'", errMsg)
	}
}

// validPostBody returns a map with all required fields set to valid values;
// callers override individual keys per test case.
func validPostBody(productID, warehouseID uuid.UUID) map[string]any {
	return map[string]any{
		"product_id":     productID,
		"warehouse_id":   warehouseID,
		"direction":      "in",
		"qty":            "5",
		"reference_type": "purchase",
	}
}

func TestPostMovement_ZeroQty_Returns400(t *testing.T) {
	tenantID := uuid.New()
	h := buildHandler(nil, nil, nil)
	body := validPostBody(uuid.New(), uuid.New())
	body["qty"] = "0"
	b, _ := json.Marshal(body)
	c, w := newCtx(http.MethodPost, "/api/v1/stock/movements", b, &tenantID, nil)

	h.PostMovement(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	resp := decodeBody(t, w)
	if resp["error"] != "invalid qty: must be a non-zero decimal" {
		t.Errorf("error = %v, want 'invalid qty: must be a non-zero decimal'", resp["error"])
	}
}

func TestPostMovement_UnparseableQty_Returns400(t *testing.T) {
	tenantID := uuid.New()
	h := buildHandler(nil, nil, nil)
	body := validPostBody(uuid.New(), uuid.New())
	body["qty"] = "not-a-number"
	b, _ := json.Marshal(body)
	c, w := newCtx(http.MethodPost, "/api/v1/stock/movements", b, &tenantID, nil)

	h.PostMovement(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

func TestPostMovement_UnparseableUnitCost_Returns400(t *testing.T) {
	tenantID := uuid.New()
	h := buildHandler(nil, nil, nil)
	body := validPostBody(uuid.New(), uuid.New())
	body["unit_cost"] = "garbage"
	b, _ := json.Marshal(body)
	c, w := newCtx(http.MethodPost, "/api/v1/stock/movements", b, &tenantID, nil)

	h.PostMovement(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	resp := decodeBody(t, w)
	if resp["error"] != "invalid unit_cost: must be a decimal" {
		t.Errorf("error = %v, want 'invalid unit_cost: must be a decimal'", resp["error"])
	}
}

func TestPostMovement_InvalidDirection_Returns400(t *testing.T) {
	tenantID := uuid.New()
	h := buildHandler(nil, nil, nil)
	body := validPostBody(uuid.New(), uuid.New())
	body["direction"] = "sideways"
	b, _ := json.Marshal(body)
	c, w := newCtx(http.MethodPost, "/api/v1/stock/movements", b, &tenantID, nil)

	h.PostMovement(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	resp := decodeBody(t, w)
	if resp["error"] != "invalid direction: must be in|out|adjust" {
		t.Errorf("error = %v, want direction validation message", resp["error"])
	}
}

func TestPostMovement_Success_Returns201(t *testing.T) {
	tenantID := uuid.New()
	productID := uuid.New()
	warehouseID := uuid.New()
	h := buildHandler(&fakeStockRepo{}, &fakeCalculator{}, nil)
	body := validPostBody(productID, warehouseID)
	body["unit_cost"] = "12.50"
	b, _ := json.Marshal(body)
	c, w := newCtx(http.MethodPost, "/api/v1/stock/movements", b, &tenantID, nil)

	h.PostMovement(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", w.Code, w.Body.String())
	}
	var snap domain.Snapshot
	if err := json.Unmarshal(w.Body.Bytes(), &snap); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	if snap.ProductID != productID || snap.WarehouseID != warehouseID {
		t.Errorf("snapshot echo mismatch: got product=%s warehouse=%s", snap.ProductID, snap.WarehouseID)
	}
}

// business_invariant: an InsufficientStockError from the use case must yield
// HTTP 422 with error=insufficient_stock and the ProductID/Available/Requested
// echoed — oversell is blocked, not swallowed as 500 (handler.go:148-157).
func TestPostMovement_Oversell_Returns422WithDetails(t *testing.T) {
	tenantID := uuid.New()
	productID := uuid.New()
	warehouseID := uuid.New()

	ise := &appstock.InsufficientStockError{
		ProductID: productID,
		Available: decimal.NewFromInt(3),
		Requested: decimal.NewFromInt(100),
	}
	calc := &fakeCalculator{validateErr: ise}
	h := buildHandler(&fakeStockRepo{}, calc, nil)

	body := validPostBody(productID, warehouseID)
	body["direction"] = "out"
	body["qty"] = "100"
	body["reference_type"] = "sale"
	b, _ := json.Marshal(body)
	c, w := newCtx(http.MethodPost, "/api/v1/stock/movements", b, &tenantID, nil)

	h.PostMovement(c)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", w.Code, w.Body.String())
	}
	resp := decodeBody(t, w)
	if resp["error"] != "insufficient_stock" {
		t.Errorf("error = %v, want insufficient_stock", resp["error"])
	}
	if resp["product_id"] != productID.String() {
		t.Errorf("product_id = %v, want %s", resp["product_id"], productID.String())
	}
	if resp["available"] != "3" {
		t.Errorf("available = %v, want 3", resp["available"])
	}
	if resp["requested"] != "100" {
		t.Errorf("requested = %v, want 100", resp["requested"])
	}
}

// Any use-case error that is NOT an InsufficientStockError must fall through
// to httperr.WriteInternal (500), not be misclassified as 422.
func TestPostMovement_NonISEUseCaseError_Returns500(t *testing.T) {
	tenantID := uuid.New()
	productID := uuid.New()
	warehouseID := uuid.New()

	calc := &fakeCalculator{applyErr: sql.ErrTxDone}
	h := buildHandler(&fakeStockRepo{}, calc, nil)

	body := validPostBody(productID, warehouseID)
	b, _ := json.Marshal(body)
	c, w := newCtx(http.MethodPost, "/api/v1/stock/movements", b, &tenantID, nil)

	h.PostMovement(c)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", w.Code, w.Body.String())
	}
	resp := decodeBody(t, w)
	if resp["error"] == "insufficient_stock" {
		t.Errorf("non-ISE error must not be reported as insufficient_stock")
	}
}

// ============================================================================
// GetSnapshot
// ============================================================================

func TestGetSnapshot_NoTenant_Returns401(t *testing.T) {
	h := buildHandler(nil, nil, nil)
	c, w := newCtx(http.MethodGet, "/api/v1/stock/snapshots/x/y", nil, nil, gin.Params{
		{Key: "product_id", Value: uuid.New().String()},
		{Key: "warehouse_id", Value: uuid.New().String()},
	})

	h.GetSnapshot(c)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", w.Code, w.Body.String())
	}
}

func TestGetSnapshot_InvalidProductID_Returns400(t *testing.T) {
	tenantID := uuid.New()
	h := buildHandler(nil, nil, nil)
	c, w := newCtx(http.MethodGet, "/api/v1/stock/snapshots/not-a-uuid/y", nil, &tenantID, gin.Params{
		{Key: "product_id", Value: "not-a-uuid"},
		{Key: "warehouse_id", Value: uuid.New().String()},
	})

	h.GetSnapshot(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	resp := decodeBody(t, w)
	if resp["error"] != "invalid product_id" {
		t.Errorf("error = %v, want invalid product_id", resp["error"])
	}
}

func TestGetSnapshot_InvalidWarehouseID_Returns400(t *testing.T) {
	tenantID := uuid.New()
	h := buildHandler(nil, nil, nil)
	c, w := newCtx(http.MethodGet, "/api/v1/stock/snapshots/x/not-a-uuid", nil, &tenantID, gin.Params{
		{Key: "product_id", Value: uuid.New().String()},
		{Key: "warehouse_id", Value: "not-a-uuid"},
	})

	h.GetSnapshot(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	resp := decodeBody(t, w)
	if resp["error"] != "invalid warehouse_id" {
		t.Errorf("error = %v, want invalid warehouse_id", resp["error"])
	}
}

func TestGetSnapshot_NotFound_Returns404(t *testing.T) {
	tenantID := uuid.New()
	h := buildHandler(&fakeStockRepo{snapshot: nil}, nil, nil)
	c, w := newCtx(http.MethodGet, "/api/v1/stock/snapshots/x/y", nil, &tenantID, gin.Params{
		{Key: "product_id", Value: uuid.New().String()},
		{Key: "warehouse_id", Value: uuid.New().String()},
	})

	h.GetSnapshot(c)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", w.Code, w.Body.String())
	}
	resp := decodeBody(t, w)
	if resp["error"] != "snapshot not found" {
		t.Errorf("error = %v, want 'snapshot not found'", resp["error"])
	}
}

func TestGetSnapshot_Found_Returns200(t *testing.T) {
	tenantID := uuid.New()
	productID := uuid.New()
	warehouseID := uuid.New()
	snap := &domain.Snapshot{ID: uuid.New(), TenantID: tenantID, ProductID: productID, WarehouseID: warehouseID, OnHandQty: decimal.NewFromInt(42)}
	h := buildHandler(&fakeStockRepo{snapshot: snap}, nil, nil)
	c, w := newCtx(http.MethodGet, "/api/v1/stock/snapshots/x/y", nil, &tenantID, gin.Params{
		{Key: "product_id", Value: productID.String()},
		{Key: "warehouse_id", Value: warehouseID.String()},
	})

	h.GetSnapshot(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
}

func TestGetSnapshot_UseCaseError_Returns500(t *testing.T) {
	tenantID := uuid.New()
	h := buildHandler(&fakeStockRepo{getSnapshotErr: sql.ErrConnDone}, nil, nil)
	c, w := newCtx(http.MethodGet, "/api/v1/stock/snapshots/x/y", nil, &tenantID, gin.Params{
		{Key: "product_id", Value: uuid.New().String()},
		{Key: "warehouse_id", Value: uuid.New().String()},
	})

	h.GetSnapshot(c)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// ============================================================================
// ListSnapshots
// ============================================================================

func TestListSnapshots_NoTenant_Returns401(t *testing.T) {
	h := buildHandler(nil, nil, nil)
	c, w := newCtx(http.MethodGet, "/api/v1/stock/snapshots", nil, nil, nil)

	h.ListSnapshots(c)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", w.Code, w.Body.String())
	}
}

func TestListSnapshots_Success_Returns200(t *testing.T) {
	tenantID := uuid.New()
	repo := &fakeStockRepo{snapshots: []domain.Snapshot{
		{ID: uuid.New(), TenantID: tenantID, OnHandQty: decimal.NewFromInt(10)},
	}}
	h := buildHandler(repo, nil, nil)
	c, w := newCtx(http.MethodGet, "/api/v1/stock/snapshots", nil, &tenantID, nil)

	h.ListSnapshots(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	resp := decodeBody(t, w)
	items, ok := resp["items"].([]any)
	if !ok || len(items) != 1 {
		t.Errorf("items = %v, want len 1 slice", resp["items"])
	}
}

// business_invariant: an invalid product_id/warehouse_id query string must be
// silently ignored (uuid.Parse err path leaves the filter field zero-value) —
// it must NOT 400. This asserts handler.go's ListSnapshots query-parsing
// behaviour, which differs from GetSnapshot's path-param parsing (which does
// 400 on invalid UUIDs).
func TestListSnapshots_InvalidProductIDQuery_IgnoredNot400(t *testing.T) {
	tenantID := uuid.New()
	h := buildHandler(&fakeStockRepo{}, nil, nil)
	c, w := newCtx(http.MethodGet, "/api/v1/stock/snapshots?product_id=not-a-uuid&warehouse_id=also-bad", nil, &tenantID, nil)
	c.Request.URL.RawQuery = "product_id=not-a-uuid&warehouse_id=also-bad"

	h.ListSnapshots(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (invalid uuid query silently ignored); body=%s", w.Code, w.Body.String())
	}
}

func TestListSnapshots_UseCaseError_Returns500(t *testing.T) {
	tenantID := uuid.New()
	h := buildHandler(&fakeStockRepo{listSnapshotsErr: sql.ErrConnDone}, nil, nil)
	c, w := newCtx(http.MethodGet, "/api/v1/stock/snapshots", nil, &tenantID, nil)

	h.ListSnapshots(c)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// ============================================================================
// ListMovements
// ============================================================================

func TestListMovements_NoTenant_Returns401(t *testing.T) {
	h := buildHandler(nil, nil, nil)
	c, w := newCtx(http.MethodGet, "/api/v1/stock/movements", nil, nil, nil)

	h.ListMovements(c)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", w.Code, w.Body.String())
	}
}

func TestListMovements_Success_Returns200(t *testing.T) {
	tenantID := uuid.New()
	repo := &fakeStockRepo{movements: []domain.Movement{
		{ID: uuid.New(), TenantID: tenantID, Direction: domain.DirectionIn},
	}}
	h := buildHandler(repo, nil, nil)
	c, w := newCtx(http.MethodGet, "/api/v1/stock/movements", nil, &tenantID, nil)

	h.ListMovements(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	resp := decodeBody(t, w)
	items, ok := resp["items"].([]any)
	if !ok || len(items) != 1 {
		t.Errorf("items = %v, want len 1 slice", resp["items"])
	}
}

// Same invariant as ListSnapshots but for the ListMovements query params.
func TestListMovements_InvalidWarehouseIDQuery_IgnoredNot400(t *testing.T) {
	tenantID := uuid.New()
	h := buildHandler(&fakeStockRepo{}, nil, nil)
	c, w := newCtx(http.MethodGet, "/api/v1/stock/movements", nil, &tenantID, nil)
	c.Request.URL.RawQuery = "product_id=nope&warehouse_id=nope-too"

	h.ListMovements(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (invalid uuid query silently ignored); body=%s", w.Code, w.Body.String())
	}
}

func TestListMovements_UseCaseError_Returns500(t *testing.T) {
	tenantID := uuid.New()
	h := buildHandler(&fakeStockRepo{listMovementsErr: sql.ErrConnDone}, nil, nil)
	c, w := newCtx(http.MethodGet, "/api/v1/stock/movements", nil, &tenantID, nil)

	h.ListMovements(c)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// ============================================================================
// RegisterRoutes — confirms routes are mounted on the real handler (drives
// the remaining line coverage for the routing table itself).
// ============================================================================

func TestRegisterRoutes_MountsExpectedPaths(t *testing.T) {
	h := buildHandler(nil, nil, nil)
	r := gin.New()
	rg := r.Group("/api/v1")
	h.RegisterRoutes(rg)

	tenantID := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/stock/snapshots", nil)
	// RegisterRoutes wires through gin's normal routing (no test-injected
	// tenant), so we expect 401 here — this proves the route is registered
	// and reaches the real handler rather than 404 (unregistered).
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code == http.StatusNotFound {
		t.Fatalf("GET /api/v1/stock/snapshots returned 404 — route not registered")
	}
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (no tenant injected via real gin routing); body=%s", w.Code, w.Body.String())
	}
	_ = tenantID // reserved for readability; not needed since no auth middleware is mounted here
}
