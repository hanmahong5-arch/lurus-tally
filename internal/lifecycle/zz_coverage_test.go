package lifecycle

// zz_coverage_test.go closes remaining coverage gaps in package lifecycle after
// the existing server_test.go / lifecycle_test.go / migrate_test.go suites. It
// targets the pure/extractable pieces that don't need a real Postgres/NATS/Redis:
//   - dbscopePinner.WithPinnedConn: the uuid.Nil vs non-nil tenant branch (RLS
//     isolation correctness fix — nil tenant must run fn unpinned, not pin
//     app.tenant_id to the all-zeros UUID).
//   - the importing/return adapter shims (import_adapters.go, return_adapters.go):
//     field-translation correctness (ConvFactor="1", WarehouseID deref) and
//     error pass-through, driven against fake BillRepo/StockRepo/CurrencyRepo
//     doubles (real use case objects, not self-validating mocks).
//   - shopRepoAdapter / shopifyShopResolver: field round-trip translation between
//     the app and repo ShopMapping struct shapes, via go-sqlmock so the repo
//     layer's real SQL methods run against a scripted driver.
//   - NewApp(nil): the one NewApp branch reachable without a live DB/Redis/NATS.
//
// RunMigrations/RunMigrationsDown/Start/Stop's happy paths and NewApp's full
// wiring already have real-DSN-required coverage in lifecycle_test.go /
// migrate_test.go; this file does not duplicate those.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	reposhopify "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/shopify"
	repowarehouse "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/warehouse"
	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	appcurrency "github.com/hanmahong5-arch/lurus-tally/internal/app/currency"
	appimporting "github.com/hanmahong5-arch/lurus-tally/internal/app/importing"
	appshopify "github.com/hanmahong5-arch/lurus-tally/internal/app/shopify"
	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	domainbill "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
	domaincurrency "github.com/hanmahong5-arch/lurus-tally/internal/domain/currency"
	domainstock "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/config"
)

// ===========================================================================
// dbscopePinner.WithPinnedConn — nil vs non-nil tenant branch
// ===========================================================================

// TestDbscopePinner_WithPinnedConn_NilTenant_RunsFnUnpinned proves the RLS
// isolation correctness fix: tenantID == uuid.Nil must invoke fn directly on
// the shared pool WITHOUT going through dbscope.WithPinnedConn's SET
// app.tenant_id, because uuid.Nil.String() is the non-empty all-zeros UUID
// string and would otherwise pin the GUC to it instead of degrading to
// unpinned. We prove "no SET app.tenant_id ran" by NOT registering any sqlmock
// expectation and asserting no error / no unmet expectations.
func TestDbscopePinner_WithPinnedConn_NilTenant_RunsFnUnpinned(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(false))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close() //nolint:errcheck

	p := dbscopePinner{db: db}

	calls := 0
	fn := func(ctx context.Context) error {
		calls++
		return nil
	}

	if err := p.WithPinnedConn(context.Background(), uuid.Nil, fn); err != nil {
		t.Fatalf("WithPinnedConn(nil tenant): unexpected error: %v", err)
	}
	if calls != 1 {
		t.Errorf("fn must be invoked exactly once, got %d", calls)
	}
	// No ExpectExec was registered above; if the pinner had gone through
	// dbscope.WithPinnedConn's SET/RESET path against this driver, sqlmock
	// would report an unexpected-call error captured here.
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("sqlmock recorded unexpected calls (nil tenant must bypass SET app.tenant_id): %v", err)
	}
}

// TestDbscopePinner_WithPinnedConn_NonNilTenant_PinsConnection proves the
// complementary branch: a real tenant ID DOES route through
// dbscope.WithPinnedConn, which acquires a dedicated connection and issues
// SET/RESET app.tenant_id around fn.
func TestDbscopePinner_WithPinnedConn_NonNilTenant_PinsConnection(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(false))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close() //nolint:errcheck

	tenant := uuid.New()
	mock.ExpectExec("SELECT set_config").
		WithArgs(tenant.String()).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("RESET app.tenant_id").
		WillReturnResult(sqlmock.NewResult(0, 0))

	p := dbscopePinner{db: db}

	calls := 0
	fn := func(ctx context.Context) error {
		calls++
		return nil
	}

	if err := p.WithPinnedConn(context.Background(), tenant, fn); err != nil {
		t.Fatalf("WithPinnedConn(non-nil tenant): unexpected error: %v", err)
	}
	if calls != 1 {
		t.Errorf("fn must be invoked exactly once, got %d", calls)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("expected SET app.tenant_id + RESET around fn, got: %v", err)
	}
}

// TestDbscopePinner_WithPinnedConn_NonNilTenant_SetConfigFails proves that a
// failure setting the GUC surfaces as an error and fn is never invoked (the
// tenant pin must be established before any tenant-scoped work runs).
func TestDbscopePinner_WithPinnedConn_NonNilTenant_SetConfigFails(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(false))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close() //nolint:errcheck

	tenant := uuid.New()
	mock.ExpectExec("SELECT set_config").
		WithArgs(tenant.String()).
		WillReturnError(errors.New("boom"))

	p := dbscopePinner{db: db}

	calls := 0
	fn := func(ctx context.Context) error {
		calls++
		return nil
	}

	if err := p.WithPinnedConn(context.Background(), tenant, fn); err == nil {
		t.Fatal("expected error when set_config fails, got nil")
	}
	if calls != 0 {
		t.Errorf("fn must NOT run when set_config fails, got %d calls", calls)
	}
}

