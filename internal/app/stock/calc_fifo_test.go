package stock_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
)

// TestFIFO_ApplyMovement_Inbound_CreatesLot verifies AC-3 (partial):
// an inbound movement must create exactly one lot with qty_remaining == qty_base.
func TestFIFO_ApplyMovement_Inbound_CreatesLot(t *testing.T) {
	repo := newMockRepo(nil)
	calc := appstock.NewCalculator(stubProfile{"fifo"}, repo)

	m := newMovement(testTenantID, testProductID, testWarehouseID, domain.DirectionIn, d("50"), d("8"), domain.RefPurchase)
	result, err := calc.ApplyMovement(context.Background(), nil, m)
	if err != nil {
		t.Fatalf("ApplyMovement error: %v", err)
	}

	if !result.OnHandQty.Equal(d("50")) {
		t.Errorf("OnHandQty = %s, want 50", result.OnHandQty)
	}
	if len(repo.lots) != 1 {
		t.Fatalf("len(lots) = %d, want 1", len(repo.lots))
	}
	if !repo.lots[0].QtyRemaining.Equal(d("50")) {
		t.Errorf("lot[0].QtyRemaining = %s, want 50", repo.lots[0].QtyRemaining)
	}
	if !repo.lots[0].UnitCost.Equal(d("8")) {
		t.Errorf("lot[0].UnitCost = %s, want 8", repo.lots[0].UnitCost)
	}
}

// TestFIFO_ApplyMovement_Inbound_ThreeLots verifies AC-3:
// three inbound movements → three lots with correct qty_remaining values.
func TestFIFO_ApplyMovement_Inbound_ThreeLots(t *testing.T) {
	repo := newMockRepo(nil)
	calc := appstock.NewCalculator(stubProfile{"fifo"}, repo)
	ctx := context.Background()

	ins := []struct {
		qty  string
		cost string
	}{
		{"50", "8"},
		{"30", "9"},
		{"20", "10"},
	}
	for _, in := range ins {
		m := newMovement(testTenantID, testProductID, testWarehouseID, domain.DirectionIn, d(in.qty), d(in.cost), domain.RefPurchase)
		if _, err := calc.ApplyMovement(ctx, nil, m); err != nil {
			t.Fatalf("ApplyMovement in %s@%s: %v", in.qty, in.cost, err)
		}
	}

	if len(repo.lots) != 3 {
		t.Fatalf("len(lots) = %d, want 3", len(repo.lots))
	}
	expects := []string{"50", "30", "20"}
	for i, exp := range expects {
		if !repo.lots[i].QtyRemaining.Equal(d(exp)) {
			t.Errorf("lots[%d].QtyRemaining = %s, want %s", i, repo.lots[i].QtyRemaining, exp)
		}
	}
	if !repo.snapshot.OnHandQty.Equal(d("100")) {
		t.Errorf("snapshot.OnHandQty = %s, want 100", repo.snapshot.OnHandQty)
	}
}

// TestFIFO_ApplyMovement_Outbound_ConsumesOldestLotFirst verifies AC-3+AC-4:
// after 3 inbound lots (50@8, 30@9, 20@10), an outbound of 60 should:
// - consume lot1 entirely (50→0)
// - consume 10 from lot2 (30→20)
// - snapshot on_hand = 40
func TestFIFO_ApplyMovement_Outbound_ConsumesOldestLotFirst(t *testing.T) {
	repo := newMockRepo(nil)
	calc := appstock.NewCalculator(stubProfile{"fifo"}, repo)
	ctx := context.Background()

	// Build three lots.
	for _, in := range []struct{ qty, cost string }{{"50", "8"}, {"30", "9"}, {"20", "10"}} {
		m := newMovement(testTenantID, testProductID, testWarehouseID, domain.DirectionIn, d(in.qty), d(in.cost), domain.RefPurchase)
		if _, err := calc.ApplyMovement(ctx, nil, m); err != nil {
			t.Fatalf("inbound: %v", err)
		}
	}

	// Outbound 60.
	mOut := newMovement(testTenantID, testProductID, testWarehouseID, domain.DirectionOut, d("60"), d("0"), domain.RefSale)
	result, err := calc.ApplyMovement(ctx, nil, mOut)
	if err != nil {
		t.Fatalf("outbound ApplyMovement: %v", err)
	}

	if !result.OnHandQty.Equal(d("40")) {
		t.Errorf("OnHandQty = %s, want 40", result.OnHandQty)
	}

	// lot[0] should be fully consumed.
	if !repo.lots[0].QtyRemaining.IsZero() {
		t.Errorf("lots[0].QtyRemaining = %s, want 0", repo.lots[0].QtyRemaining)
	}
	// lot[1] should have 20 remaining (started at 30, consumed 10).
	if !repo.lots[1].QtyRemaining.Equal(d("20")) {
		t.Errorf("lots[1].QtyRemaining = %s, want 20", repo.lots[1].QtyRemaining)
	}
	// lot[2] untouched.
	if !repo.lots[2].QtyRemaining.Equal(d("20")) {
		t.Errorf("lots[2].QtyRemaining = %s, want 20", repo.lots[2].QtyRemaining)
	}
}

