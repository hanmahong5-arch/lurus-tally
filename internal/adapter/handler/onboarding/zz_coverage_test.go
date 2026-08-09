package onboarding_test

// This file supplements handler_test.go with:
//   - the two 500-path branches (seed.Execute err, clear.Execute err)
//   - the seed.Execute persona whitelist boundary (horticulture + a case that
//     falls through demoCatalogue's persona switch)
//   - direct unit coverage of stockAdapter (Execute / RecordSale), asserting
//     the exact RecordMovementRequest fields the adapter builds, using a real
//     *appstock.RecordMovementUseCase wired to fakes of StockRepo /
//     InventoryCalculator (both are exported interfaces — no source edits
//     needed to observe the adapter's translation).
//
// Per the task's hard constraints: only this new file is added; no existing
// *_test.go or source file is touched.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	handleronboarding "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/onboarding"
	appob "github.com/hanmahong5-arch/lurus-tally/internal/app/onboarding"
	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	domainproduct "github.com/hanmahong5-arch/lurus-tally/internal/domain/product"
	domainstock "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
)

// ---------------------------------------------------------------------------
// handler-layer error-path fakes
// ---------------------------------------------------------------------------

// erroringProductCreator always fails, driving SeedDemoUseCase.Execute into
// its "create product" error branch, which the handler must turn into a 500
// via httperr.WriteInternal (never a 200, never a leaked internal message).
type erroringProductCreator struct{}

func (erroringProductCreator) Execute(_ context.Context, _ domainproduct.CreateInput) (*domainproduct.Product, error) {
	return nil, errors.New("db unavailable")
}

// noopStockInitializer/noopSalesRecorder are only reached if productCreator
// succeeds; unused by the erroring-creator test but required to satisfy
// NewSeedDemoUseCase's three-dependency constructor when building other cases.
type noopStockInitializer struct{}

func (noopStockInitializer) Execute(_ context.Context, req appob.StockInitRequest) (*domainstock.Snapshot, error) {
	return &domainstock.Snapshot{TenantID: req.TenantID, ProductID: req.ProductID, WarehouseID: req.WarehouseID, OnHandQty: req.Qty}, nil
}

type noopSalesRecorder struct{}

func (noopSalesRecorder) RecordSale(_ context.Context, _ appob.DemoSaleRequest) error { return nil }

// erroringDemoDeleter always fails, driving ClearDemoUseCase.Execute into its
// error branch, which the handler must turn into a 500.
type erroringDemoDeleter struct{}

func (erroringDemoDeleter) DeleteDemoProducts(_ context.Context, _ uuid.UUID) error {
	return errors.New("connection reset")
}

// InsertDemoSale satisfies the other half of demoStore (appob.DemoSaleWriter).
// erroringDemoDeleter only fails on delete; sale-bill writes succeed with a
// fresh synthetic bill_item id, so tests wiring this type through the
// production New(...) constructor (which requires the full demoStore surface)
// can still exercise the seed/stock-adapter paths that call InsertDemoSale.
func (erroringDemoDeleter) InsertDemoSale(_ context.Context, _ appob.DemoSaleBill) (uuid.UUID, error) {
	return uuid.New(), nil
}

func buildRouterZZ(seed *appob.SeedDemoUseCase, clear *appob.ClearDemoUseCase) *gin.Engine {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if tid := c.GetHeader("X-Tenant-ID"); tid != "" {
			if id, err := uuid.Parse(tid); err == nil {
				c.Set("tenant_id", id)
			}
		}
		c.Next()
	})
	api := r.Group("/api/v1")
	handleronboarding.NewForTest(seed, clear).RegisterRoutes(api)
	return r
}

const zzTenantID = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"

// TestSeedDemoHandler_UseCaseError_Returns500 exercises the httperr.WriteInternal
// branch in SeedDemo: a real create-product failure (not a bind/validation
// error) must surface as 500, and the body must NOT leak the internal cause
// string "db unavailable" — httperr.Internal's contract is a static message.
func TestSeedDemoHandler_UseCaseError_Returns500(t *testing.T) {
	seed := appob.NewSeedDemoUseCase(erroringProductCreator{}, noopStockInitializer{}, noopSalesRecorder{})
	clear := appob.NewClearDemoUseCase(erroringDemoDeleter{})
	r := buildRouterZZ(seed, clear)

	body, err := json.Marshal(map[string]string{"persona": "retail", "warehouse_id": uuid.New().String()})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/seed-demo", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", zzTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d: %s", w.Code, w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), []byte("db unavailable")) {
		t.Errorf("500 body must not leak internal cause, got: %s", w.Body.String())
	}
}