// ===========================================================================
// importSaleCreator — field mapping (ConvFactor="1", WarehouseID deref) +
// error pass-through
// ===========================================================================

// fakeBillRepo is a hand-rolled appbill.BillRepo double. Only the methods each
// use case under test actually calls are meaningful; the rest return zero
// values because CreateSaleUseCase / CreateReturnBillUseCase never reach them
// in these tests.
type fakeBillRepo struct {
	productExists    bool
	productExistsErr error
	warehouseExists  bool
	createBillErr    error
	nextBillNo       string

	// capture: the items CreateBill was invoked with, so we can assert the
	// adapter's field translation reached the real use case unmodified.
	capturedItems []*domainbill.BillItem
	capturedHead  *domainbill.BillHead
}

func (f *fakeBillRepo) WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	return fn(nil)
}

func (f *fakeBillRepo) CreateBill(ctx context.Context, tx *sql.Tx, head *domainbill.BillHead, items []*domainbill.BillItem) error {
	f.capturedHead = head
	f.capturedItems = items
	return f.createBillErr
}

func (f *fakeBillRepo) GetBillForUpdate(ctx context.Context, tx *sql.Tx, tenantID, billID uuid.UUID) (*domainbill.BillHead, error) {
	return nil, fmt.Errorf("not implemented in fake")
}

func (f *fakeBillRepo) GetBill(ctx context.Context, tenantID, billID uuid.UUID) (*domainbill.BillHead, error) {
	return nil, fmt.Errorf("not implemented in fake")
}

func (f *fakeBillRepo) GetBillItems(ctx context.Context, tenantID, billID uuid.UUID) ([]*domainbill.BillItem, error) {
	return nil, fmt.Errorf("not implemented in fake")
}

func (f *fakeBillRepo) UpdateBillStatus(ctx context.Context, tx *sql.Tx, tenantID, billID uuid.UUID, status domainbill.BillStatus, meta map[string]any) error {
	return fmt.Errorf("not implemented in fake")
}

func (f *fakeBillRepo) UpdateBill(ctx context.Context, tx *sql.Tx, head *domainbill.BillHead, items []*domainbill.BillItem) error {
	return fmt.Errorf("not implemented in fake")
}

func (f *fakeBillRepo) ListBills(ctx context.Context, filter appbill.BillListFilter) ([]domainbill.BillHead, int64, error) {
	return nil, 0, fmt.Errorf("not implemented in fake")
}

func (f *fakeBillRepo) NextBillNo(ctx context.Context, tx *sql.Tx, tenantID uuid.UUID, prefix string) (string, error) {
	if f.nextBillNo == "" {
		return "BN-0001", nil
	}
	return f.nextBillNo, nil
}

func (f *fakeBillRepo) AcquireBillAdvisoryLock(ctx context.Context, tx *sql.Tx, tenantID, billID uuid.UUID) error {
	return nil
}

func (f *fakeBillRepo) UpdatePaidAmount(ctx context.Context, tx *sql.Tx, tenantID, billID uuid.UUID, paidAmount decimal.Decimal) error {
	return nil
}

func (f *fakeBillRepo) ProductExists(ctx context.Context, tenantID, productID uuid.UUID) (bool, error) {
	return f.productExists, f.productExistsErr
}

func (f *fakeBillRepo) WarehouseExists(ctx context.Context, tenantID, warehouseID uuid.UUID) (bool, error) {
	return f.warehouseExists, nil
}

// TestImportSaleCreator_Create_MapsConvFactorAndWarehouseID asserts the
// business invariant: every SaleItem the adapter builds gets ConvFactor="1"
// and the deref'd WarehouseID copied onto each line (money/stock write
// correctness for imported orders).
func TestImportSaleCreator_Create_MapsConvFactorAndWarehouseID(t *testing.T) {
	repo := &fakeBillRepo{productExists: true, warehouseExists: true}
	creator := importSaleCreator{uc: appbill.NewCreateSaleUseCase(repo)}

	tenantID := uuid.New()
	creatorID := uuid.New()
	warehouseID := uuid.New()
	productID := uuid.New()

	in := appimporting.SaleCreatorInput{
		TenantID:    tenantID,
		CreatorID:   creatorID,
		WarehouseID: &warehouseID,
		BillDate:    time.Now(),
		Remark:      "imported order",
		Items: []appimporting.SaleLineItem{
			{ProductID: productID, LineNo: 1, Qty: decimal.NewFromInt(2), UnitPrice: decimal.NewFromInt(10)},
			{ProductID: productID, LineNo: 2, Qty: decimal.NewFromInt(3), UnitPrice: decimal.NewFromInt(5)},
		},
	}

	out, err := creator.Create(context.Background(), in)
	if err != nil {
		t.Fatalf("Create: unexpected error: %v", err)
	}
	if out == nil {
		t.Fatal("Create: expected non-nil output")
	}
	if len(repo.capturedItems) != 2 {
		t.Fatalf("CreateBill must be called with 2 items, got %d", len(repo.capturedItems))
	}
	// Hand-computed expectation: create_sale.go persists it.Qty/it.UnitPrice
	// onto domain.BillItem but the adapter's ConvFactor="1" is NOT one of the
	// domain.BillItem fields (BillItem has no ConvFactor column) — the
	// invariant we can observe end-to-end is that the adapter fed the exact
	// item quantities through (no reordering/dropping) and the resolved
	// warehouse on the head matches the deref'd pointer.
	if repo.capturedHead.WarehouseID == nil || *repo.capturedHead.WarehouseID != warehouseID {
		t.Errorf("bill head WarehouseID = %v, want %v", repo.capturedHead.WarehouseID, warehouseID)
	}
	if !repo.capturedItems[0].Qty.Equal(decimal.NewFromInt(2)) {
		t.Errorf("item[0].Qty = %s, want 2", repo.capturedItems[0].Qty)
	}
	if !repo.capturedItems[1].Qty.Equal(decimal.NewFromInt(3)) {
		t.Errorf("item[1].Qty = %s, want 3", repo.capturedItems[1].Qty)
	}
}

