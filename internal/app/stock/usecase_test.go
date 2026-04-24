package stock_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
)

func newUseCase(snap *domain.Snapshot, strategy string) (*appstock.RecordMovementUseCase, *mockRepo) {
	repo := newMockRepo(snap)
	calc := appstock.NewCalculator(stubProfile{strategy}, repo)
	uc := appstock.NewRecordMovementUseCase(repo, calc, nil, nil)
	return uc, repo
}

// TestRecordMovement_FIFO_Inbound_CommitsAll verifies that after Execute:
// - 1 movement is recorded, 1 lot created, snapshot updated.
func TestRecordMovement_FIFO_Inbound_CommitsAll(t *testing.T) {
	uc, repo := newUseCase(nil, "fifo")

	req := appstock.RecordMovementRequest{
		TenantID:      testTenantID,
		ProductID:     testProductID,
		WarehouseID:   testWarehouseID,
		Direction:     domain.DirectionIn,
		Qty:           d("100"),
		UnitCost:      d("8"),
		CostStrategy:  "fifo",
		ReferenceType: domain.RefPurchase,
	}
	snap, err := uc.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !snap.OnHandQty.Equal(d("100")) {
		t.Errorf("OnHandQty = %s, want 100", snap.OnHandQty)
	}
	if len(repo.movements) != 1 {
		t.Errorf("movements count = %d, want 1", len(repo.movements))
	}
	if len(repo.lots) != 1 {
		t.Errorf("lots count = %d, want 1", len(repo.lots))
	}
	if !repo.lots[0].QtyRemaining.Equal(d("100")) {
		t.Errorf("lots[0].QtyRemaining = %s, want 100", repo.lots[0].QtyRemaining)
	}
}

// TestRecordMovement_WAC_Inbound_CommitsAll: WAC produces no lots.
func TestRecordMovement_WAC_Inbound_CommitsAll(t *testing.T) {
	uc, repo := newUseCase(nil, "wac")

	req := appstock.RecordMovementRequest{
		TenantID:      testTenantID,
		ProductID:     testProductID,
		WarehouseID:   testWarehouseID,
		Direction:     domain.DirectionIn,
		Qty:           d("50"),
		UnitCost:      d("12"),
		CostStrategy:  "wac",
		ReferenceType: domain.RefPurchase,
	}
	snap, err := uc.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !snap.OnHandQty.Equal(d("50")) {
		t.Errorf("OnHandQty = %s, want 50", snap.OnHandQty)
	}
	if len(repo.movements) != 1 {
		t.Errorf("movements count = %d, want 1", len(repo.movements))
	}
	if len(repo.lots) != 0 {
		t.Errorf("lots count = %d, want 0 (WAC has no lots)", len(repo.lots))
	}
}

// TestRecordMovement_UnitConversion_AppliedBeforeCalc verifies AC-5:
// qty=1 + factor="500" → qty_base=500 → snapshot.on_hand_qty=500.
func TestRecordMovement_UnitConversion_AppliedBeforeCalc(t *testing.T) {
	uc, repo := newUseCase(nil, "wac")

	req := appstock.RecordMovementRequest{
		TenantID:      testTenantID,
		ProductID:     testProductID,
		WarehouseID:   testWarehouseID,
		Direction:     domain.DirectionIn,
		Qty:           d("1"),
		ConvFactor:    "500", // 1 pack = 500g
		UnitCost:      d("2"),
		CostStrategy:  "wac",
		ReferenceType: domain.RefPurchase,
	}
	snap, err := uc.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !snap.OnHandQty.Equal(d("500")) {
		t.Errorf("OnHandQty = %s, want 500", snap.OnHandQty)
	}
	if !repo.movements[0].QtyBase.Equal(d("500")) {
		t.Errorf("movement.QtyBase = %s, want 500", repo.movements[0].QtyBase)
	}
}