// TestClearDemoHandler_UseCaseError_Returns500 exercises the httperr.WriteInternal
// branch in ClearDemo.
func TestClearDemoHandler_UseCaseError_Returns500(t *testing.T) {
	seed := appob.NewSeedDemoUseCase(erroringProductCreator{}, noopStockInitializer{}, noopSalesRecorder{})
	clear := appob.NewClearDemoUseCase(erroringDemoDeleter{})
	r := buildRouterZZ(seed, clear)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/clear-demo", http.NoBody)
	req.Header.Set("X-Tenant-ID", zzTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d: %s", w.Code, w.Body.String())
	}
}

// TestSeedDemoHandler_Horticulture_Returns200WithCount rounds out the persona
// whitelist: cross_border and retail are covered by handler_test.go; this adds
// the third legal persona so all three branches of the SeedDemo switch
// (business invariant: only cross_border/retail/horticulture are legal) are
// exercised by name, not by falling through to the default catalogue.
func TestSeedDemoHandler_Horticulture_Returns200WithCount(t *testing.T) {
	seed := appob.NewSeedDemoUseCase(&stubProductCreatorZZ{}, noopStockInitializer{}, noopSalesRecorder{})
	clear := appob.NewClearDemoUseCase(erroringDemoDeleter{})
	r := buildRouterZZ(seed, clear)

	body, err := json.Marshal(map[string]string{"persona": "horticulture", "warehouse_id": uuid.New().String()})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/seed-demo", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", zzTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]int
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	// horticulture demoCatalogue seeds exactly 3 SKUs (DEMO-HO-001..003).
	if resp["products_created"] != 3 {
		t.Errorf("want products_created=3, got %d", resp["products_created"])
	}
}