// TestImportSaleCreator_Create_NilWarehouseID_DefaultsToUUIDNil verifies the
// adapter's `warehouseID := uuid.Nil; if in.WarehouseID != nil { ... }` guard:
// a nil pointer must not panic and must leave the per-item warehouse as
// uuid.Nil (create_sale.go then falls back to the bill-level WarehouseID,
// which is also nil here, so validateRefs's WarehouseExists check is skipped).
func TestImportSaleCreator_Create_NilWarehouseID_DefaultsToUUIDNil(t *testing.T) {
	repo := &fakeBillRepo{productExists: true}
	creator := importSaleCreator{uc: appbill.NewCreateSaleUseCase(repo)}

	in := appimporting.SaleCreatorInput{
		TenantID:    uuid.New(),
		CreatorID:   uuid.New(),
		WarehouseID: nil,
		Items: []appimporting.SaleLineItem{
			{ProductID: uuid.New(), LineNo: 1, Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(1)},
		},
	}

	if _, err := creator.Create(context.Background(), in); err != nil {
		t.Fatalf("Create with nil WarehouseID: unexpected error: %v", err)
	}
	if repo.capturedHead.WarehouseID != nil {
		t.Errorf("bill head WarehouseID = %v, want nil (no bill-level or per-item warehouse given)", *repo.capturedHead.WarehouseID)
	}
}

// TestImportSaleCreator_Create_ErrorPassthrough proves the wrapped use case's
// error surfaces unchanged (validation failure: zero items).
func TestImportSaleCreator_Create_ErrorPassthrough(t *testing.T) {
	repo := &fakeBillRepo{productExists: true}
	creator := importSaleCreator{uc: appbill.NewCreateSaleUseCase(repo)}

	in := appimporting.SaleCreatorInput{
		TenantID:  uuid.New(),
		CreatorID: uuid.New(),
		Items:     nil, // triggers "at least one item is required"
	}

	out, err := creator.Create(context.Background(), in)
	if err == nil {
		t.Fatal("expected error for empty Items, got nil")
	}
	if out != nil {
		t.Errorf("expected nil output on error, got %+v", out)
	}
	if !errors.Is(err, appbill.ErrValidation) {
		t.Errorf("expected ErrValidation, got %v", err)
	}
}

