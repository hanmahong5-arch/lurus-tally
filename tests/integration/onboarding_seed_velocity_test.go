//go:build integration

// Package integration — onboarding seed-velocity end-to-end test.
//
// WHY THIS TEST EXISTS
// --------------------
// A freshly-seeded demo tenant must land on a dashboard where the AI-native
// intelligence is ALIVE: one urgent low-stock alert, a populated replenishment
// list, and a lit Monday digest — zero config. That only works if the seed
// plants ~30 days of backdated sales so velocity (and therefore the learned
// reorder point) is non-zero. This test runs the REAL SeedDemoUseCase against a
// real Postgres container and asserts the whole picture lights up coherently,
// while the ledger stays honest (end-state on-hand == the displayed qtyOnHand).
//
// Run:
//
//	go test -tags integration -run TestOnboardingSeedVelocity ./tests/integration/ -timeout 360s -v
package integration

import (
	"context"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	productrepo "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/product"
	replenishrepo "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/replenish"
	repostock "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/stock"
	appdigest "github.com/hanmahong5-arch/lurus-tally/internal/app/digest"
	appob "github.com/hanmahong5-arch/lurus-tally/internal/app/onboarding"
	appproduct "github.com/hanmahong5-arch/lurus-tally/internal/app/product"
	appreplenish "github.com/hanmahong5-arch/lurus-tally/internal/app/replenish"
	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	domainstock "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"

	digestrepo "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/digest"
)

// seedStockAdapter implements appob.StockInitializer + appob.SalesRecorder over
// the real RecordMovementUseCase — the inline equivalent of the handler's
// stockAdapter, so this test exercises the same translation the HTTP path uses.
type seedStockAdapter struct {
	uc *appstock.RecordMovementUseCase
}

func (a *seedStockAdapter) Execute(ctx context.Context, req appob.StockInitRequest) (*domainstock.Snapshot, error) {
	return a.uc.Execute(ctx, appstock.RecordMovementRequest{
		TenantID:      req.TenantID,
		ProductID:     req.ProductID,
		WarehouseID:   req.WarehouseID,
		Direction:     domainstock.DirectionIn,
		Qty:           req.Qty,
		ConvFactor:    "1",
		UnitCost:      req.UnitCost,
		CostStrategy:  domainstock.CostStrategyWAC,
		ReferenceType: domainstock.RefInit,
		OccurredAt:    req.OccurredAt,
	})
}

func (a *seedStockAdapter) RecordSale(ctx context.Context, req appob.DemoSaleRequest) error {
	ref := uuid.New()
	_, err := a.uc.Execute(ctx, appstock.RecordMovementRequest{
		TenantID:      req.TenantID,
		ProductID:     req.ProductID,
		WarehouseID:   req.WarehouseID,
		Direction:     domainstock.DirectionOut,
		Qty:           req.Qty,
		ConvFactor:    "1",
		CostStrategy:  domainstock.CostStrategyWAC,
		ReferenceType: domainstock.RefSale,
		ReferenceID:   &ref,
		OccurredAt:    req.OccurredAt,
	})
	return err
}