// TestFIFO_ApplyMovement_Outbound_CostedByWeightedConsumedLots verifies AC-4:
// out 60 from lots (50@8, 30@9, 20@10) → movement.unit_cost = (50*8+10*9)/60 = 8.1667 (rounded to 6dp)
func TestFIFO_ApplyMovement_Outbound_CostedByWeightedConsumedLots(t *testing.T) {
	repo := newMockRepo(nil)
	calc := appstock.NewCalculator(stubProfile{"fifo"}, repo)
	ctx := context.Background()

	for _, in := range []struct{ qty, cost string }{{"50", "8"}, {"30", "9"}, {"20", "10"}} {
		m := newMovement(testTenantID, testProductID, testWarehouseID, domain.DirectionIn, d(in.qty), d(in.cost), domain.RefPurchase)
		if _, err := calc.ApplyMovement(ctx, nil, m); err != nil {
			t.Fatalf("inbound: %v", err)
		}
	}

	mOut := newMovement(testTenantID, testProductID, testWarehouseID, domain.DirectionOut, d("60"), d("0"), domain.RefSale)
	_, err := calc.ApplyMovement(ctx, nil, mOut)
	if err != nil {
		t.Fatalf("outbound: %v", err)
	}

	// The outbound movement should be in repo.movements (last inserted).
	// Find the outbound movement.
	var outMv *domain.Movement
	for i := range repo.movements {
		if repo.movements[i].Direction == domain.DirectionOut {
			outMv = &repo.movements[i]
			break
		}
	}
	if outMv == nil {
		t.Fatal("no outbound movement recorded")
	}

	// (50*8 + 10*9) / 60 = (400 + 90) / 60 = 490/60 = 8.166667
	want := d("8.166667")
	if !outMv.UnitCost.Equal(want) {
		t.Errorf("outbound movement.UnitCost = %s, want %s", outMv.UnitCost, want)
	}

	wantTotal := d("490")
	if !outMv.TotalCost.Equal(wantTotal) {
		t.Errorf("outbound movement.TotalCost = %s, want %s", outMv.TotalCost, wantTotal)
	}
}

// TestFIFO_ValidateMovement_Oversell_Returns422 verifies AC-6 for FIFO.
func TestFIFO_ValidateMovement_Oversell_Returns422(t *testing.T) {
	snap := &domain.Snapshot{
		ID: uuid.New(), TenantID: testTenantID, ProductID: testProductID, WarehouseID: testWarehouseID,
		OnHandQty: d("50"), UnitCost: d("8"),
	}
	calc := appstock.NewCalculator(stubProfile{"fifo"}, newMockRepo(snap))

	m := newMovement(testTenantID, testProductID, testWarehouseID, domain.DirectionOut, d("100"), d("0"), domain.RefSale)
	err := calc.ValidateMovement(context.Background(), nil, m)
	if err == nil {
		t.Fatal("expected InsufficientStockError, got nil")
	}
	if !appstock.IsInsufficientStock(err) {
		t.Errorf("expected InsufficientStockError, got %T: %v", err, err)
	}
	ise := err.(*appstock.InsufficientStockError)
	if !ise.Available.Equal(d("50")) {
		t.Errorf("Available = %s, want 50", ise.Available)
	}
	// Confirm qty of oversell.
	if !ise.Requested.Equal(d("100")) {
		t.Errorf("Requested = %s, want 100", ise.Requested)
	}
}

// Compile-time check: FIFOCalculator satisfies InventoryCalculator.
var _ appstock.InventoryCalculator = (*appstock.FIFOCalculator)(nil)

// Compile-time check: WACCalculator satisfies InventoryCalculator.
var _ appstock.InventoryCalculator = (*appstock.WACCalculator)(nil)

// helper: make a snapshot with lots pre-seeded into a repo (for isolated outbound tests).
func makeRepoWithLots(lots []struct {
	qty      decimal.Decimal
	cost     decimal.Decimal
	received string // time offset hack not needed — insertion order matters
}) *mockRepo {
	repo := newMockRepo(nil)
	return repo
}