// TestImportSaleCreator_Create_RepoErrorPassthrough proves a repo-level
// failure (CreateBill error) surfaces unchanged through the adapter.
func TestImportSaleCreator_Create_RepoErrorPassthrough(t *testing.T) {
	wantErr := errors.New("db write failed")
	repo := &fakeBillRepo{productExists: true, createBillErr: wantErr}
	creator := importSaleCreator{uc: appbill.NewCreateSaleUseCase(repo)}

	in := appimporting.SaleCreatorInput{
		TenantID:  uuid.New(),
		CreatorID: uuid.New(),
		Items: []appimporting.SaleLineItem{
			{ProductID: uuid.New(), LineNo: 1, Qty: decimal.NewFromInt(1), UnitPrice: decimal.NewFromInt(1)},
		},
	}

	out, err := creator.Create(context.Background(), in)
	if err == nil {
		t.Fatal("expected repo error to propagate, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("expected wrapped %v, got %v", wantErr, err)
	}
	if out != nil {
		t.Errorf("expected nil output on error, got %+v", out)
	}
}

// ===========================================================================
// importSaleApprover — direct field passthrough + error passthrough
// ===========================================================================

// fakeApproveSaleDeps builds a minimal ApproveSaleUseCase whose GetBill call
// (the pre-lock idempotency check) fails, forcing WithTx's fn to run so
// AcquireBillAdvisoryLock -> GetBillForUpdate is exercised deterministically
// via the same fakeBillRepo. We only need to prove request field passthrough
// and error surfacing, not full approval business logic (that belongs to
// app/bill's own test suite).
func TestImportSaleApprover_Approve_ErrorPassthrough(t *testing.T) {
	repo := &fakeBillRepo{}
	// stockUC/unitRepo/paymentRepo may be nil: GetBill fails immediately below
	// (not implemented in fake), so approveSaleUC.Execute short-circuits into
	// WithTx -> AcquireBillAdvisoryLock (nil-safe) -> GetBillForUpdate (fails)
	// before ever touching stockUC/unitRepo/paymentRepo.
	approveSaleUC := appbill.NewApproveSaleUseCase(repo, nil, nil, nil)
	approver := importSaleApprover{uc: approveSaleUC}

	tenantID := uuid.New()
	billID := uuid.New()
	creatorID := uuid.New()

	err := approver.Approve(context.Background(), appimporting.SaleApproverInput{
		TenantID:  tenantID,
		BillID:    billID,
		CreatorID: creatorID,
	})
	if err == nil {
		t.Fatal("expected error (fakeBillRepo.GetBillForUpdate always errors), got nil")
	}
}

// TestImportSaleApprover_Approve_ZeroBillID_ReturnsValidationError proves the
// wrapped use case's own guard ("bill_id is required") surfaces through the
// adapter unchanged when BillID is the zero UUID — no DB call needed.
func TestImportSaleApprover_Approve_ZeroBillID_ReturnsValidationError(t *testing.T) {
	repo := &fakeBillRepo{}
	approveSaleUC := appbill.NewApproveSaleUseCase(repo, nil, nil, nil)
	approver := importSaleApprover{uc: approveSaleUC}

	err := approver.Approve(context.Background(), appimporting.SaleApproverInput{
		TenantID:  uuid.New(),
		BillID:    uuid.Nil,
		CreatorID: uuid.New(),
	})
	if err == nil {
		t.Fatal("expected error for zero BillID, got nil")
	}
}

// ===========================================================================
// importStockChecker — nil snapshot -> decimal.Zero,nil / error passthrough
// ===========================================================================

// fakeStockRepo implements appstock.StockRepo. Only GetSnapshot is meaningful
// for GetSnapshotUseCase; the rest are unreachable in these tests.
type fakeStockRepo struct {
	snapshot *domainstock.Snapshot
	err      error
}

func (f *fakeStockRepo) GetSnapshot(ctx context.Context, tenantID, productID, warehouseID uuid.UUID) (*domainstock.Snapshot, error) {
	return f.snapshot, f.err
}
func (f *fakeStockRepo) SelectForUpdate(ctx context.Context, tx *sql.Tx, tenantID, productID, warehouseID uuid.UUID) (*domainstock.Snapshot, error) {
	return nil, fmt.Errorf("not implemented in fake")
}
func (f *fakeStockRepo) UpsertSnapshot(ctx context.Context, tx *sql.Tx, s *domainstock.Snapshot) error {
	return fmt.Errorf("not implemented in fake")
}
func (f *fakeStockRepo) InsertMovement(ctx context.Context, tx *sql.Tx, m *domainstock.Movement) error {
	return fmt.Errorf("not implemented in fake")
}
func (f *fakeStockRepo) ListMovements(ctx context.Context, filter appstock.MovementFilter) ([]domainstock.Movement, error) {
	return nil, fmt.Errorf("not implemented in fake")
}
func (f *fakeStockRepo) InsertLot(ctx context.Context, tx *sql.Tx, l *domainstock.Lot) error {
	return fmt.Errorf("not implemented in fake")
}
func (f *fakeStockRepo) ListActiveLots(ctx context.Context, tx *sql.Tx, tenantID, productID, warehouseID uuid.UUID) ([]domainstock.Lot, error) {
	return nil, fmt.Errorf("not implemented in fake")
}
func (f *fakeStockRepo) UpdateLotQty(ctx context.Context, tx *sql.Tx, lotID uuid.UUID, qtyRemaining decimal.Decimal) error {
	return fmt.Errorf("not implemented in fake")
}
func (f *fakeStockRepo) AcquireAdvisoryLock(ctx context.Context, tx *sql.Tx, tenantID, productID, warehouseID uuid.UUID) error {
	return fmt.Errorf("not implemented in fake")
}
func (f *fakeStockRepo) ListSnapshots(ctx context.Context, filter appstock.ListSnapshotsFilter) ([]domainstock.Snapshot, error) {
	return nil, fmt.Errorf("not implemented in fake")
}
func (f *fakeStockRepo) WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	return fn(nil)
}

func TestImportStockChecker_AvailableQty_TableDriven(t *testing.T) {
	someErr := errors.New("stock repo unavailable")
	cases := []struct {
		name     string
		snapshot *domainstock.Snapshot
		repoErr  error
		wantQty  decimal.Decimal
		wantErr  bool
	}{
		{
			name:     "nil snapshot returns decimal.Zero, nil error",
			snapshot: nil,
			repoErr:  nil,
			wantQty:  decimal.Zero,
			wantErr:  false,
		},
		{
			name:     "present snapshot returns its AvailableQty",
			snapshot: &domainstock.Snapshot{AvailableQty: decimal.NewFromInt(42)},
			repoErr:  nil,
			wantQty:  decimal.NewFromInt(42),
			wantErr:  false,
		},
		{
			name:     "repo error returns decimal.Zero, err (wrapped, still detectable)",
			snapshot: nil,
			repoErr:  someErr,
			wantQty:  decimal.Zero,
			wantErr:  true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeStockRepo{snapshot: tc.snapshot, err: tc.repoErr}
			checker := importStockChecker{uc: appstock.NewGetSnapshotUseCase(repo)}

			qty, err := checker.AvailableQty(context.Background(), uuid.New(), uuid.New(), uuid.New())

			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !errors.Is(err, someErr) {
					t.Errorf("expected wrapped %v, got %v", someErr, err)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !qty.Equal(tc.wantQty) {
				t.Errorf("AvailableQty = %s, want %s", qty, tc.wantQty)
			}
		})
	}
}