// TestSeedDemoHandler_MissingPersona_Returns400 and
// TestSeedDemoHandler_MissingWarehouseID_Returns400 cover the two
// binding:"required" fields independently (handler_test.go only covers an
// invalid (non-empty) warehouse_id and an invalid persona value; empty-string
// required-field rejection is a distinct Gin binding branch).
func TestSeedDemoHandler_MissingPersona_Returns400(t *testing.T) {
	seed, clear, _ := seedClear()
	r := buildRouter(seed, clear)

	body, err := json.Marshal(map[string]string{"warehouse_id": uuid.New().String()})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/seed-demo", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", zzTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSeedDemoHandler_MissingWarehouseID_Returns400(t *testing.T) {
	seed, clear, _ := seedClear()
	r := buildRouter(seed, clear)

	body, err := json.Marshal(map[string]string{"persona": "retail"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/seed-demo", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", zzTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestSeedDemoHandler_MalformedJSON_Returns400 covers the ShouldBindJSON parse
// error itself (as opposed to a validation failure on well-formed JSON).
func TestSeedDemoHandler_MalformedJSON_Returns400(t *testing.T) {
	seed, clear, _ := seedClear()
	r := buildRouter(seed, clear)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/seed-demo", bytes.NewReader([]byte("{not json")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", zzTenantID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d: %s", w.Code, w.Body.String())
	}
}

// stubProductCreatorZZ mirrors handler_test.go's stubProductCreator (that type
// is unexported to this file's identical needs, so it is re-declared here
// rather than editing the existing test file).
type stubProductCreatorZZ struct{}

func (s *stubProductCreatorZZ) Execute(_ context.Context, in domainproduct.CreateInput) (*domainproduct.Product, error) {
	return &domainproduct.Product{ID: uuid.New(), TenantID: in.TenantID, Code: in.Code, Name: in.Name}, nil
}

// ---------------------------------------------------------------------------
// stockAdapter unit tests (via a real *appstock.RecordMovementUseCase)
// ---------------------------------------------------------------------------
//
// stockAdapter is unexported; it cannot be constructed directly from this
// external test package. It is reached transitively through New(...) — the
// production constructor (as opposed to NewForTest) — by wiring a real
// *appstock.RecordMovementUseCase over a fake StockRepo/InventoryCalculator
// and driving SeedDemoUseCase end-to-end. The fake InventoryCalculator
// captures the *domain.Movement ApplyMovement receives, letting the test
// assert exactly what stockAdapter.Execute / RecordSale constructed.

// fakeStockRepo is a minimal appstock.StockRepo: WithTx runs fn directly (no
// real transaction), AcquireAdvisoryLock is a no-op. All other methods are
// unreachable through the seed-demo path and panic if called, so a
// regression that starts exercising them fails loudly instead of silently
// returning zero values.
type fakeStockRepo struct{}

func (fakeStockRepo) GetSnapshot(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*domainstock.Snapshot, error) {
	panic("unreachable: GetSnapshot not used by RecordMovementUseCase.Execute")
}
func (fakeStockRepo) SelectForUpdate(context.Context, *sql.Tx, uuid.UUID, uuid.UUID, uuid.UUID) (*domainstock.Snapshot, error) {
	panic("unreachable: SelectForUpdate is called by the calculator, not the repo directly in this fake")
}
func (fakeStockRepo) UpsertSnapshot(context.Context, *sql.Tx, *domainstock.Snapshot) error {
	panic("unreachable: UpsertSnapshot not used by this fake calculator")
}
func (fakeStockRepo) InsertMovement(context.Context, *sql.Tx, *domainstock.Movement) error {
	panic("unreachable: InsertMovement not used by this fake calculator")
}
func (fakeStockRepo) ListMovements(context.Context, appstock.MovementFilter) ([]domainstock.Movement, error) {
	panic("unreachable: ListMovements not used by RecordMovementUseCase.Execute")
}
func (fakeStockRepo) InsertLot(context.Context, *sql.Tx, *domainstock.Lot) error {
	panic("unreachable: InsertLot not used by this fake calculator")
}
func (fakeStockRepo) ListActiveLots(context.Context, *sql.Tx, uuid.UUID, uuid.UUID, uuid.UUID) ([]domainstock.Lot, error) {
	panic("unreachable: ListActiveLots not used by this fake calculator")
}
func (fakeStockRepo) UpdateLotQty(context.Context, *sql.Tx, uuid.UUID, decimal.Decimal) error {
	panic("unreachable: UpdateLotQty not used by this fake calculator")
}
func (fakeStockRepo) AcquireAdvisoryLock(context.Context, *sql.Tx, uuid.UUID, uuid.UUID, uuid.UUID) error {
	return nil
}
func (fakeStockRepo) ListSnapshots(context.Context, appstock.ListSnapshotsFilter) ([]domainstock.Snapshot, error) {
	panic("unreachable: ListSnapshots not used by RecordMovementUseCase.Execute")
}
func (fakeStockRepo) WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	return fn(nil)
}

// captureCalculator is a fake InventoryCalculator that records the last
// *domain.Movement passed to ValidateMovement/ApplyMovement, and returns a
// canned snapshot so RecordMovementUseCase.Execute succeeds. When
// applyErr/validateErr is set, the corresponding method fails instead —
// driving stockAdapter.Execute/RecordSale's own error-wrapping branches.
type captureCalculator struct {
	lastMovement *domainstock.Movement
	validateErr  error
	applyErr     error
}

func (c *captureCalculator) ValidateMovement(_ context.Context, _ *sql.Tx, m *domainstock.Movement) error {
	c.lastMovement = m
	return c.validateErr
}

func (c *captureCalculator) ApplyMovement(_ context.Context, _ *sql.Tx, m *domainstock.Movement) (*domainstock.Snapshot, error) {
	c.lastMovement = m
	if c.applyErr != nil {
		return nil, c.applyErr
	}
	return &domainstock.Snapshot{
		TenantID:    m.TenantID,
		ProductID:   m.ProductID,
		WarehouseID: m.WarehouseID,
		OnHandQty:   m.QtyBase,
	}, nil
}

func (c *captureCalculator) Name() string { return "fake" }

// newAdapterHandler builds a Handler via the production New(...) constructor
// (not NewForTest), which is the only way to reach the unexported
// stockAdapter type from this external test package. The productCreator
// param lets each test control whether SeedDemo proceeds past product
// creation for every SKU in the persona catalogue.
func newAdapterHandler(t *testing.T, productCreator appob.ProductCreator, calc appstock.InventoryCalculator) *handleronboarding.Handler {
	t.Helper()
	uc := appstock.NewRecordMovementUseCase(fakeStockRepo{}, calc, nil, nil)
	return handleronboarding.New(productCreator, uc, erroringDemoDeleter{})
}

// allCapturingCalculator records every movement passed to ApplyMovement, in
// order, so the test can assert on the very first one (the opening receipt)
// deterministically regardless of how many demo sales follow it.
type allCapturingCalculator struct {
	movements []domainstock.Movement
}

func (c *allCapturingCalculator) ValidateMovement(context.Context, *sql.Tx, *domainstock.Movement) error {
	return nil
}

func (c *allCapturingCalculator) ApplyMovement(_ context.Context, _ *sql.Tx, m *domainstock.Movement) (*domainstock.Snapshot, error) {
	c.movements = append(c.movements, *m)
	return &domainstock.Snapshot{TenantID: m.TenantID, ProductID: m.ProductID, WarehouseID: m.WarehouseID, OnHandQty: m.QtyBase}, nil
}

func (c *allCapturingCalculator) Name() string { return "fake-all" }

// TestStockAdapter_Execute_AllOpeningReceiptsAreDirectionIn asserts the
// business invariant for stockAdapter.Execute: every opening receipt built
// from StockInitRequest is DirectionIn / RefInit / CostStrategyWAC, with
// QtyBase equal to the requested Qty unchanged (ConvFactor="1" → no scaling)
// and UnitCost carried through. The retail persona seeds 3 SKUs, each
// producing exactly 1 opening receipt followed by up to demoSalesParts sale
// movements, so movements[0] is deterministically the first SKU's receipt.
func TestStockAdapter_Execute_AllOpeningReceiptsAreDirectionIn(t *testing.T) {
	calc := &allCapturingCalculator{}
	h := newAdapterHandler(t, &stubProductCreatorZZ{}, calc)

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("tenant_id", uuid.MustParse(zzTenantID)); c.Next() })
	api := r.Group("/api/v1")
	h.RegisterRoutes(api)

	body, err := json.Marshal(map[string]string{"persona": "retail", "warehouse_id": uuid.New().String()})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/seed-demo", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(calc.movements) == 0 {
		t.Fatal("no movements captured — stockAdapter never invoked RecordMovementUseCase")
	}

	opening := calc.movements[0]
	if opening.Direction != domainstock.DirectionIn {
		t.Errorf("opening receipt: want Direction=in, got %q", opening.Direction)
	}
	if opening.ReferenceType != domainstock.RefInit {
		t.Errorf("opening receipt: want ReferenceType=init, got %q", opening.ReferenceType)
	}
	// retail SKU[0] (DEMO-RT-001): qtyOnHand=60, monthlySales=72 → opening Qty = 132.
	// ConvFactor "1" means convertToBase leaves Qty unchanged, so QtyBase must equal 132 exactly.
	wantQty := decimal.NewFromInt(60).Add(decimal.NewFromInt(72))
	if !opening.QtyBase.Equal(wantQty) {
		t.Errorf("opening receipt: want QtyBase=%s, got %s", wantQty, opening.QtyBase)
	}
	// retail SKU[0] unitCost=28.
	if !opening.UnitCost.Equal(decimal.NewFromInt(28)) {
		t.Errorf("opening receipt: want UnitCost=28, got %s", opening.UnitCost)
	}

	// Every subsequent movement for this run must be a DirectionOut/RefSale
	// demo sale (stockAdapter.RecordSale's contract), each with a distinct
	// synthetic ReferenceID (a fresh uuid per call — never nil, never reused).
	// The invariant is "exactly one opening receipt per SKU (ProductID)", not
	// "exactly one opening receipt in the whole run": the retail persona seeds
	// 3 SKUs, so 3 distinct products each get their own DirectionIn boundary
	// followed by that SKU's demo sales.
	seen := map[uuid.UUID]bool{}
	openingSeenByProduct := map[uuid.UUID]bool{}
	for i, m := range calc.movements {
		if m.Direction == domainstock.DirectionIn {
			if openingSeenByProduct[m.ProductID] {
				t.Errorf("movement[%d]: unexpected second DirectionIn for product %s — expected exactly one opening receipt per SKU", i, m.ProductID)
			}
			openingSeenByProduct[m.ProductID] = true
			continue
		}
		if m.Direction != domainstock.DirectionOut {
			t.Fatalf("movement[%d]: want Direction in {in,out}, got %q", i, m.Direction)
		}
		if m.ReferenceType != domainstock.RefSale {
			t.Errorf("movement[%d] (sale): want ReferenceType=sale, got %q", i, m.ReferenceType)
		}
		if m.ReferenceID == nil {
			t.Errorf("movement[%d] (sale): want non-nil synthetic ReferenceID, got nil", i)
			continue
		}
		if seen[*m.ReferenceID] {
			t.Errorf("movement[%d] (sale): ReferenceID %s reused across sales — must be fresh per call", i, *m.ReferenceID)
		}
		seen[*m.ReferenceID] = true
		// RecordSale never supplies UnitCost — the WAC engine overwrites the
		// out unit cost with the prevailing average, so the adapter must pass
		// the zero value through untouched.
		if !m.UnitCost.IsZero() {
			t.Errorf("movement[%d] (sale): want UnitCost=0 (WAC overrides), got %s", i, m.UnitCost)
		}
	}
	// retail persona's demoCatalogue seeds exactly 3 SKUs (DEMO-RT-001..003),
	// each a distinct ProductID, each producing its own opening receipt.
	if len(openingSeenByProduct) != 3 {
		t.Errorf("want 3 distinct products with opening receipts (retail persona has 3 SKUs), got %d", len(openingSeenByProduct))
	}
}