func TestOnboardingSeedVelocity_RetailLightsUp(t *testing.T) {
	db, cleanup := sqlRealDB(t)
	defer cleanup()

	ctx := context.Background()

	tenantID := insertTenant(t, db, ctx)
	warehouseID := insertWarehouse(t, db, ctx, tenantID)

	// Build the seed use case exactly like lifecycle wires it.
	stockRepo := repostock.New(db)
	recordMovementUC := appstock.NewRecordMovementUseCase(
		stockRepo, appstock.NewCalculator(refInitWACProfile{}, stockRepo), nil, nil)
	adapter := &seedStockAdapter{uc: recordMovementUC}
	seed := appob.NewSeedDemoUseCase(
		appproduct.NewCreateUseCase(productrepo.New(db)), adapter, adapter)

	res, err := seed.Execute(ctx, appob.SeedInput{
		TenantID:    tenantID,
		WarehouseID: warehouseID,
		Persona:     appob.PersonaRetail,
	})
	if err != nil {
		t.Fatalf("seed retail: %v", err)
	}
	if res.ProductsCreated != 3 {
		t.Fatalf("products_created: want 3, got %d", res.ProductsCreated)
	}

	// ── End-state on-hand == displayed qtyOnHand (honest ledger) ─────────────
	wantOnHand := map[string]string{
		"DEMO-RT-001": "60",
		"DEMO-RT-002": "45",
		"DEMO-RT-003": "5",
	}
	for code, want := range wantOnHand {
		var onHand string
		err := db.QueryRowContext(ctx, `
			SELECT COALESCE(SUM(ss.on_hand_qty), 0)::text
			FROM tally.stock_snapshot ss
			JOIN tally.product p ON p.id = ss.product_id
			WHERE p.tenant_id = $1 AND p.code = $2`, tenantID, code).Scan(&onHand)
		if err != nil {
			t.Fatalf("on_hand %s: %v", code, err)
		}
		if !decEqual(onHand, want) {
			t.Errorf("%s on_hand: want %s, got %s", code, want, onHand)
		}
	}

	// ── Movement ledger: 1 backdated 'in' + K 'out', all within the window ───
	for code := range wantOnHand {
		var ins, outs int
		if err := db.QueryRowContext(ctx, `
			SELECT
				COUNT(*) FILTER (WHERE m.direction='in'),
				COUNT(*) FILTER (WHERE m.direction='out')
			FROM tally.stock_movement m
			JOIN tally.product p ON p.id = m.product_id
			WHERE p.tenant_id = $1 AND p.code = $2`, tenantID, code).Scan(&ins, &outs); err != nil {
			t.Fatalf("movement counts %s: %v", code, err)
		}
		if ins != 1 {
			t.Errorf("%s: want exactly 1 'in' (opening receipt), got %d", code, ins)
		}
		if outs < 1 {
			t.Errorf("%s: want ≥1 'out' (backdated sales), got %d", code, outs)
		}
		// Every sale is inside the 30-day velocity lookback and in the past.
		var saleOutsideWindow int
		if err := db.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM tally.stock_movement m
			JOIN tally.product p ON p.id = m.product_id
			WHERE p.tenant_id = $1 AND p.code = $2 AND m.direction='out'
			  AND (m.occurred_at <= now() - interval '30 days' OR m.occurred_at >= now())`,
			tenantID, code).Scan(&saleOutsideWindow); err != nil {
			t.Fatalf("sale window %s: %v", code, err)
		}
		if saleOutsideWindow != 0 {
			t.Errorf("%s: %d sale(s) fell outside (now-30d, now)", code, saleOutsideWindow)
		}
	}

	// ── Low-stock alert: only RT-003 (折叠雨伞) is below its reorder point ─────
	lowUC := appreplenish.NewListLowStockUseCase(replenishrepo.NewSQLSuggestionRepo(db))
	lowRows, err := lowUC.Execute(ctx, tenantID, 50)
	if err != nil {
		t.Fatalf("list low stock: %v", err)
	}
	if len(lowRows) != 1 {
		t.Fatalf("low-stock alerts: want exactly 1 (RT-003), got %d: %+v", len(lowRows), lowRows)
	}
	if lowRows[0].ProductCode != "DEMO-RT-003" {
		t.Errorf("low-stock SKU: want DEMO-RT-003, got %s", lowRows[0].ProductCode)
	}
	if !decGreater(lowRows[0].ReorderPoint, lowRows[0].AvailableQty) {
		t.Errorf("RT-003 reorder_point (%s) must exceed available (%s)",
			lowRows[0].ReorderPoint, lowRows[0].AvailableQty)
	}

	// ── Suggestions: 3 rows, each with learned velocity > 0 ──────────────────
	sugUC := appreplenish.NewListSuggestionsUseCase(replenishrepo.NewSQLSuggestionRepo(db))
	sugRows, err := sugUC.Execute(ctx, tenantID, 0)
	if err != nil {
		t.Fatalf("list suggestions: %v", err)
	}
	if len(sugRows) != 3 {
		t.Fatalf("suggestions: want 3 rows, got %d", len(sugRows))
	}
	for _, r := range sugRows {
		if !r.AvgDailySales.IsPositive() {
			t.Errorf("%s: avg_daily_sales must be > 0 (seeded velocity), got %s",
				r.ProductCode, r.AvgDailySales)
		}
	}

	// ── Monday digest: lit (count ≥ 1, amount > 0), dead-stock excludes demo ─
	digUC := appdigest.NewWeeklySummaryUseCase(
		digestrepo.New(db), replenishrepo.NewSQLSuggestionRepo(db))
	sum, err := digUC.Execute(ctx, tenantID)
	if err != nil {
		t.Fatalf("weekly summary: %v", err)
	}
	if sum.ReplenishCount < 1 {
		t.Errorf("digest replenish_count: want ≥1 (RT-003), got %d", sum.ReplenishCount)
	}
	if !sum.ReplenishAmountCNY.IsPositive() {
		t.Errorf("digest replenish_amount_cny: want > 0, got %s", sum.ReplenishAmountCNY)
	}
	if sum.DeadStockCount != 0 {
		t.Errorf("dead-stock count: want 0 (demo has recent sales), got %d", sum.DeadStockCount)
	}

	t.Logf("PASS: seed retail lit up — low-stock=1, suggestions=3, digest count=%d amount=%s",
		sum.ReplenishCount, sum.ReplenishAmountCNY)
}

// decEqual / decGreater compare two numeric strings as decimals (tolerating
// formatting differences like "5" vs "5.000000" from a ::text cast).
func decEqual(a, b string) bool {
	da, _ := decimal.NewFromString(a)
	dbv, _ := decimal.NewFromString(b)
	return da.Equal(dbv)
}

func decGreater(a, b string) bool {
	da, _ := decimal.NewFromString(a)
	dbv, _ := decimal.NewFromString(b)
	return da.GreaterThan(dbv)
}