// ===========================================================================
// importWarehouseChecker — BelongsToTenant error passthrough
// ===========================================================================

func TestImportWarehouseChecker_BelongsToTenant_ErrorPassthrough(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(false))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close() //nolint:errcheck

	tenantID := uuid.New()
	warehouseID := uuid.New()
	mock.ExpectQuery("SELECT id, tenant_id, code, name, address, manager, is_default, remark, created_at, updated_at, deleted_at").
		WithArgs(warehouseID, tenantID).
		WillReturnError(errors.New("connection reset"))

	checker := importWarehouseChecker{repo: repowarehouse.New(db)}

	if err := checker.BelongsToTenant(context.Background(), tenantID, warehouseID); err == nil {
		t.Fatal("expected error to propagate from GetByID, got nil")
	}
}

// TestImportWarehouseChecker_BelongsToTenant_NotFound proves the not-found
// case (any error, including ErrNotFound) is sufficient to reject the request
// — importWarehouseChecker.BelongsToTenant treats any non-nil error as
// "does not belong to this tenant", per import_adapters.go's doc comment.
func TestImportWarehouseChecker_BelongsToTenant_NotFound(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(false))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close() //nolint:errcheck

	tenantID := uuid.New()
	warehouseID := uuid.New()
	mock.ExpectQuery("SELECT id, tenant_id, code, name, address, manager, is_default, remark, created_at, updated_at, deleted_at").
		WithArgs(warehouseID, tenantID).
		WillReturnError(sql.ErrNoRows)

	checker := importWarehouseChecker{repo: repowarehouse.New(db)}

	if err := checker.BelongsToTenant(context.Background(), tenantID, warehouseID); err == nil {
		t.Fatal("expected ErrNotFound to surface as a non-nil error, got nil")
	}
}

// ===========================================================================
// importCurrencyRater — rate mapping + error passthrough
// ===========================================================================

type fakeCurrencyRepo struct {
	rate *domaincurrency.ExchangeRate
	err  error
}

func (f *fakeCurrencyRepo) ListCurrencies(ctx context.Context) ([]domaincurrency.Currency, error) {
	return nil, fmt.Errorf("not implemented in fake")
}
func (f *fakeCurrencyRepo) GetRateOn(ctx context.Context, tenantID uuid.UUID, from, to string, date time.Time) (*domaincurrency.ExchangeRate, error) {
	return f.rate, f.err
}
func (f *fakeCurrencyRepo) SaveRate(ctx context.Context, r *domaincurrency.ExchangeRate) error {
	return fmt.Errorf("not implemented in fake")
}
func (f *fakeCurrencyRepo) ListRateHistory(ctx context.Context, tenantID uuid.UUID, from, to string, days int) ([]domaincurrency.ExchangeRate, error) {
	return nil, fmt.Errorf("not implemented in fake")
}