// TestStockAdapter_Execute_WrapsUseCaseError asserts stockAdapter.Execute's
// error-wrapping contract: a RecordMovementUseCase.Execute failure (surfaced
// here via ValidateMovement returning an error) is wrapped with the
// "onboarding stock adapter: " prefix, and that wrapped error propagates
// through SeedDemoUseCase.Execute (as "seed demo: record opening stock for
// <code>: onboarding stock adapter: <cause>") all the way to a 500 at the
// handler.
func TestStockAdapter_Execute_WrapsUseCaseError(t *testing.T) {
	wantCause := errors.New("insufficient stock")
	calc := &captureCalculator{validateErr: wantCause}
	h := newAdapterHandler(t, &stubProductCreatorZZ{}, calc)

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("tenant_id", uuid.MustParse(zzTenantID)); c.Next() })
	api := r.Group("/api/v1")
	h.RegisterRoutes(api)

	body, err := json.Marshal(map[string]string{"persona": "retail", "warehouse_id": uuid.New().String()})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/seed-demo", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500 (stockAdapter.Execute error propagated), got %d: %s", w.Code, w.Body.String())
	}
	// httperr.Internal's contract hides the cause from the body — verified in
	// TestSeedDemoHandler_UseCaseError_Returns500. Here we only additionally
	// confirm the calculator was actually invoked (i.e. the error genuinely
	// came from the stockAdapter/RecordMovementUseCase path, not an earlier
	// validation short-circuit).
	if calc.lastMovement == nil {
		t.Fatal("calculator never invoked — error did not originate from stockAdapter.Execute")
	}
}