// TestRecordMovement_AdvisoryLock_AcquiredBeforeApply tracks that advisory lock is called
// inside the transaction (via WithTx wrapper).
func TestRecordMovement_AdvisoryLock_AcquiredBeforeApply(t *testing.T) {
	inner := newMockRepo(nil)
	lockCount := 0
	wrapped := &countingLockRepo{inner: inner, count: &lockCount}
	calc := appstock.NewCalculator(stubProfile{"wac"}, wrapped)
	uc := appstock.NewRecordMovementUseCase(wrapped, calc, nil, nil)

	req := appstock.RecordMovementRequest{
		TenantID:      testTenantID,
		ProductID:     testProductID,
		WarehouseID:   testWarehouseID,
		Direction:     domain.DirectionIn,
		Qty:           d("10"),
		UnitCost:      d("5"),
		ReferenceType: domain.RefPurchase,
	}
	_, err := uc.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if lockCount != 1 {
		t.Errorf("advisory lock acquired %d times, want 1", lockCount)
	}
}

// TestRecordMovement_MissingTenantID_ReturnsError validates required field guard.
func TestRecordMovement_MissingTenantID_ReturnsError(t *testing.T) {
	uc, _ := newUseCase(nil, "wac")
	req := appstock.RecordMovementRequest{
		ProductID:     testProductID,
		WarehouseID:   testWarehouseID,
		Direction:     domain.DirectionIn,
		Qty:           d("1"),
		ReferenceType: domain.RefPurchase,
	}
	_, err := uc.Execute(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for missing TenantID, got nil")
	}
}

// countingLockRepo delegates all methods to inner, but increments count on AcquireAdvisoryLock.
type countingLockRepo struct {
	inner *mockRepo
	count *int
}

func (c *countingLockRepo) GetSnapshot(ctx context.Context, a, b, d uuid.UUID) (*domain.Snapshot, error) {
	return c.inner.GetSnapshot(ctx, a, b, d)
}
func (c *countingLockRepo) SelectForUpdate(ctx context.Context, tx *sql.Tx, a, b, d uuid.UUID) (*domain.Snapshot, error) {
	return c.inner.SelectForUpdate(ctx, tx, a, b, d)
}
func (c *countingLockRepo) UpsertSnapshot(ctx context.Context, tx *sql.Tx, s *domain.Snapshot) error {
	return c.inner.UpsertSnapshot(ctx, tx, s)
}
func (c *countingLockRepo) InsertMovement(ctx context.Context, tx *sql.Tx, m *domain.Movement) error {
	return c.inner.InsertMovement(ctx, tx, m)
}
func (c *countingLockRepo) ListMovements(ctx context.Context, f appstock.MovementFilter) ([]domain.Movement, error) {
	return c.inner.ListMovements(ctx, f)
}
func (c *countingLockRepo) InsertLot(ctx context.Context, tx *sql.Tx, l *domain.Lot) error {
	return c.inner.InsertLot(ctx, tx, l)
}
func (c *countingLockRepo) ListActiveLots(ctx context.Context, tx *sql.Tx, a, b, d uuid.UUID) ([]domain.Lot, error) {
	return c.inner.ListActiveLots(ctx, tx, a, b, d)
}
func (c *countingLockRepo) UpdateLotQty(ctx context.Context, tx *sql.Tx, lotID uuid.UUID, qty decimal.Decimal) error {
	return c.inner.UpdateLotQty(ctx, tx, lotID, qty)
}
func (c *countingLockRepo) AcquireAdvisoryLock(ctx context.Context, tx *sql.Tx, a, b, d uuid.UUID) error {
	*c.count++
	return nil
}
func (c *countingLockRepo) WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	return c.inner.WithTx(ctx, fn)
}
func (c *countingLockRepo) ListSnapshots(ctx context.Context, f appstock.ListSnapshotsFilter) ([]domain.Snapshot, error) {
	return c.inner.ListSnapshots(ctx, f)
}