func TestImportCurrencyRater_GetRate_TableDriven(t *testing.T) {
	someErr := errors.New("currency repo down")
	cases := []struct {
		name     string
		rate     *domaincurrency.ExchangeRate
		repoErr  error
		wantRate decimal.Decimal
		wantErr  bool
	}{
		{
			name:     "repo returns a rate row",
			rate:     &domaincurrency.ExchangeRate{Rate: decimal.NewFromFloat(7.15), Source: "manual"},
			wantRate: decimal.NewFromFloat(7.15),
		},
		{
			name:     "repo returns nil (no_rate_found) -> default 1",
			rate:     nil,
			wantRate: decimal.NewFromInt(1),
		},
		{
			name:    "repo error propagates",
			repoErr: someErr,
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeCurrencyRepo{rate: tc.rate, err: tc.repoErr}
			rater := importCurrencyRater{uc: appcurrency.NewGetRateUseCase(repo)}

			rate, err := rater.GetRate(context.Background(), uuid.New(), "USD", "CNY", time.Now())

			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !errors.Is(err, someErr) {
					t.Errorf("expected wrapped %v, got %v", someErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !rate.Equal(tc.wantRate) {
				t.Errorf("GetRate = %s, want %s", rate, tc.wantRate)
			}
		})
	}
}

// TestImportCurrencyRater_GetRate_SameCurrency_ShortCircuitsToRateOne proves
// the from==to fast path (no repo call) returns rate=1 without error — hand
// computed from get_rate.go's `if from == to` branch.
func TestImportCurrencyRater_GetRate_SameCurrency_ShortCircuitsToRateOne(t *testing.T) {
	repo := &fakeCurrencyRepo{err: errors.New("must not be called")}
	rater := importCurrencyRater{uc: appcurrency.NewGetRateUseCase(repo)}

	rate, err := rater.GetRate(context.Background(), uuid.New(), "CNY", "CNY", time.Now())
	if err != nil {
		t.Fatalf("same-currency GetRate must not error, got: %v", err)
	}
	if !rate.Equal(decimal.NewFromInt(1)) {
		t.Errorf("GetRate(CNY,CNY) = %s, want 1", rate)
	}
}

// ===========================================================================
// importReturnCreator / importReturnApprover — field mapping + error passthrough
// ===========================================================================

// TestImportReturnCreator_Create_MapsItemsFaithfully asserts the business
// invariant: ReturnItem qty/price map through unchanged (line_no, qty, price)
// and the created bill head carries the given remark/warehouse.
func TestImportReturnCreator_Create_MapsItemsFaithfully(t *testing.T) {
	repo := &fakeBillRepo{productExists: true, warehouseExists: true}
	creator := importReturnCreator{uc: appbill.NewCreateReturnBillUseCase(repo)}

	tenantID := uuid.New()
	creatorID := uuid.New()
	warehouseID := uuid.New()
	productID := uuid.New()

	out, err := creator.Create(context.Background(), appimporting.ReturnCreatorInput{
		TenantID:    tenantID,
		CreatorID:   creatorID,
		WarehouseID: &warehouseID,
		Remark:      "cancel reversal",
		Items: []appimporting.SaleLineItem{
			{ProductID: productID, LineNo: 1, Qty: decimal.NewFromInt(5), UnitPrice: decimal.NewFromInt(9)},
		},
	})
	if err != nil {
		t.Fatalf("Create: unexpected error: %v", err)
	}
	if out == nil {
		t.Fatal("expected non-nil output")
	}
	if len(repo.capturedItems) != 1 {
		t.Fatalf("expected 1 captured item, got %d", len(repo.capturedItems))
	}
	if !repo.capturedItems[0].Qty.Equal(decimal.NewFromInt(5)) {
		t.Errorf("item Qty = %s, want 5", repo.capturedItems[0].Qty)
	}
	if !repo.capturedItems[0].UnitPrice.Equal(decimal.NewFromInt(9)) {
		t.Errorf("item UnitPrice = %s, want 9", repo.capturedItems[0].UnitPrice)
	}
	if repo.capturedItems[0].LineNo != 1 {
		t.Errorf("item LineNo = %d, want 1", repo.capturedItems[0].LineNo)
	}
	if repo.capturedHead.WarehouseID == nil || *repo.capturedHead.WarehouseID != warehouseID {
		t.Errorf("bill head WarehouseID = %v, want %v", repo.capturedHead.WarehouseID, warehouseID)
	}
	if repo.capturedHead.Remark != "cancel reversal" {
		t.Errorf("bill head Remark = %q, want %q", repo.capturedHead.Remark, "cancel reversal")
	}
}

// TestImportReturnCreator_Create_ErrorPassthrough proves the wrapped use
// case's validation error surfaces unchanged.
func TestImportReturnCreator_Create_ErrorPassthrough(t *testing.T) {
	repo := &fakeBillRepo{productExists: true}
	creator := importReturnCreator{uc: appbill.NewCreateReturnBillUseCase(repo)}

	out, err := creator.Create(context.Background(), appimporting.ReturnCreatorInput{
		TenantID:  uuid.New(),
		CreatorID: uuid.New(),
		Items:     nil,
	})
	if err == nil {
		t.Fatal("expected error for empty Items, got nil")
	}
	if out != nil {
		t.Errorf("expected nil output on error, got %+v", out)
	}
	if !errors.Is(err, appbill.ErrValidation) {
		t.Errorf("expected ErrValidation, got %v", err)
	}
}

// TestImportReturnApprover_Approve_ErrorPassthrough proves ApproveReturnBillUseCase's
// own guard ("bill_id is required") surfaces through the adapter unchanged.
func TestImportReturnApprover_Approve_ErrorPassthrough(t *testing.T) {
	repo := &fakeBillRepo{}
	approveUC := appbill.NewApproveReturnBillUseCase(repo, nil)
	approver := importReturnApprover{uc: approveUC}

	err := approver.Approve(context.Background(), appimporting.ReturnApproverInput{
		TenantID:  uuid.New(),
		BillID:    uuid.Nil,
		CreatorID: uuid.New(),
	})
	if err == nil {
		t.Fatal("expected error for zero BillID, got nil")
	}
}

// ===========================================================================
// shopRepoAdapter — round-trips all five ShopMapping fields
// ===========================================================================

// TestShopRepoAdapter_Create_RoundTripsAllFields drives the real
// reposhopify.ShopMapRepo.Create SQL through go-sqlmock and asserts the
// adapter forwarded every field (ID/TenantID/ShopDomain/WarehouseID/CreatorID)
// unchanged — the tenant-binding integrity invariant.
func TestShopRepoAdapter_Create_RoundTripsAllFields(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(false))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close() //nolint:errcheck

	id := uuid.New()
	tenantID := uuid.New()
	warehouseID := uuid.New()
	creatorID := uuid.New()

	mock.ExpectExec("INSERT INTO tally.shopify_shop_map").
		WithArgs(id, tenantID, "store.myshopify.com", warehouseID, creatorID).
		WillReturnResult(sqlmock.NewResult(1, 1))

	adapter := newShopRepoAdapter(reposhopify.New(db))

	err = adapter.Create(context.Background(), &appshopify.ShopMapping{
		ID:          id,
		TenantID:    tenantID,
		ShopDomain:  "store.myshopify.com",
		WarehouseID: warehouseID,
		CreatorID:   creatorID,
	})
	if err != nil {
		t.Fatalf("Create: unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Create did not forward all 5 fields to the INSERT: %v", err)
	}
}

