//go:build integration

// Package integration — clear-demo FK-cascade regression test.
//
// WHY THIS TEST EXISTS
// --------------------
// POST /api/v1/onboarding/clear-demo hard-deletes products with remark='DEMO'.
// The repo comment claimed an ON DELETE CASCADE removed the child stock rows,
// but stock_movement.product_id is ON DELETE RESTRICT (migration 000022) and
// stock_snapshot/stock_lot reference product(id) with NO ACTION. So once a demo
// SKU had opening stock, a bare product DELETE raised SQLSTATE 23503 and
// clear-demo 500'd (UAT 2026-06-16 finding). This test seeds a DEMO product WITH
// opening stock against real Postgres and asserts clear-demo removes it cleanly.
//
// Run:
//
//	go test -tags integration -run TestClearDemo ./tests/integration/ -timeout 360s -v
package integration

import (
	"context"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/shopspring/decimal"

	repoonboarding "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/onboarding"
	repostock "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/stock"
	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	domainstock "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
)

func TestClearDemo_DeletesDemoProductsWithStock(t *testing.T) {
	db, cleanup := sqlRealDB(t)
	defer cleanup()

	ctx := context.Background()

	tenantID := insertTenant(t, db, ctx)
	warehouseID := insertWarehouse(t, db, ctx, tenantID)
	productID := insertProduct(t, db, ctx, tenantID, "Demo Clear Widget", "DEMO-CLR-1")

	// Mark the product as demo data (what seed-demo writes via remark="DEMO").
	if _, err := db.ExecContext(ctx, `UPDATE tally.product SET remark='DEMO' WHERE id=$1`, productID); err != nil {
		t.Fatalf("mark product DEMO: %v", err)
	}

	// Give it opening stock through the REAL use case. The resulting
	// stock_movement (+ stock_snapshot) rows are exactly what made the bare
	// product DELETE raise 23503.
	repo := repostock.New(db)
	uc := appstock.NewRecordMovementUseCase(repo, appstock.NewCalculator(refInitWACProfile{}, repo), nil, nil)
	if _, err := uc.Execute(ctx, appstock.RecordMovementRequest{
		TenantID:      tenantID,
		ProductID:     productID,
		WarehouseID:   warehouseID,
		Direction:     domainstock.DirectionIn,
		Qty:           decimal.NewFromInt(20),
		ConvFactor:    "1",
		UnitCost:      decimal.NewFromInt(3),
		CostStrategy:  domainstock.CostStrategyWAC,
		ReferenceType: domainstock.RefInit,
	}); err != nil {
		t.Fatalf("seed opening stock: %v", err)
	}

	// Sanity: the movement exists, so the FK RESTRICT would bite a naive delete.
	var movements int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM tally.stock_movement WHERE product_id=$1`, productID,
	).Scan(&movements); err != nil {
		t.Fatalf("count movements: %v", err)
	}
	if movements == 0 {
		t.Fatal("expected at least one stock_movement for the demo product")
	}

	// clear-demo must succeed (pre-fix: SQLSTATE 23503 from the stock_movement FK).
	if err := repoonboarding.New(db).DeleteDemoProducts(ctx, tenantID); err != nil {
		t.Fatalf("clear-demo must delete demo products that have stock, got: %v", err)
	}

	// The product and all its stock rows are gone.
	checks := []struct {
		label string
		query string
	}{
		{"product", `SELECT count(*) FROM tally.product WHERE id=$1`},
		{"stock_movement", `SELECT count(*) FROM tally.stock_movement WHERE product_id=$1`},
		{"stock_lot", `SELECT count(*) FROM tally.stock_lot WHERE product_id=$1`},
		{"stock_snapshot", `SELECT count(*) FROM tally.stock_snapshot WHERE product_id=$1`},
	}
	for _, c := range checks {
		var n int
		if err := db.QueryRowContext(ctx, c.query, productID).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", c.label, err)
		}
		if n != 0 {
			t.Errorf("%s still has %d row(s) for the demo product after clear-demo", c.label, n)
		}
	}

	t.Logf("PASS: clear-demo removed the demo product and its stock rows")
}
