package stock_test

// This file adds coverage for error-propagation branches, query use cases,
// BatchInsufficientStockError formatting, RecordMovementUseCase guard clauses,
// ExecuteInTx, and a race-safety check on advisory-lock ordering. It reuses
// the mockRepo/newMovement/d/stubProfile helpers already defined in
// mock_repo_test.go, calc_wac_test.go and calculator_factory_test.go
// (same package, same test binary) and does not modify any existing file.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
)

// ---------------------------------------------------------------------------
// errRepo: wraps mockRepo, letting each StockRepo method be forced to fail so
// every wrapped-error branch in calc_wac.go / calc_fifo.go / query.go can be
// exercised without touching the existing mockRepo fake.
// ---------------------------------------------------------------------------

type errRepo struct {
	*mockRepo

	insertMovementErr error
	upsertSnapshotErr error
	insertLotErr      error
	listActiveLotsErr error
	updateLotQtyErr   error
	getSnapshotErr    error
	listMovementsErr  error
	listSnapshotsErr  error

	lastMovementFilter appstock.MovementFilter
	lastSnapshotFilter appstock.ListSnapshotsFilter
}

func newErrRepo(snap *domain.Snapshot) *errRepo {
	return &errRepo{mockRepo: newMockRepo(snap)}
}

func (e *errRepo) InsertMovement(ctx context.Context, tx *sql.Tx, mv *domain.Movement) error {
	if e.insertMovementErr != nil {
		return e.insertMovementErr
	}
	return e.mockRepo.InsertMovement(ctx, tx, mv)
}

func (e *errRepo) UpsertSnapshot(ctx context.Context, tx *sql.Tx, s *domain.Snapshot) error {
	if e.upsertSnapshotErr != nil {
		return e.upsertSnapshotErr
	}
	return e.mockRepo.UpsertSnapshot(ctx, tx, s)
}

func (e *errRepo) InsertLot(ctx context.Context, tx *sql.Tx, l *domain.Lot) error {
	if e.insertLotErr != nil {
		return e.insertLotErr
	}
	return e.mockRepo.InsertLot(ctx, tx, l)
}

func (e *errRepo) ListActiveLots(ctx context.Context, tx *sql.Tx, t, p, w uuid.UUID) ([]domain.Lot, error) {
	if e.listActiveLotsErr != nil {
		return nil, e.listActiveLotsErr
	}
	return e.mockRepo.ListActiveLots(ctx, tx, t, p, w)
}

func (e *errRepo) UpdateLotQty(ctx context.Context, tx *sql.Tx, lotID uuid.UUID, qty decimal.Decimal) error {
	if e.updateLotQtyErr != nil {
		return e.updateLotQtyErr
	}
	return e.mockRepo.UpdateLotQty(ctx, tx, lotID, qty)
}

func (e *errRepo) GetSnapshot(ctx context.Context, t, p, w uuid.UUID) (*domain.Snapshot, error) {
	if e.getSnapshotErr != nil {
		return nil, e.getSnapshotErr
	}
	return e.mockRepo.GetSnapshot(ctx, t, p, w)
}

func (e *errRepo) ListMovements(ctx context.Context, f appstock.MovementFilter) ([]domain.Movement, error) {
	e.lastMovementFilter = f
	if e.listMovementsErr != nil {
		return nil, e.listMovementsErr
	}
	return e.mockRepo.ListMovements(ctx, f)
}

func (e *errRepo) ListSnapshots(ctx context.Context, f appstock.ListSnapshotsFilter) ([]domain.Snapshot, error) {
	e.lastSnapshotFilter = f
	if e.listSnapshotsErr != nil {
		return nil, e.listSnapshotsErr
	}
	return e.mockRepo.ListSnapshots(ctx, f)
}

var _ appstock.StockRepo = (*errRepo)(nil)
var _ appstock.SnapshotLister = (*errRepo)(nil)

// ---------------------------------------------------------------------------
// fakeOutbox: minimal OutboxEnqueuer implementation.
// ---------------------------------------------------------------------------

type fakeOutbox struct {
	mu      sync.Mutex
	called  bool
	subject string
	err     error
}

func (f *fakeOutbox) Enqueue(_ context.Context, _ *sql.Tx, _ uuid.UUID, subject string, _ json.RawMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.called = true
	f.subject = subject
	return f.err
}

var _ appstock.OutboxEnqueuer = (*fakeOutbox)(nil)

// ===========================================================================
// InsufficientStockError / BatchInsufficientStockError
// ===========================================================================

