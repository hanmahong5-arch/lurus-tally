package stock_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
)

// stockIn books qty@cost through the calculator.
func stockIn(t *testing.T, calc appstock.InventoryCalculator, qty, cost string) {
	t.Helper()
	m := newMovement(testTenantID, testProductID, testWarehouseID, domain.DirectionIn, d(qty), d(cost), domain.RefPurchase)
	if _, err := calc.ApplyMovement(context.Background(), nil, m); err != nil {
		t.Fatalf("in %s@%s: %v", qty, cost, err)
	}
}

func TestWAC_ExplicitCostOut_LeavesAtGivenPriceAndRevaluesRest(t *testing.T) {
	repo := newMockRepo(nil)
	calc := appstock.NewCalculator(stubProfile{"wac"}, repo)
	stockIn(t, calc, "10", "20")
	stockIn(t, calc, "10", "10") // average 15

	m := newMovement(testTenantID, testProductID, testWarehouseID, domain.DirectionOut, d("3"), d("10"), domain.RefReturn)
	m.ExplicitCost = true
	snap, err := calc.ApplyMovement(context.Background(), nil, m)
	if err != nil {
		t.Fatal(err)
	}
	last := repo.movements[len(repo.movements)-1]
	if !last.UnitCost.Equal(d("10")) || !last.TotalCost.Equal(d("30")) {
		t.Fatalf("movement cost = %s / %s, want 10 / 30", last.UnitCost, last.TotalCost)
	}
	if !snap.OnHandQty.Equal(d("17")) {
		t.Fatalf("qty = %s, want 17", snap.OnHandQty)
	}
	// 20 * 15 - 3 * 10 = 270 stays on the books.
	value := snap.OnHandQty.Mul(snap.UnitCost)
	if value.Sub(d("270")).Abs().GreaterThan(d("0.001")) {
		t.Fatalf("stock value = %s, want 270", value)
	}
}

func TestWAC_PlainOut_StillLeavesAtAverage(t *testing.T) {
	repo := newMockRepo(nil)
	calc := appstock.NewCalculator(stubProfile{"wac"}, repo)
	stockIn(t, calc, "10", "20")
	stockIn(t, calc, "10", "10")
	m := newMovement(testTenantID, testProductID, testWarehouseID, domain.DirectionOut, d("3"), d("99"), domain.RefSale)
	snap, err := calc.ApplyMovement(context.Background(), nil, m)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.UnitCost.Equal(d("15")) || !repo.movements[len(repo.movements)-1].UnitCost.Equal(d("15")) {
		t.Fatalf("plain out must keep the average, got snapshot %s", snap.UnitCost)
	}
}

func TestWAC_ExplicitCostOut_DrainingEverythingKeepsCost(t *testing.T) {
	calc := appstock.NewCalculator(stubProfile{"wac"}, newMockRepo(nil))
	stockIn(t, calc, "5", "8")
	m := newMovement(testTenantID, testProductID, testWarehouseID, domain.DirectionOut, d("5"), d("9"), domain.RefReturn)
	m.ExplicitCost = true
	snap, err := calc.ApplyMovement(context.Background(), nil, m)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.OnHandQty.IsZero() || snap.UnitCost.IsNegative() {
		t.Fatalf("snapshot = %+v", snap)
	}
}

func TestFIFO_ExplicitCostOut_ReversesMatchingLot(t *testing.T) {
	repo := newMockRepo(nil)
	calc := appstock.NewCalculator(stubProfile{"fifo"}, repo)
	stockIn(t, calc, "10", "20")
	stockIn(t, calc, "10", "10")

	m := newMovement(testTenantID, testProductID, testWarehouseID, domain.DirectionOut, d("3"), d("10"), domain.RefReturn)
	m.ExplicitCost = true
	snap, err := calc.ApplyMovement(context.Background(), nil, m)
	if err != nil {
		t.Fatal(err)
	}
	if !repo.lots[0].QtyRemaining.Equal(d("10")) || !repo.lots[1].QtyRemaining.Equal(d("7")) {
		t.Fatalf("lots = %s / %s, want 10 / 7", repo.lots[0].QtyRemaining, repo.lots[1].QtyRemaining)
	}
	last := repo.movements[len(repo.movements)-1]
	if !last.UnitCost.Equal(d("10")) || !last.TotalCost.Equal(d("30")) {
		t.Fatalf("movement cost = %s / %s", last.UnitCost, last.TotalCost)
	}
	if snap.OnHandQty.Mul(snap.UnitCost).Sub(d("270")).Abs().GreaterThan(d("0.001")) {
		t.Fatalf("stock value = %s", snap.OnHandQty.Mul(snap.UnitCost))
	}
}

func TestFIFO_ExplicitCostOut_FallsBackToOldestWhenNoLotMatches(t *testing.T) {
	repo := newMockRepo(nil)
	calc := appstock.NewCalculator(stubProfile{"fifo"}, repo)
	stockIn(t, calc, "10", "20")
	m := newMovement(testTenantID, testProductID, testWarehouseID, domain.DirectionOut, d("2"), d("7"), domain.RefReturn)
	m.ExplicitCost = true
	if _, err := calc.ApplyMovement(context.Background(), nil, m); err != nil {
		t.Fatal(err)
	}
	if !repo.lots[0].QtyRemaining.Equal(d("8")) {
		t.Fatalf("lot remaining = %s, want 8", repo.lots[0].QtyRemaining)
	}
}

func TestAdjustIn_WithCost_ValuesStock(t *testing.T) {
	for _, method := range []string{"wac", "fifo"} {
		repo := newMockRepo(nil)
		calc := appstock.NewCalculator(stubProfile{method}, repo)
		m := newMovement(testTenantID, testProductID, testWarehouseID, domain.DirectionAdjust, d("5"), d("8.3"), domain.RefAdjust)
		snap, err := calc.ApplyMovement(context.Background(), nil, m)
		if err != nil {
			t.Fatal(err)
		}
		if !snap.UnitCost.Equal(d("8.3")) || !repo.movements[0].TotalCost.Equal(d("41.5")) {
			t.Fatalf("%s: cost = %s total = %s", method, snap.UnitCost, repo.movements[0].TotalCost)
		}
		if method == "fifo" && (len(repo.lots) != 1 || !repo.lots[0].UnitCost.Equal(d("8.3"))) {
			t.Fatalf("fifo must layer the counted stock, lots = %+v", repo.lots)
		}
	}
}

func TestAdjustIn_WithoutCost_KeepsAverage(t *testing.T) {
	snap0 := &domain.Snapshot{ID: uuid.New(), TenantID: testTenantID, ProductID: testProductID, WarehouseID: testWarehouseID,
		OnHandQty: d("10"), UnitCost: d("5")}
	calc := appstock.NewCalculator(stubProfile{"wac"}, newMockRepo(snap0))
	m := newMovement(testTenantID, testProductID, testWarehouseID, domain.DirectionAdjust, d("2"), d("0"), domain.RefAdjust)
	snap, err := calc.ApplyMovement(context.Background(), nil, m)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.UnitCost.Equal(d("5")) {
		t.Fatalf("cost = %s, want 5", snap.UnitCost)
	}
}