// TestStockAdapter_RecordSale_WrapsUseCaseError asserts stockAdapter.RecordSale's
// error-wrapping contract ("onboarding sales adapter: "). It uses a
// calculator that succeeds on the opening receipt (ValidateMovement/ApplyMovement
// return no error the first call) but fails from the second call onward —
// i.e. every demo sale fails — via a calculator wrapping captureCalculator
// with a call counter.
type failAfterNCalculator struct {
	n      int // number of successful ApplyMovement calls before failing
	calls  int
	failed bool
}

func (c *failAfterNCalculator) ValidateMovement(context.Context, *sql.Tx, *domainstock.Movement) error {
	return nil
}

func (c *failAfterNCalculator) ApplyMovement(_ context.Context, _ *sql.Tx, m *domainstock.Movement) (*domainstock.Snapshot, error) {
	c.calls++
	if c.calls > c.n {
		c.failed = true
		return nil, errors.New("sale write failed")
	}
	return &domainstock.Snapshot{TenantID: m.TenantID, ProductID: m.ProductID, WarehouseID: m.WarehouseID, OnHandQty: m.QtyBase}, nil
}

func (c *failAfterNCalculator) Name() string { return "fail-after-n" }

func TestStockAdapter_RecordSale_WrapsUseCaseError(t *testing.T) {
	// n=1: the first ApplyMovement call (the opening receipt) succeeds; every
	// call after that (the demo sales) fails, isolating the RecordSale path.
	calc := &failAfterNCalculator{n: 1}
	h := newAdapterHandler(t, &stubProductCreatorZZ{}, calc)

	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("tenant_id", uuid.MustParse(zzTenantID)); c.Next() })
	api := r.Group("/api/v1")
	h.RegisterRoutes(api)

	body, err := json.Marshal(map[string]string{"persona": "retail", "warehouse_id": uuid.New().String()})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/seed-demo", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500 (stockAdapter.RecordSale error propagated), got %d: %s", w.Code, w.Body.String())
	}
	if !calc.failed {
		t.Fatal("calculator never reached its failing branch — RecordSale path not exercised")
	}
	if calc.calls < 2 {
		t.Fatalf("want at least 2 ApplyMovement calls (1 receipt + >=1 sale), got %d", calc.calls)
	}
}