func TestInsufficientStockError_ErrorString(t *testing.T) {
	pid := uuid.New()
	err := &appstock.InsufficientStockError{ProductID: pid, Available: d("5"), Requested: d("12")}
	got := err.Error()
	want := "stock: insufficient stock: product=" + pid.String() + ", available=5, requested=12"
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestBatchInsufficientStockError_ErrorString(t *testing.T) {
	pid1, pid2 := uuid.New(), uuid.New()

	t.Run("zero shortages", func(t *testing.T) {
		e := &appstock.BatchInsufficientStockError{}
		got := e.Error()
		want := "stock: insufficient stock for "
		if got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("one shortage", func(t *testing.T) {
		e := &appstock.BatchInsufficientStockError{
			Shortages: []appstock.InsufficientStockError{
				{ProductID: pid1, Available: d("1"), Requested: d("3")},
			},
		}
		got := e.Error()
		want := "stock: insufficient stock for product=" + pid1.String() + " available=1 requested=3"
		if got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("two shortages", func(t *testing.T) {
		e := &appstock.BatchInsufficientStockError{
			Shortages: []appstock.InsufficientStockError{
				{ProductID: pid1, Available: d("1"), Requested: d("3")},
				{ProductID: pid2, Available: d("0"), Requested: d("2")},
			},
		}
		got := e.Error()
		want := "stock: insufficient stock for product=" + pid1.String() + " available=1 requested=3; " +
			"product=" + pid2.String() + " available=0 requested=2"
		if got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})
}

func TestBatchInsufficientStockError_BatchDetails(t *testing.T) {
	pid := uuid.New()
	e := &appstock.BatchInsufficientStockError{
		Shortages: []appstock.InsufficientStockError{
			{ProductID: pid, Available: d("4"), Requested: d("9")},
		},
	}
	details := e.BatchDetails()
	if len(details) != 1 {
		t.Fatalf("len(details) = %d, want 1", len(details))
	}
	if details[0].ProductIDStr != pid.String() {
		t.Errorf("ProductIDStr = %q, want %q", details[0].ProductIDStr, pid.String())
	}
	if details[0].AvailableStr != "4" {
		t.Errorf("AvailableStr = %q, want %q", details[0].AvailableStr, "4")
	}
	if details[0].RequestedStr != "9" {
		t.Errorf("RequestedStr = %q, want %q", details[0].RequestedStr, "9")
	}
}

func TestIsBatchInsufficientStock(t *testing.T) {
	if appstock.IsBatchInsufficientStock(errors.New("plain")) {
		t.Error("plain error must not be classified as batch insufficient stock")
	}
	if !appstock.IsBatchInsufficientStock(&appstock.BatchInsufficientStockError{}) {
		t.Error("*BatchInsufficientStockError must be classified as batch insufficient stock")
	}
}

// ===========================================================================
// WAC — ValidateMovement edge cases
// ===========================================================================

func TestWAC_ValidateMovement_AdjustCases(t *testing.T) {
	snap := &domain.Snapshot{ID: uuid.New(), TenantID: testTenantID, ProductID: testProductID, WarehouseID: testWarehouseID,
		OnHandQty: d("10"), UnitCost: d("5")}

	cases := []struct {
		name    string
		qty     string
		wantErr bool
	}{
		{"adjust positive", "3", false},
		{"adjust negative within available", "-10", false},
		{"adjust negative beyond available", "-11", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calc := appstock.NewCalculator(stubProfile{"wac"}, newMockRepo(snap))
			m := newMovement(testTenantID, testProductID, testWarehouseID, domain.DirectionAdjust, d(tc.qty), d("0"), domain.RefAdjust)
			err := calc.ValidateMovement(context.Background(), nil, m)
			if tc.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
			if tc.wantErr && !appstock.IsInsufficientStock(err) {
				t.Errorf("expected InsufficientStockError, got %T", err)
			}
		})
	}
}

func TestWAC_ValidateMovement_Out_NilSnapshot_InsufficientStock(t *testing.T) {
	calc := appstock.NewCalculator(stubProfile{"wac"}, newMockRepo(nil))
	m := newMovement(testTenantID, testProductID, testWarehouseID, domain.DirectionOut, d("1"), d("0"), domain.RefSale)
	err := calc.ValidateMovement(context.Background(), nil, m)
	if err == nil || !appstock.IsInsufficientStock(err) {
		t.Fatalf("expected InsufficientStockError for out against nil snapshot, got %v", err)
	}
	ise := err.(*appstock.InsufficientStockError)
	if !ise.Available.IsZero() {
		t.Errorf("Available = %s, want 0", ise.Available)
	}
}

func TestWAC_ValidateMovement_SelectForUpdateError_Propagated(t *testing.T) {
	repo := newMockRepo(nil)
	repo.selectForUpdateErr = errors.New("db down")
	calc := appstock.NewCalculator(stubProfile{"wac"}, repo)
	m := newMovement(testTenantID, testProductID, testWarehouseID, domain.DirectionOut, d("1"), d("0"), domain.RefSale)
	err := calc.ValidateMovement(context.Background(), nil, m)
	if err == nil || !strings.Contains(err.Error(), "wac validate:") || !strings.Contains(err.Error(), "db down") {
		t.Fatalf("expected wrapped 'wac validate:' error, got %v", err)
	}
}

// ===========================================================================
// WAC — ApplyMovement hand-computed formulas and error propagation
// ===========================================================================

// TestWAC_ApplyMovement_Inbound_HandComputed verifies the brief's exact numbers:
// old(qty=10,cost=5) + in(qty=10,cost=7) -> newQty=20, newCost=6.000000, TotalCost=70.
func TestWAC_ApplyMovement_Inbound_HandComputed(t *testing.T) {
	snap := &domain.Snapshot{ID: uuid.New(), TenantID: testTenantID, ProductID: testProductID, WarehouseID: testWarehouseID,
		OnHandQty: d("10"), UnitCost: d("5")}
	calc := appstock.NewCalculator(stubProfile{"wac"}, newMockRepo(snap))

	m := newMovement(testTenantID, testProductID, testWarehouseID, domain.DirectionIn, d("10"), d("7"), domain.RefPurchase)
	result, err := calc.ApplyMovement(context.Background(), nil, m)
	if err != nil {
		t.Fatalf("ApplyMovement error: %v", err)
	}
	if !result.OnHandQty.Equal(d("20")) {
		t.Errorf("OnHandQty = %s, want 20", result.OnHandQty)
	}
	if !result.UnitCost.Equal(d("6")) {
		t.Errorf("UnitCost = %s, want 6.000000", result.UnitCost)
	}
	if !m.TotalCost.Equal(d("70")) {
		t.Errorf("m.TotalCost = %s, want 70", m.TotalCost)
	}
}

// TestWAC_ApplyMovement_Inbound_ZeroQtyEdge: oldQty=0 and inbound qty=0 -> newQty=0 -> newCost=0.
func TestWAC_ApplyMovement_Inbound_ZeroQtyEdge(t *testing.T) {
	calc := appstock.NewCalculator(stubProfile{"wac"}, newMockRepo(nil))
	m := newMovement(testTenantID, testProductID, testWarehouseID, domain.DirectionIn, d("0"), d("5"), domain.RefPurchase)
	result, err := calc.ApplyMovement(context.Background(), nil, m)
	if err != nil {
		t.Fatalf("ApplyMovement error: %v", err)
	}
	if !result.OnHandQty.IsZero() {
		t.Errorf("OnHandQty = %s, want 0", result.OnHandQty)
	}
	if !result.UnitCost.IsZero() {
		t.Errorf("UnitCost = %s, want 0", result.UnitCost)
	}
}

// TestWAC_ApplyMovement_Adjust_BusinessInvariants: cost unchanged, TotalCost=|qty|*oldCost,
// oversell (beyond available) returns InsufficientStockError.
func TestWAC_ApplyMovement_Adjust_BusinessInvariants(t *testing.T) {
	snap := &domain.Snapshot{ID: uuid.New(), TenantID: testTenantID, ProductID: testProductID, WarehouseID: testWarehouseID,
		OnHandQty: d("10"), UnitCost: d("7")}
	calc := appstock.NewCalculator(stubProfile{"wac"}, newMockRepo(snap))

	m := newMovement(testTenantID, testProductID, testWarehouseID, domain.DirectionAdjust, d("-5"), d("0"), domain.RefAdjust)
	result, err := calc.ApplyMovement(context.Background(), nil, m)
	if err != nil {
		t.Fatalf("ApplyMovement error: %v", err)
	}
	if !result.OnHandQty.Equal(d("5")) {
		t.Errorf("OnHandQty = %s, want 5", result.OnHandQty)
	}
	if !result.UnitCost.Equal(d("7")) {
		t.Errorf("UnitCost = %s, want 7 (unchanged)", result.UnitCost)
	}
	if !m.UnitCost.Equal(d("7")) {
		t.Errorf("m.UnitCost = %s, want 7", m.UnitCost)
	}
	if !m.TotalCost.Equal(d("35")) {
		t.Errorf("m.TotalCost = %s, want 35 (5*7)", m.TotalCost)
	}

	// Beyond available: oldQty=10 (after the -5 adjust it's now 5), request -20 -> negative newQty.
	m2 := newMovement(testTenantID, testProductID, testWarehouseID, domain.DirectionAdjust, d("-20"), d("0"), domain.RefAdjust)
	_, err = calc.ApplyMovement(context.Background(), nil, m2)
	if err == nil || !appstock.IsInsufficientStock(err) {
		t.Fatalf("expected InsufficientStockError for adjust beyond available, got %v", err)
	}
}

func TestWAC_ApplyMovement_ErrorPropagation(t *testing.T) {
	snap := &domain.Snapshot{ID: uuid.New(), TenantID: testTenantID, ProductID: testProductID, WarehouseID: testWarehouseID,
		OnHandQty: d("10"), UnitCost: d("5")}

	t.Run("select for update error", func(t *testing.T) {
		repo := newMockRepo(snap)
		repo.selectForUpdateErr = errors.New("conn reset")
		calc := appstock.NewCalculator(stubProfile{"wac"}, repo)
		m := newMovement(testTenantID, testProductID, testWarehouseID, domain.DirectionIn, d("1"), d("1"), domain.RefPurchase)
		_, err := calc.ApplyMovement(context.Background(), nil, m)
		if err == nil || !strings.Contains(err.Error(), "wac apply: load snapshot:") {
			t.Fatalf("expected wrapped 'wac apply: load snapshot:' error, got %v", err)
		}
	})

	t.Run("insert movement error", func(t *testing.T) {
		repo := newErrRepo(snap)
		repo.insertMovementErr = errors.New("insert failed")
		calc := appstock.NewCalculator(stubProfile{"wac"}, repo)
		m := newMovement(testTenantID, testProductID, testWarehouseID, domain.DirectionIn, d("1"), d("1"), domain.RefPurchase)
		_, err := calc.ApplyMovement(context.Background(), nil, m)
		if err == nil || !strings.Contains(err.Error(), "wac apply: insert movement:") {
			t.Fatalf("expected wrapped 'wac apply: insert movement:' error, got %v", err)
		}
	})

	t.Run("upsert snapshot error", func(t *testing.T) {
		repo := newErrRepo(snap)
		repo.upsertSnapshotErr = errors.New("upsert failed")
		calc := appstock.NewCalculator(stubProfile{"wac"}, repo)
		m := newMovement(testTenantID, testProductID, testWarehouseID, domain.DirectionIn, d("1"), d("1"), domain.RefPurchase)
		_, err := calc.ApplyMovement(context.Background(), nil, m)
		if err == nil || !strings.Contains(err.Error(), "wac apply: upsert snapshot:") {
			t.Fatalf("expected wrapped 'wac apply: upsert snapshot:' error, got %v", err)
		}
	})
}

// ===========================================================================
// FIFO — ValidateMovement edge cases
// ===========================================================================

func TestFIFO_ValidateMovement_AdjustCases(t *testing.T) {
	snap := &domain.Snapshot{ID: uuid.New(), TenantID: testTenantID, ProductID: testProductID, WarehouseID: testWarehouseID,
		OnHandQty: d("10"), UnitCost: d("5")}

	cases := []struct {
		name    string
		qty     string
		wantErr bool
	}{
		{"adjust positive", "3", false},
		{"adjust negative within available", "-10", false},
		{"adjust negative beyond available", "-11", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calc := appstock.NewCalculator(stubProfile{"fifo"}, newMockRepo(snap))
			m := newMovement(testTenantID, testProductID, testWarehouseID, domain.DirectionAdjust, d(tc.qty), d("0"), domain.RefAdjust)
			err := calc.ValidateMovement(context.Background(), nil, m)
			if tc.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
		})
	}
}

func TestFIFO_ValidateMovement_Out_NilSnapshot_InsufficientStock(t *testing.T) {
	calc := appstock.NewCalculator(stubProfile{"fifo"}, newMockRepo(nil))
	m := newMovement(testTenantID, testProductID, testWarehouseID, domain.DirectionOut, d("1"), d("0"), domain.RefSale)
	err := calc.ValidateMovement(context.Background(), nil, m)
	if err == nil || !appstock.IsInsufficientStock(err) {
		t.Fatalf("expected InsufficientStockError for out against nil snapshot, got %v", err)
	}
}

func TestFIFO_ValidateMovement_SelectForUpdateError_Propagated(t *testing.T) {
	repo := newMockRepo(nil)
	repo.selectForUpdateErr = errors.New("db down")
	calc := appstock.NewCalculator(stubProfile{"fifo"}, repo)
	m := newMovement(testTenantID, testProductID, testWarehouseID, domain.DirectionOut, d("1"), d("0"), domain.RefSale)
	err := calc.ValidateMovement(context.Background(), nil, m)
	if err == nil || !strings.Contains(err.Error(), "fifo validate:") || !strings.Contains(err.Error(), "db down") {
		t.Fatalf("expected wrapped 'fifo validate:' error, got %v", err)
	}
}

// ===========================================================================
// FIFO — ApplyMovement hand-computed formulas and error propagation
// ===========================================================================

// TestFIFO_ApplyMovement_Outbound_DrainsOldestLotsFirst_HandComputed verifies the
// brief's exact numbers: lots [qty2@cost4, qty3@cost6], out qty=4 ->
// consume 2@4 + 2@6, totalCost=8+12=20, m.UnitCost=20/4=5.000000,
// lot1 remaining 0, lot2 remaining 1.
func TestFIFO_ApplyMovement_Outbound_DrainsOldestLotsFirst_HandComputed(t *testing.T) {
	now := time.Now().UTC()
	lot1ID, lot2ID := uuid.New(), uuid.New()
	snap := &domain.Snapshot{ID: uuid.New(), TenantID: testTenantID, ProductID: testProductID, WarehouseID: testWarehouseID,
		OnHandQty: d("5"), UnitCost: d("5")}
	repo := newMockRepo(snap)
	repo.lots = []domain.Lot{
		{ID: lot1ID, TenantID: testTenantID, ProductID: testProductID, WarehouseID: testWarehouseID,
			Qty: d("2"), QtyRemaining: d("2"), UnitCost: d("4"), ReceivedAt: now.Add(-time.Hour)},
		{ID: lot2ID, TenantID: testTenantID, ProductID: testProductID, WarehouseID: testWarehouseID,
			Qty: d("3"), QtyRemaining: d("3"), UnitCost: d("6"), ReceivedAt: now},
	}
	calc := appstock.NewCalculator(stubProfile{"fifo"}, repo)

	m := newMovement(testTenantID, testProductID, testWarehouseID, domain.DirectionOut, d("4"), d("0"), domain.RefSale)
	result, err := calc.ApplyMovement(context.Background(), nil, m)
	if err != nil {
		t.Fatalf("ApplyMovement error: %v", err)
	}

	if !result.OnHandQty.Equal(d("1")) {
		t.Errorf("OnHandQty = %s, want 1", result.OnHandQty)
	}
	if !result.UnitCost.Equal(d("5")) {
		t.Errorf("snapshot UnitCost = %s, want 5 (unchanged aggregate cost)", result.UnitCost)
	}
	if !m.UnitCost.Equal(d("5")) {
		t.Errorf("m.UnitCost = %s, want 5.000000 (20/4)", m.UnitCost)
	}
	if !m.TotalCost.Equal(d("20")) {
		t.Errorf("m.TotalCost = %s, want 20 (8+12)", m.TotalCost)
	}

	var gotLot1, gotLot2 *domain.Lot
	for i := range repo.lots {
		if repo.lots[i].ID == lot1ID {
			gotLot1 = &repo.lots[i]
		}
		if repo.lots[i].ID == lot2ID {
			gotLot2 = &repo.lots[i]
		}
	}
	if gotLot1 == nil || !gotLot1.QtyRemaining.IsZero() {
		t.Errorf("lot1.QtyRemaining = %v, want 0", gotLot1)
	}
	if gotLot2 == nil || !gotLot2.QtyRemaining.Equal(d("1")) {
		t.Errorf("lot2.QtyRemaining = %v, want 1", gotLot2)
	}
}

// TestFIFO_ApplyMovement_Outbound_LotsExhausted_HardError: snapshot says qty=5 is
// available but the lots only sum to 3 (out-of-sync data) -> hard error, not silent
// oversell.
func TestFIFO_ApplyMovement_Outbound_LotsExhausted_HardError(t *testing.T) {
	snap := &domain.Snapshot{ID: uuid.New(), TenantID: testTenantID, ProductID: testProductID, WarehouseID: testWarehouseID,
		OnHandQty: d("5"), UnitCost: d("5")}
	repo := newMockRepo(snap)
	repo.lots = []domain.Lot{
		{ID: uuid.New(), TenantID: testTenantID, ProductID: testProductID, WarehouseID: testWarehouseID,
			Qty: d("3"), QtyRemaining: d("3"), UnitCost: d("5"), ReceivedAt: time.Now().UTC()},
	}
	calc := appstock.NewCalculator(stubProfile{"fifo"}, repo)

	m := newMovement(testTenantID, testProductID, testWarehouseID, domain.DirectionOut, d("4"), d("0"), domain.RefSale)
	_, err := calc.ApplyMovement(context.Background(), nil, m)
	if err == nil {
		t.Fatal("expected hard error when lots are exhausted before qty satisfied, got nil")
	}
	if !strings.Contains(err.Error(), "lots exhausted before qty satisfied") ||
		!strings.Contains(err.Error(), "snapshot/lots out of sync") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestFIFO_ApplyMovement_Adjust_BusinessInvariants(t *testing.T) {
	snap := &domain.Snapshot{ID: uuid.New(), TenantID: testTenantID, ProductID: testProductID, WarehouseID: testWarehouseID,
		OnHandQty: d("10"), UnitCost: d("7")}
	calc := appstock.NewCalculator(stubProfile{"fifo"}, newMockRepo(snap))

	m := newMovement(testTenantID, testProductID, testWarehouseID, domain.DirectionAdjust, d("-5"), d("0"), domain.RefAdjust)
	result, err := calc.ApplyMovement(context.Background(), nil, m)
	if err != nil {
		t.Fatalf("ApplyMovement error: %v", err)
	}
	if !result.OnHandQty.Equal(d("5")) {
		t.Errorf("OnHandQty = %s, want 5", result.OnHandQty)
	}
	if !m.TotalCost.Equal(d("35")) {
		t.Errorf("m.TotalCost = %s, want 35 (5*7)", m.TotalCost)
	}

	m2 := newMovement(testTenantID, testProductID, testWarehouseID, domain.DirectionAdjust, d("-20"), d("0"), domain.RefAdjust)
	_, err = calc.ApplyMovement(context.Background(), nil, m2)
	if err == nil || !appstock.IsInsufficientStock(err) {
		t.Fatalf("expected InsufficientStockError for adjust beyond available, got %v", err)
	}
}

func TestFIFO_ApplyMovement_ErrorPropagation(t *testing.T) {
	snap := &domain.Snapshot{ID: uuid.New(), TenantID: testTenantID, ProductID: testProductID, WarehouseID: testWarehouseID,
		OnHandQty: d("10"), UnitCost: d("5")}

	t.Run("select for update error", func(t *testing.T) {
		repo := newMockRepo(nil)
		repo.selectForUpdateErr = errors.New("conn reset")
		calc := appstock.NewCalculator(stubProfile{"fifo"}, repo)
		m := newMovement(testTenantID, testProductID, testWarehouseID, domain.DirectionIn, d("1"), d("1"), domain.RefPurchase)
		_, err := calc.ApplyMovement(context.Background(), nil, m)
		if err == nil || !strings.Contains(err.Error(), "fifo apply: load snapshot:") {
			t.Fatalf("expected wrapped 'fifo apply: load snapshot:' error, got %v", err)
		}
	})

	t.Run("insert lot error", func(t *testing.T) {
		repo := newErrRepo(nil)
		repo.insertLotErr = errors.New("insert lot failed")
		calc := appstock.NewCalculator(stubProfile{"fifo"}, repo)
		m := newMovement(testTenantID, testProductID, testWarehouseID, domain.DirectionIn, d("1"), d("1"), domain.RefPurchase)
		_, err := calc.ApplyMovement(context.Background(), nil, m)
		if err == nil || !strings.Contains(err.Error(), "fifo apply: insert lot:") {
			t.Fatalf("expected wrapped 'fifo apply: insert lot:' error, got %v", err)
		}
	})

	t.Run("insert movement error", func(t *testing.T) {
		repo := newErrRepo(nil)
		repo.insertMovementErr = errors.New("insert movement failed")
		calc := appstock.NewCalculator(stubProfile{"fifo"}, repo)
		m := newMovement(testTenantID, testProductID, testWarehouseID, domain.DirectionIn, d("1"), d("1"), domain.RefPurchase)
		_, err := calc.ApplyMovement(context.Background(), nil, m)
		if err == nil || !strings.Contains(err.Error(), "fifo apply: insert movement:") {
			t.Fatalf("expected wrapped 'fifo apply: insert movement:' error, got %v", err)
		}
	})

	t.Run("upsert snapshot error", func(t *testing.T) {
		repo := newErrRepo(nil)
		repo.upsertSnapshotErr = errors.New("upsert failed")
		calc := appstock.NewCalculator(stubProfile{"fifo"}, repo)
		m := newMovement(testTenantID, testProductID, testWarehouseID, domain.DirectionIn, d("1"), d("1"), domain.RefPurchase)
		_, err := calc.ApplyMovement(context.Background(), nil, m)
		if err == nil || !strings.Contains(err.Error(), "fifo apply: upsert snapshot:") {
			t.Fatalf("expected wrapped 'fifo apply: upsert snapshot:' error, got %v", err)
		}
	})

	t.Run("list active lots error", func(t *testing.T) {
		repo := newErrRepo(snap)
		repo.listActiveLotsErr = errors.New("list lots failed")
		calc := appstock.NewCalculator(stubProfile{"fifo"}, repo)
		m := newMovement(testTenantID, testProductID, testWarehouseID, domain.DirectionOut, d("1"), d("0"), domain.RefSale)
		_, err := calc.ApplyMovement(context.Background(), nil, m)
		if err == nil || !strings.Contains(err.Error(), "fifo apply: list lots:") {
			t.Fatalf("expected wrapped 'fifo apply: list lots:' error, got %v", err)
		}
	})

	t.Run("update lot qty error", func(t *testing.T) {
		repo := newErrRepo(snap)
		repo.lots = []domain.Lot{
			{ID: uuid.New(), TenantID: testTenantID, ProductID: testProductID, WarehouseID: testWarehouseID,
				Qty: d("10"), QtyRemaining: d("10"), UnitCost: d("5"), ReceivedAt: time.Now().UTC()},
		}
		repo.updateLotQtyErr = errors.New("update lot failed")
		calc := appstock.NewCalculator(stubProfile{"fifo"}, repo)
		m := newMovement(testTenantID, testProductID, testWarehouseID, domain.DirectionOut, d("1"), d("0"), domain.RefSale)
		_, err := calc.ApplyMovement(context.Background(), nil, m)
		if err == nil || !strings.Contains(err.Error(), "fifo apply: update lot qty:") {
			t.Fatalf("expected wrapped 'fifo apply: update lot qty:' error, got %v", err)
		}
	})
}

// ===========================================================================
// query.go — GetSnapshotUseCase / ListSnapshotsUseCase / ListMovementsUseCase
// ===========================================================================

func TestGetSnapshotUseCase_Execute(t *testing.T) {
	snap := &domain.Snapshot{ID: uuid.New(), OnHandQty: d("10")}

	t.Run("success", func(t *testing.T) {
		uc := appstock.NewGetSnapshotUseCase(newErrRepo(snap))
		got, err := uc.Execute(context.Background(), testTenantID, testProductID, testWarehouseID)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if got == nil || !got.OnHandQty.Equal(d("10")) {
			t.Errorf("got = %v, want OnHandQty=10", got)
		}
	})

	t.Run("error", func(t *testing.T) {
		repo := newErrRepo(nil)
		repo.getSnapshotErr = errors.New("db down")
		uc := appstock.NewGetSnapshotUseCase(repo)
		_, err := uc.Execute(context.Background(), testTenantID, testProductID, testWarehouseID)
		if err == nil || !strings.Contains(err.Error(), "get snapshot:") {
			t.Fatalf("expected wrapped 'get snapshot:' error, got %v", err)
		}
	})
}

func TestListSnapshotsUseCase_Execute(t *testing.T) {
	t.Run("default limit applied when zero", func(t *testing.T) {
		repo := newErrRepo(nil)
		uc := appstock.NewListSnapshotsUseCase(repo)
		_, err := uc.Execute(context.Background(), appstock.ListSnapshotsFilter{TenantID: testTenantID})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if repo.lastSnapshotFilter.Limit != 20 {
			t.Errorf("Limit = %d, want 20 (default)", repo.lastSnapshotFilter.Limit)
		}
	})

	t.Run("explicit limit preserved", func(t *testing.T) {
		repo := newErrRepo(nil)
		uc := appstock.NewListSnapshotsUseCase(repo)
		_, err := uc.Execute(context.Background(), appstock.ListSnapshotsFilter{TenantID: testTenantID, Limit: 7})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if repo.lastSnapshotFilter.Limit != 7 {
			t.Errorf("Limit = %d, want 7", repo.lastSnapshotFilter.Limit)
		}
	})

	t.Run("error propagated", func(t *testing.T) {
		repo := newErrRepo(nil)
		repo.listSnapshotsErr = errors.New("boom")
		uc := appstock.NewListSnapshotsUseCase(repo)
		_, err := uc.Execute(context.Background(), appstock.ListSnapshotsFilter{TenantID: testTenantID})
		if err == nil || !strings.Contains(err.Error(), "list snapshots:") {
			t.Fatalf("expected wrapped 'list snapshots:' error, got %v", err)
		}
	})
}

func TestListMovementsUseCase_Execute(t *testing.T) {
	t.Run("default limit applied when zero", func(t *testing.T) {
		repo := newErrRepo(nil)
		uc := appstock.NewListMovementsUseCase(repo)
		_, err := uc.Execute(context.Background(), appstock.MovementFilter{TenantID: testTenantID})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if repo.lastMovementFilter.Limit != 50 {
			t.Errorf("Limit = %d, want 50 (default)", repo.lastMovementFilter.Limit)
		}
	})

	t.Run("explicit limit preserved", func(t *testing.T) {
		repo := newErrRepo(nil)
		uc := appstock.NewListMovementsUseCase(repo)
		_, err := uc.Execute(context.Background(), appstock.MovementFilter{TenantID: testTenantID, Limit: 9})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if repo.lastMovementFilter.Limit != 9 {
			t.Errorf("Limit = %d, want 9", repo.lastMovementFilter.Limit)
		}
	})

	t.Run("error propagated", func(t *testing.T) {
		repo := newErrRepo(nil)
		repo.listMovementsErr = errors.New("boom")
		uc := appstock.NewListMovementsUseCase(repo)
		_, err := uc.Execute(context.Background(), appstock.MovementFilter{TenantID: testTenantID})
		if err == nil || !strings.Contains(err.Error(), "list movements:") {
			t.Fatalf("expected wrapped 'list movements:' error, got %v", err)
		}
	})
}

// ===========================================================================
// RecordMovementUseCase.Execute — nil guards, unit conversion, outbox, ValidateMovement pass-through
// ===========================================================================

func baseRequest() appstock.RecordMovementRequest {
	return appstock.RecordMovementRequest{
		TenantID:      testTenantID,
		ProductID:     testProductID,
		WarehouseID:   testWarehouseID,
		Direction:     domain.DirectionIn,
		Qty:           d("10"),
		UnitCost:      d("5"),
		ReferenceType: domain.RefPurchase,
	}
}

func TestRecordMovementUseCase_Execute_NilGuards(t *testing.T) {
	uc, _ := newUseCase(nil, "wac")

	t.Run("missing product id", func(t *testing.T) {
		req := baseRequest()
		req.ProductID = uuid.Nil
		if _, err := uc.Execute(context.Background(), req); err == nil {
			t.Fatal("expected error for missing ProductID")
		}
	})

	t.Run("missing warehouse id", func(t *testing.T) {
		req := baseRequest()
		req.WarehouseID = uuid.Nil
		if _, err := uc.Execute(context.Background(), req); err == nil {
			t.Fatal("expected error for missing WarehouseID")
		}
	})

	t.Run("invalid direction", func(t *testing.T) {
		req := baseRequest()
		req.Direction = domain.Direction("bogus")
		_, err := uc.Execute(context.Background(), req)
		if err == nil || !errors.Is(err, domain.ErrInvalidDirection) {
			t.Fatalf("expected ErrInvalidDirection, got %v", err)
		}
	})

	t.Run("invalid conv factor string (non-numeric)", func(t *testing.T) {
		req := baseRequest()
		req.ConvFactor = "abc"
		_, err := uc.Execute(context.Background(), req)
		if err == nil || !strings.Contains(err.Error(), "invalid conversion factor") {
			t.Fatalf("expected invalid conversion factor error, got %v", err)
		}
	})
}

func TestRecordMovementUseCase_Execute_ConvFactor_UnchangedCases(t *testing.T) {
	for _, factor := range []string{"", "1"} {
		t.Run("factor="+factor, func(t *testing.T) {
			uc, repo := newUseCase(nil, "wac")
			req := baseRequest()
			req.ConvFactor = factor
			req.Qty = d("42")
			snap, err := uc.Execute(context.Background(), req)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if !snap.OnHandQty.Equal(d("42")) {
				t.Errorf("OnHandQty = %s, want 42 (unchanged qty)", snap.OnHandQty)
			}
			if !repo.movements[0].QtyBase.Equal(d("42")) {
				t.Errorf("QtyBase = %s, want 42", repo.movements[0].QtyBase)
			}
		})
	}
}

func TestRecordMovementUseCase_Execute_ValidateFailure_ReturnsInsufficientStockUnwrapped(t *testing.T) {
	uc, _ := newUseCase(nil, "wac") // nil snapshot -> available = 0
	req := baseRequest()
	req.Direction = domain.DirectionOut
	req.Qty = d("5")

	_, err := uc.Execute(context.Background(), req)
	if err == nil {
		t.Fatal("expected InsufficientStockError")
	}
	if !appstock.IsInsufficientStock(err) {
		t.Errorf("expected *InsufficientStockError, got %T: %v", err, err)
	}
}

func TestRecordMovementUseCase_Execute_RefInit_SelfReferencesID(t *testing.T) {
	uc, repo := newUseCase(nil, "wac")
	req := baseRequest()
	req.ReferenceType = domain.RefInit
	req.ReferenceID = nil

	_, err := uc.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(repo.movements) != 1 {
		t.Fatalf("movements = %d, want 1", len(repo.movements))
	}
	mv := repo.movements[0]
	if mv.ReferenceID == nil || *mv.ReferenceID != mv.ID {
		t.Errorf("ReferenceID = %v, want self-reference to %s", mv.ReferenceID, mv.ID)
	}
}

func TestRecordMovementUseCase_Execute_Outbox(t *testing.T) {
	t.Run("success enqueues in same tx", func(t *testing.T) {
		repo := newMockRepo(nil)
		calc := appstock.NewCalculator(stubProfile{"wac"}, repo)
		ob := &fakeOutbox{}
		uc := appstock.NewRecordMovementUseCase(repo, calc, ob, nil)

		_, err := uc.Execute(context.Background(), baseRequest())
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if !ob.called {
			t.Error("expected outbox.Enqueue to be called")
		}
		if ob.subject != "PSI_EVENTS.stock.movement_recorded" {
			t.Errorf("subject = %q, want PSI_EVENTS.stock.movement_recorded", ob.subject)
		}
	})

	t.Run("error wraps and aborts commit", func(t *testing.T) {
		repo := newMockRepo(nil)
		calc := appstock.NewCalculator(stubProfile{"wac"}, repo)
		ob := &fakeOutbox{err: errors.New("nats down")}
		uc := appstock.NewRecordMovementUseCase(repo, calc, ob, nil)

		_, err := uc.Execute(context.Background(), baseRequest())
		if err == nil || !strings.Contains(err.Error(), "enqueue outbox:") {
			t.Fatalf("expected wrapped 'enqueue outbox:' error, got %v", err)
		}
	})

	t.Run("nil outbox is skipped silently", func(t *testing.T) {
		uc, _ := newUseCase(nil, "wac")
		if _, err := uc.Execute(context.Background(), baseRequest()); err != nil {
			t.Fatalf("Execute with nil outbox: %v", err)
		}
	})
}

// ===========================================================================
// RecordMovementUseCase.ExecuteInTx
// ===========================================================================

func TestRecordMovementUseCase_ExecuteInTx_NilGuards(t *testing.T) {
	uc, _ := newUseCase(nil, "wac")

	cases := []struct {
		name string
		mut  func(*appstock.RecordMovementRequest)
	}{
		{"missing tenant id", func(r *appstock.RecordMovementRequest) { r.TenantID = uuid.Nil }},
		{"missing product id", func(r *appstock.RecordMovementRequest) { r.ProductID = uuid.Nil }},
		{"missing warehouse id", func(r *appstock.RecordMovementRequest) { r.WarehouseID = uuid.Nil }},
		{"invalid direction", func(r *appstock.RecordMovementRequest) { r.Direction = domain.Direction("nope") }},
		{"invalid conv factor", func(r *appstock.RecordMovementRequest) { r.ConvFactor = "-1" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := baseRequest()
			tc.mut(&req)
			if _, err := uc.ExecuteInTx(context.Background(), nil, req); err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestRecordMovementUseCase_ExecuteInTx_Success(t *testing.T) {
	uc, repo := newUseCase(nil, "wac")
	snap, err := uc.ExecuteInTx(context.Background(), nil, baseRequest())
	if err != nil {
		t.Fatalf("ExecuteInTx: %v", err)
	}
	if !snap.OnHandQty.Equal(d("10")) {
		t.Errorf("OnHandQty = %s, want 10", snap.OnHandQty)
	}
	if len(repo.movements) != 1 {
		t.Errorf("movements = %d, want 1", len(repo.movements))
	}
}

func TestRecordMovementUseCase_ExecuteInTx_ApplyMovementError(t *testing.T) {
	repo := newErrRepo(nil)
	repo.insertMovementErr = errors.New("insert failed")
	calc := appstock.NewCalculator(stubProfile{"wac"}, repo)
	uc := appstock.NewRecordMovementUseCase(repo, calc, nil, nil)

	_, err := uc.ExecuteInTx(context.Background(), nil, baseRequest())
	if err == nil || !strings.Contains(err.Error(), "record movement (tx): apply movement:") {
		t.Fatalf("expected wrapped 'record movement (tx): apply movement:' error, got %v", err)
	}
}

func TestRecordMovementUseCase_ExecuteInTx_Outbox(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo := newMockRepo(nil)
		calc := appstock.NewCalculator(stubProfile{"wac"}, repo)
		ob := &fakeOutbox{}
		uc := appstock.NewRecordMovementUseCase(repo, calc, ob, nil)

		_, err := uc.ExecuteInTx(context.Background(), nil, baseRequest())
		if err != nil {
			t.Fatalf("ExecuteInTx: %v", err)
		}
		if !ob.called {
			t.Error("expected outbox.Enqueue to be called")
		}
	})

	t.Run("error", func(t *testing.T) {
		repo := newMockRepo(nil)
		calc := appstock.NewCalculator(stubProfile{"wac"}, repo)
		ob := &fakeOutbox{err: errors.New("nats down")}
		uc := appstock.NewRecordMovementUseCase(repo, calc, ob, nil)

		_, err := uc.ExecuteInTx(context.Background(), nil, baseRequest())
		if err == nil || !strings.Contains(err.Error(), "record movement (tx): enqueue outbox:") {
			t.Fatalf("expected wrapped 'record movement (tx): enqueue outbox:' error, got %v", err)
		}
	})
}

// ===========================================================================
// Concurrency: AcquireAdvisoryLock must always precede SelectForUpdate for the
// same (tenant,product,warehouse) key, even under -race with distinct SKUs
// running concurrently.
// ===========================================================================

type raceRepo struct {
	mu      sync.Mutex
	locked  map[string]bool
	snap    map[string]*domain.Snapshot
	violate bool
}

func newRaceRepo() *raceRepo {
	return &raceRepo{locked: map[string]bool{}, snap: map[string]*domain.Snapshot{}}
}

func raceKey(t, p, w uuid.UUID) string {
	return t.String() + "|" + p.String() + "|" + w.String()
}

func (r *raceRepo) GetSnapshot(_ context.Context, t, p, w uuid.UUID) (*domain.Snapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.snap[raceKey(t, p, w)], nil
}

func (r *raceRepo) SelectForUpdate(_ context.Context, _ *sql.Tx, t, p, w uuid.UUID) (*domain.Snapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := raceKey(t, p, w)
	if !r.locked[k] {
		r.violate = true
	}
	return r.snap[k], nil
}

func (r *raceRepo) UpsertSnapshot(_ context.Context, _ *sql.Tx, s *domain.Snapshot) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.snap[raceKey(s.TenantID, s.ProductID, s.WarehouseID)] = s
	return nil
}

func (r *raceRepo) InsertMovement(_ context.Context, _ *sql.Tx, _ *domain.Movement) error { return nil }

func (r *raceRepo) ListMovements(_ context.Context, _ appstock.MovementFilter) ([]domain.Movement, error) {
	return nil, nil
}

func (r *raceRepo) InsertLot(_ context.Context, _ *sql.Tx, _ *domain.Lot) error { return nil }

func (r *raceRepo) ListActiveLots(_ context.Context, _ *sql.Tx, _, _, _ uuid.UUID) ([]domain.Lot, error) {
	return nil, nil
}

func (r *raceRepo) UpdateLotQty(_ context.Context, _ *sql.Tx, _ uuid.UUID, _ decimal.Decimal) error {
	return nil
}

func (r *raceRepo) AcquireAdvisoryLock(_ context.Context, _ *sql.Tx, t, p, w uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.locked[raceKey(t, p, w)] = true
	return nil
}

func (r *raceRepo) ListSnapshots(_ context.Context, _ appstock.ListSnapshotsFilter) ([]domain.Snapshot, error) {
	return nil, nil
}

func (r *raceRepo) WithTx(_ context.Context, fn func(tx *sql.Tx) error) error {
	return fn(nil)
}

var _ appstock.StockRepo = (*raceRepo)(nil)

func TestRecordMovementUseCase_Execute_Concurrent_DifferentSKUs_RaceSafe(t *testing.T) {
	repo := newRaceRepo()
	calc := appstock.NewCalculator(stubProfile{"wac"}, repo)
	uc := appstock.NewRecordMovementUseCase(repo, calc, nil, nil)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := baseRequest()
			req.ProductID = uuid.New() // distinct SKU per goroutine
			if _, err := uc.Execute(context.Background(), req); err != nil {
				t.Errorf("Execute: %v", err)
			}
		}()
	}
	wg.Wait()

	repo.mu.Lock()
	violated := repo.violate
	repo.mu.Unlock()
	if violated {
		t.Error("SelectForUpdate observed before AcquireAdvisoryLock for the same key")
	}
}
