//go:build integration

// Low-stock alert real-SQL test: the alert is "products at or below their
// auto-computed reorder point", derived from the same learned velocity + lead
// time the replenishment engine uses (zero-config). Exercised end-to-end against
// a real PostgreSQL schema. Run with:
//
//	go test -v -tags integration -timeout 180s ./tests/integration/ -run TestSQLRealLowStockAlert
package integration

import (
	"context"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/shopspring/decimal"

	replenishrepo "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/replenish"
	appreplenish "github.com/hanmahong5-arch/lurus-tally/internal/app/replenish"
)

func TestSQLRealLowStockAlert(t *testing.T) {
	db, cleanup := sqlRealDB(t)
	defer cleanup()

	ctx := context.Background()

	tenantID := insertTenant(t, db, ctx)
	warehouseID := insertWarehouse(t, db, ctx, tenantID)

	// Product A: low stock with real sales velocity.
	//   available = 5; sales = 60 units over the trailing 30 days → avgDaily = 2.
	//   ROP = 2×7 + safety(≈2.62) ≈ 16.62 > 5  → ALERTS.
	//   days-of-supply = 5 / 2 = 2.5.
	productA := insertProduct(t, db, ctx, tenantID, "Low Stock A", "LOW-A")
	insertStockSnapshot(t, db, ctx, tenantID, productA, warehouseID, 5, 5, 100.0)
	// Six outbound movements of 10 within the 30-day velocity window (sum 60).
	for i := 1; i <= 6; i++ {
		occurred := time.Now().Add(-time.Duration(i*2) * 24 * time.Hour)
		insertStockMovement(t, db, ctx, tenantID, productA, warehouseID, "out", 10, occurred)
	}

	// Product B: plenty of stock, no sales velocity → ROP 0 → EXCLUDED.
	productB := insertProduct(t, db, ctx, tenantID, "No Signal B", "NOSIG-B")
	insertStockSnapshot(t, db, ctx, tenantID, productB, warehouseID, 100, 100, 50.0)

	uc := appreplenish.NewListLowStockUseCase(replenishrepo.NewSQLSuggestionRepo(db))
	rows, err := uc.Execute(ctx, tenantID, 50)
	if err != nil {
		t.Fatalf("FAIL ListLowStock.Execute: %v", err)
	}

	if len(rows) != 1 {
		t.Fatalf("FAIL: expected exactly 1 alert (product A), got %d: %+v", len(rows), rows)
	}
	r := rows[0]
	if r.ProductID != productA {
		t.Fatalf("FAIL: alert product = %s, want product A (%s)", r.ProductID, productA)
	}

	avail := mustDecimal(t, r.AvailableQty, "available_qty")
	rop := mustDecimal(t, r.ReorderPoint, "reorder_point")
	if !rop.GreaterThan(avail) {
		t.Errorf("FAIL: reorder_point (%s) must exceed available (%s)", r.ReorderPoint, r.AvailableQty)
	}

	// days-of-supply ≈ 2.5 (available 5 / avgDaily 2).
	dos := mustDecimal(t, r.DaysOfSupply, "days_of_supply")
	if dos.Sub(decimal.NewFromFloat(2.5)).Abs().GreaterThan(decimal.NewFromFloat(0.05)) {
		t.Errorf("FAIL: days_of_supply = %s, want ≈2.5", r.DaysOfSupply)
	}

	// Product B (no velocity, ample stock) must be absent.
	for _, row := range rows {
		if row.ProductID == productB {
			t.Errorf("FAIL: product B (no demand signal) must not alert")
		}
	}
	t.Logf("PASS: product A alerts (avail=%s rop=%s dos=%s); product B excluded",
		r.AvailableQty, r.ReorderPoint, r.DaysOfSupply)
}

func mustDecimal(t *testing.T, s, field string) decimal.Decimal {
	t.Helper()
	v, err := decimal.NewFromString(s)
	if err != nil {
		t.Fatalf("FAIL: %s = %q is not a decimal: %v", field, s, err)
	}
	return v
}