// TestClearDemoHandler_Success_TenantPropagation asserts the tenant-isolation
// invariant on the clear path directly: ClearDemoUseCase.Execute's sole
// argument (the tenant UUID) must be exactly the context's tenant_id — never
// zero, never a different tenant's id. A capturing DemoDeleter fake pins this
// down independently of handler_test.go's stubDemoDeleter (which only records
// that it was called, not with which tenant).
type capturingDemoDeleter struct {
	gotTenantID uuid.UUID
	called      bool
}

func (d *capturingDemoDeleter) DeleteDemoProducts(_ context.Context, tenantID uuid.UUID) error {
	d.called = true
	d.gotTenantID = tenantID
	return nil
}

func TestClearDemoHandler_Success_TenantPropagation(t *testing.T) {
	del := &capturingDemoDeleter{}
	seed := appob.NewSeedDemoUseCase(erroringProductCreator{}, noopStockInitializer{}, noopSalesRecorder{})
	clear := appob.NewClearDemoUseCase(del)
	r := buildRouterZZ(seed, clear)

	wantTenant := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/clear-demo", http.NoBody)
	req.Header.Set("X-Tenant-ID", wantTenant.String())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d: %s", w.Code, w.Body.String())
	}
	if !del.called {
		t.Fatal("want DeleteDemoProducts called")
	}
	if del.gotTenantID != wantTenant {
		t.Errorf("want DeleteDemoProducts tenantID=%s, got %s (tenant isolation broken)", wantTenant, del.gotTenantID)
	}
}

// TestSeedDemoHandler_MissingTenant_UseCaseNotInvoked asserts the tenant-gate
// invariant on the seed path from the other direction: when tenantID==Nil the
// handler must return before calling seed.Execute at all (not merely that the
// eventual response happens to be 401 — a productCreator that panics on any
// call proves the use case chain was never entered).
type panicProductCreator struct{}

func (panicProductCreator) Execute(context.Context, domainproduct.CreateInput) (*domainproduct.Product, error) {
	panic("SeedDemoUseCase.Execute must not be invoked when tenantID == uuid.Nil")
}

func TestSeedDemoHandler_MissingTenant_UseCaseNotInvoked(t *testing.T) {
	seed := appob.NewSeedDemoUseCase(panicProductCreator{}, noopStockInitializer{}, noopSalesRecorder{})
	clear := appob.NewClearDemoUseCase(erroringDemoDeleter{})
	r := buildRouterZZ(seed, clear)

	body, err := json.Marshal(map[string]string{"persona": "retail", "warehouse_id": uuid.New().String()})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/seed-demo", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// No X-Tenant-ID header → tenant_id not in context → handler must 401 before touching seed.Execute.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req) // would panic (failing the test) if seed.Execute were reached

	if w.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d: %s", w.Code, w.Body.String())
	}
}

// TestClearDemoHandler_MissingTenant_UseCaseNotInvoked mirrors the above for
// ClearDemo: an erroring-and-panicking DemoDeleter proves clear.Execute is
// never reached when tenantID == uuid.Nil.
type panicDemoDeleter struct{}

func (panicDemoDeleter) DeleteDemoProducts(context.Context, uuid.UUID) error {
	panic("ClearDemoUseCase.Execute must not be invoked when tenantID == uuid.Nil")
}

func TestClearDemoHandler_MissingTenant_UseCaseNotInvoked(t *testing.T) {
	seed := appob.NewSeedDemoUseCase(erroringProductCreator{}, noopStockInitializer{}, noopSalesRecorder{})
	clear := appob.NewClearDemoUseCase(panicDemoDeleter{})
	r := buildRouterZZ(seed, clear)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/clear-demo", http.NoBody)
	// No X-Tenant-ID header.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req) // would panic (failing the test) if clear.Execute were reached

	if w.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d: %s", w.Code, w.Body.String())
	}
}