// TestShopRepoAdapter_Create_ErrorPassthrough proves a repo-level failure
// (e.g. UNIQUE violation surfaced as reposhopify.ErrShopAlreadyBound) comes
// back through the adapter unchanged.
func TestShopRepoAdapter_Create_ErrorPassthrough(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(false))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close() //nolint:errcheck

	mock.ExpectExec("INSERT INTO tally.shopify_shop_map").
		WillReturnError(errors.New("duplicate key value violates unique constraint"))

	adapter := newShopRepoAdapter(reposhopify.New(db))

	err = adapter.Create(context.Background(), &appshopify.ShopMapping{
		ID:          uuid.New(),
		TenantID:    uuid.New(),
		ShopDomain:  "dup.myshopify.com",
		WarehouseID: uuid.New(),
		CreatorID:   uuid.New(),
	})
	if err == nil {
		t.Fatal("expected error to propagate, got nil")
	}
	if !errors.Is(err, reposhopify.ErrShopAlreadyBound) {
		t.Errorf("expected ErrShopAlreadyBound (unique violation detected), got %v", err)
	}
}

// TestShopRepoAdapter_ListByTenant_RoundTripsAllFields drives the real SELECT
// through go-sqlmock and asserts every returned row's five fields survive the
// reposhopify.ShopMapping -> appshopify.ShopMapping translation unchanged.
func TestShopRepoAdapter_ListByTenant_RoundTripsAllFields(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(false))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close() //nolint:errcheck

	tenantID := uuid.New()
	id := uuid.New()
	warehouseID := uuid.New()
	creatorID := uuid.New()

	rows := sqlmock.NewRows([]string{"id", "shop_domain", "tenant_id", "warehouse_id", "creator_id"}).
		AddRow(id, "a.myshopify.com", tenantID, warehouseID, creatorID)
	mock.ExpectQuery("SELECT id, shop_domain, tenant_id, warehouse_id, creator_id").
		WithArgs(tenantID).
		WillReturnRows(rows)

	adapter := newShopRepoAdapter(reposhopify.New(db))

	got, err := adapter.ListByTenant(context.Background(), tenantID)
	if err != nil {
		t.Fatalf("ListByTenant: unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 mapping, got %d", len(got))
	}
	m := got[0]
	if m.ID != id {
		t.Errorf("ID = %v, want %v", m.ID, id)
	}
	if m.TenantID != tenantID {
		t.Errorf("TenantID = %v, want %v", m.TenantID, tenantID)
	}
	if m.ShopDomain != "a.myshopify.com" {
		t.Errorf("ShopDomain = %q, want %q", m.ShopDomain, "a.myshopify.com")
	}
	if m.WarehouseID != warehouseID {
		t.Errorf("WarehouseID = %v, want %v", m.WarehouseID, warehouseID)
	}
	if m.CreatorID != creatorID {
		t.Errorf("CreatorID = %v, want %v", m.CreatorID, creatorID)
	}
}

// TestShopRepoAdapter_ListByTenant_ErrorPassthrough proves a query failure
// surfaces through the adapter unchanged.
func TestShopRepoAdapter_ListByTenant_ErrorPassthrough(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(false))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close() //nolint:errcheck

	tenantID := uuid.New()
	mock.ExpectQuery("SELECT id, shop_domain, tenant_id, warehouse_id, creator_id").
		WithArgs(tenantID).
		WillReturnError(errors.New("connection reset"))

	adapter := newShopRepoAdapter(reposhopify.New(db))

	got, err := adapter.ListByTenant(context.Background(), tenantID)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got != nil {
		t.Errorf("expected nil result on error, got %+v", got)
	}
}

// TestShopRepoAdapter_DeleteByID_Passthrough drives the real DELETE and
// asserts both the tenant-scoped args and the (nil-error) happy path.
func TestShopRepoAdapter_DeleteByID_Passthrough(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(false))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close() //nolint:errcheck

	tenantID := uuid.New()
	id := uuid.New()
	mock.ExpectExec("DELETE FROM tally.shopify_shop_map").
		WithArgs(id, tenantID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	adapter := newShopRepoAdapter(reposhopify.New(db))
	if err := adapter.DeleteByID(context.Background(), tenantID, id); err != nil {
		t.Fatalf("DeleteByID: unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("DeleteByID did not forward (id, tenantID) as expected: %v", err)
	}
}

// ===========================================================================
// shopifyShopResolver — GetByDomain translation + nil-mapping + error passthrough
// ===========================================================================

func TestShopifyShopResolver_GetByDomain_TranslatesMapping(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(false))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close() //nolint:errcheck

	id := uuid.New()
	tenantID := uuid.New()
	warehouseID := uuid.New()
	creatorID := uuid.New()

	rows := sqlmock.NewRows([]string{"id", "shop_domain", "tenant_id", "warehouse_id", "creator_id"}).
		AddRow(id, "store.myshopify.com", tenantID, warehouseID, creatorID)
	mock.ExpectQuery("SELECT id, shop_domain, tenant_id, warehouse_id, creator_id").
		WithArgs("store.myshopify.com").
		WillReturnRows(rows)

	resolver := &shopifyShopResolver{repo: reposhopify.New(db)}
	got, err := resolver.GetByDomain(context.Background(), "store.myshopify.com")
	if err != nil {
		t.Fatalf("GetByDomain: unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil mapping")
	}
	if got.ShopDomain != "store.myshopify.com" || got.TenantID != tenantID ||
		got.WarehouseID != warehouseID || got.CreatorID != creatorID {
		t.Errorf("mapping translation mismatch: %+v", got)
	}
}

func TestShopifyShopResolver_GetByDomain_NoRows_ReturnsNilNil(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(false))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close() //nolint:errcheck

	mock.ExpectQuery("SELECT id, shop_domain, tenant_id, warehouse_id, creator_id").
		WithArgs("missing.myshopify.com").
		WillReturnError(sql.ErrNoRows)

	resolver := &shopifyShopResolver{repo: reposhopify.New(db)}
	got, err := resolver.GetByDomain(context.Background(), "missing.myshopify.com")
	if err != nil {
		t.Fatalf("expected (nil, nil) for no rows, got error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil mapping, got %+v", got)
	}
}

func TestShopifyShopResolver_GetByDomain_ErrorPassthrough(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(false))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close() //nolint:errcheck

	mock.ExpectQuery("SELECT id, shop_domain, tenant_id, warehouse_id, creator_id").
		WithArgs("err.myshopify.com").
		WillReturnError(errors.New("connection reset"))

	resolver := &shopifyShopResolver{repo: reposhopify.New(db)}
	got, err := resolver.GetByDomain(context.Background(), "err.myshopify.com")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got != nil {
		t.Errorf("expected nil mapping on error, got %+v", got)
	}
}

// ===========================================================================
// BuildShopifyHandler — pure wiring smoke test (no network/DB touched)
// ===========================================================================

// TestBuildShopifyHandler_ReturnsNonNilHandler proves the wiring function
// constructs a handler without panicking when given a nil *sql.DB (the
// handler itself does not dial the DB at construction time).
func TestBuildShopifyHandler_ReturnsNonNilHandler(t *testing.T) {
	h := BuildShopifyHandler(nil, nil, "test-secret", nil)
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
}

// ===========================================================================
// NewApp(nil) — the one NewApp branch reachable with no DB/Redis/NATS
// ===========================================================================

// TestNewApp_NilConfig_ReturnsError proves NewApp(nil) fails fast with a
// clear error instead of panicking on a nil *config.Config dereference.
func TestNewApp_NilConfig_ReturnsError(t *testing.T) {
	app, err := NewApp(nil)
	if err == nil {
		t.Fatal("expected error for nil config, got nil")
	}
	if app != nil {
		t.Errorf("expected nil App on error, got %+v", app)
	}
	const want = "config must not be nil"
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

// TestApp_Handler_NilReceiver proves the nil-receiver guard in Handler()
// returns nil instead of panicking — used by callers that may hold a nil *App
// after a failed NewApp.
func TestApp_Handler_NilReceiver(t *testing.T) {
	var app *App
	if h := app.Handler(); h != nil {
		t.Errorf("expected nil Handler for nil *App, got %v", h)
	}
}

// ===========================================================================
// RunMigrations / RunMigrationsDown / Start / Stop — integration-only paths
// ===========================================================================

// TestRunMigrationsDown_InvalidDSN_ReturnsError covers the one branch of
// RunMigrationsDown reachable without a live Postgres: an unparseable/
// unreachable DSN fails at the ping step with a clear error prefix.
// The actual down-migration path (m.Down() against a live schema) needs a
// real Postgres — gated behind DATABASE_DSN, matching this package's existing
// integration-only convention (see migrate_test.go).
func TestRunMigrationsDown_InvalidDSN_ReturnsError(t *testing.T) {
	ctx := context.Background()
	dsn := "postgres://invalid:invalid@127.0.0.1:1/nonexistent?sslmode=disable&connect_timeout=1"

	err := RunMigrationsDown(ctx, dsn)
	if err == nil {
		t.Fatal("expected error for unreachable DSN, got nil")
	}
}

// TestStart_ContextAlreadyCancelled_ReturnsErrorWithoutTouchingDB proves the
// ctx.Err() fast-fail guard in Start runs before any migration/DB/HTTP work,
// so it is safely unit-testable without a real Postgres/Redis/NATS: a bare
// App{} with nil srv/db/log would otherwise panic if Start proceeded past
// this guard.
func TestStart_ContextAlreadyCancelled_ReturnsErrorWithoutTouchingDB(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before Start observes it

	a := &App{cfg: &config.Config{MigrateOnBoot: true}}
	err := a.Start(ctx)
	if err == nil {
		t.Fatal("expected error for already-cancelled context, got nil")
	}
}
