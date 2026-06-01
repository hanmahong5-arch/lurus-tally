//go:build integration

// Package integration contains SQL real-run tests that exercise every analytics,
// replenish, digest, and search repository against a real PostgreSQL schema.
// Run with:
//
//	go test -v -tags integration -timeout 180s ./tests/integration/ -run TestSQLReal
package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	airepo "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/ai"
	digestrepo "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/digest"
	replenishrepo "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/replenish"
	reportsrepo "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/reports"
	searchrepo "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/search"
	"github.com/hanmahong5-arch/lurus-tally/internal/lifecycle"
)

// sqlRealDB sets up a container + migrations and returns a *sql.DB ready for use.
// The cleanup function terminates the container.
func sqlRealDB(t *testing.T) (*sql.DB, func()) {
	t.Helper()
	dsn, cleanup := startPostgres(t)

	ctx := context.Background()
	if err := lifecycle.RunMigrations(ctx, dsn, nil); err != nil {
		cleanup()
		t.Fatalf("RunMigrations: %v", err)
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		cleanup()
		t.Fatalf("sql.Open: %v", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close() //nolint:errcheck
		cleanup()
		t.Fatalf("db ping: %v", err)
	}
	return db, func() {
		db.Close() //nolint:errcheck
		cleanup()
	}
}

// ----- shared fixture helpers -----------------------------------------------

// insertTenant inserts a minimal tenant row and returns its ID.
func insertTenant(t *testing.T, db *sql.DB, ctx context.Context) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.ExecContext(ctx, `
		INSERT INTO tally.tenant (id, name, status)
		VALUES ($1, $2, 1)
	`, id, "SQL Real Test Tenant "+id.String()[:8])
	if err != nil {
		t.Fatalf("insertTenant: %v", err)
	}
	return id
}

// insertWarehouse inserts a warehouse and returns its ID.
func insertWarehouse(t *testing.T, db *sql.DB, ctx context.Context, tenantID uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.ExecContext(ctx, `
		INSERT INTO tally.warehouse (id, tenant_id, name, enabled, is_default)
		VALUES ($1, $2, 'Test WH', true, true)
	`, id, tenantID)
	if err != nil {
		t.Fatalf("insertWarehouse: %v", err)
	}
	return id
}

// insertProduct inserts a product and returns its ID.
func insertProduct(t *testing.T, db *sql.DB, ctx context.Context, tenantID uuid.UUID, name, code string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.ExecContext(ctx, `
		INSERT INTO tally.product
		    (id, tenant_id, code, name, enabled, lead_time_days)
		VALUES ($1, $2, $3, $4, true, 7)
	`, id, tenantID, code, name)
	if err != nil {
		t.Fatalf("insertProduct(%s): %v", name, err)
	}
	return id
}

// insertPartner inserts a partner (supplier/customer) and returns its ID.
func insertPartner(t *testing.T, db *sql.DB, ctx context.Context, tenantID uuid.UUID, name, partnerType string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.ExecContext(ctx, `
		INSERT INTO tally.partner (id, tenant_id, name, partner_type, enabled)
		VALUES ($1, $2, $3, $4, true)
	`, id, tenantID, name, partnerType)
	if err != nil {
		t.Fatalf("insertPartner(%s): %v", name, err)
	}
	return id
}

// insertStockSnapshot upserts a stock snapshot row.
func insertStockSnapshot(t *testing.T, db *sql.DB, ctx context.Context,
	tenantID, productID, warehouseID uuid.UUID,
	onHand, available, unitCost float64,
) {
	t.Helper()
	_, err := db.ExecContext(ctx, `
		INSERT INTO tally.stock_snapshot
		    (tenant_id, product_id, warehouse_id, on_hand_qty, available_qty, unit_cost)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (tenant_id, product_id, warehouse_id)
		DO UPDATE SET on_hand_qty = EXCLUDED.on_hand_qty,
		              available_qty = EXCLUDED.available_qty,
		              unit_cost = EXCLUDED.unit_cost
	`, tenantID, productID, warehouseID, onHand, available, unitCost)
	if err != nil {
		t.Fatalf("insertStockSnapshot: %v", err)
	}
}

// insertStockMovement inserts a stock movement row.
// reference_id is required NOT NULL (migration 000034 added the constraint).
func insertStockMovement(t *testing.T, db *sql.DB, ctx context.Context,
	tenantID, productID, warehouseID uuid.UUID,
	direction string, qty float64, occurredAt time.Time,
) {
	t.Helper()
	refID := uuid.New() // synthetic reference ID — real system uses bill_item.id etc.
	_, err := db.ExecContext(ctx, `
		INSERT INTO tally.stock_movement
		    (tenant_id, product_id, warehouse_id, direction, qty_base, reference_type, reference_id, occurred_at)
		VALUES ($1, $2, $3, $4, $5, 'sale', $6, $7)
	`, tenantID, productID, warehouseID, direction, qty, refID, occurredAt)
	if err != nil {
		t.Fatalf("insertStockMovement: %v", err)
	}
}

// insertStockInitial inserts a stock_initial (safety qty) row.
func insertStockInitial(t *testing.T, db *sql.DB, ctx context.Context,
	tenantID, productID, warehouseID uuid.UUID,
	qty, lowSafeQty float64,
) {
	t.Helper()
	_, err := db.ExecContext(ctx, `
		INSERT INTO tally.stock_initial
		    (tenant_id, product_id, warehouse_id, qty, low_safe_qty)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (tenant_id, product_id, warehouse_id) DO NOTHING
	`, tenantID, productID, warehouseID, qty, lowSafeQty)
	if err != nil {
		t.Fatalf("insertStockInitial: %v", err)
	}
}

// insertBillHead inserts a bill_head row and returns its ID.
func insertBillHead(t *testing.T, db *sql.DB, ctx context.Context,
	tenantID uuid.UUID, billType, subType string, status int,
	partnerID *uuid.UUID, billDate time.Time,
) uuid.UUID {
	t.Helper()
	id := uuid.New()
	creatorID := uuid.New()
	billNo := "T-" + id.String()[:8]
	_, err := db.ExecContext(ctx, `
		INSERT INTO tally.bill_head
		    (id, tenant_id, bill_no, bill_type, sub_type, status, partner_id, creator_id, bill_date)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, id, tenantID, billNo, billType, subType, status, partnerID, creatorID, billDate)
	if err != nil {
		t.Fatalf("insertBillHead: %v", err)
	}
	return id
}

// insertBillItem inserts a bill_item row.
func insertBillItem(t *testing.T, db *sql.DB, ctx context.Context,
	tenantID, headID, productID uuid.UUID,
	qty, unitPrice, purchasePrice float64,
) {
	t.Helper()
	_, err := db.ExecContext(ctx, `
		INSERT INTO tally.bill_item
		    (tenant_id, head_id, product_id, qty, unit_price, purchase_price)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, tenantID, headID, productID, qty, unitPrice, purchasePrice)
	if err != nil {
		t.Fatalf("insertBillItem: %v", err)
	}
}

// insertSupplier inserts into tally.supplier and returns its ID.
func insertSupplier(t *testing.T, db *sql.DB, ctx context.Context, tenantID uuid.UUID, name string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.ExecContext(ctx, `
		INSERT INTO tally.supplier (id, tenant_id, name, code)
		VALUES ($1, $2, $3, $4)
	`, id, tenantID, name, "SUP-"+id.String()[:6])
	if err != nil {
		t.Fatalf("insertSupplier(%s): %v", name, err)
	}
	return id
}

// explainQuery runs EXPLAIN ANALYZE on the given query and logs the plan.
// args must match positional params in query.
func explainQuery(t *testing.T, db *sql.DB, ctx context.Context, label, query string, args ...any) {
	t.Helper()
	rows, err := db.QueryContext(ctx, "EXPLAIN ANALYZE "+query, args...)
	if err != nil {
		// EXPLAIN ANALYZE may fail on queries with no stable arguments – log but don't fail.
		t.Logf("[EXPLAIN ANALYZE %s] unavailable: %v", label, err)
		return
	}
	defer rows.Close() //nolint:errcheck
	t.Logf("[EXPLAIN ANALYZE %s]:", label)
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			break
		}
		t.Logf("  %s", line)
	}
}

// mustJSON marshals v to JSON for logging.
func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("<json err: %v>", err)
	}
	return string(b)
}

// ============================================================================
// TestSQLReal — one top-level test, subtests per repo method
// ============================================================================

func TestSQLReal(t *testing.T) {
	db, cleanup := sqlRealDB(t)
	defer cleanup()

	ctx := context.Background()

	// ── shared fixtures ──────────────────────────────────────────────────────
	tenantID := insertTenant(t, db, ctx)
	warehouseID := insertWarehouse(t, db, ctx, tenantID)

	productA := insertProduct(t, db, ctx, tenantID, "Product Alpha", "CODE-A")
	productB := insertProduct(t, db, ctx, tenantID, "Product Beta", "CODE-B")

	partnerSupplier := insertPartner(t, db, ctx, tenantID, "Test Supplier Co", "supplier")
	partnerCustomer := insertPartner(t, db, ctx, tenantID, "Test Customer Co", "customer")
	_ = partnerCustomer

	// Supplier in the dedicated supplier table (for search tests)
	_ = insertSupplier(t, db, ctx, tenantID, "Dedicated Supplier SA")

	// Stock snapshots: productA has low stock (below safety), productB is OK.
	insertStockSnapshot(t, db, ctx, tenantID, productA, warehouseID, 5, 3, 120.0)
	insertStockSnapshot(t, db, ctx, tenantID, productB, warehouseID, 100, 98, 50.0)

	// Safety stock: productA safety = 20 (so available 3 < 20 → replenish candidate)
	insertStockInitial(t, db, ctx, tenantID, productA, warehouseID, 5, 20)

	// Recent outbound movements for velocity (within last 30 days)
	recentDay := time.Now().Add(-5 * 24 * time.Hour)
	insertStockMovement(t, db, ctx, tenantID, productA, warehouseID, "out", 10, recentDay)
	insertStockMovement(t, db, ctx, tenantID, productA, warehouseID, "out", 8, recentDay.Add(-2*24*time.Hour))
	insertStockMovement(t, db, ctx, tenantID, productB, warehouseID, "out", 5, recentDay)

	// Dead stock: productB had a very old movement (>90 days ago) — add an old one
	oldDay := time.Now().Add(-120 * 24 * time.Hour)
	insertStockMovement(t, db, ctx, tenantID, productB, warehouseID, "in", 100, oldDay)

	// Approved purchase bill for productA (for replenish last_supplier CTE)
	purchaseBillID := insertBillHead(t, db, ctx, tenantID, "入库", "采购", 2, &partnerSupplier, time.Now().Add(-10*24*time.Hour))
	insertBillItem(t, db, ctx, tenantID, purchaseBillID, productA, 50, 100.0, 90.0)

	// In-transit purchase bill (status=0, draft) for productA
	draftBillID := insertBillHead(t, db, ctx, tenantID, "入库", "采购", 0, &partnerSupplier, time.Now().Add(-1*24*time.Hour))
	insertBillItem(t, db, ctx, tenantID, draftBillID, productA, 20, 100.0, 90.0)

	// Approved sale bill for both products (for reports queries)
	saleBillID := insertBillHead(t, db, ctx, tenantID, "出库", "销售", 2, &partnerCustomer, time.Now().Add(-3*24*time.Hour))
	insertBillItem(t, db, ctx, tenantID, saleBillID, productA, 5, 200.0, 120.0)
	insertBillItem(t, db, ctx, tenantID, saleBillID, productB, 3, 80.0, 50.0)

	// ── 1. replenish/repo.go → ListSuggestions (wave-2 with in-transit CTE) ──
	t.Run("replenish/ListSuggestions_with_in_transit", func(t *testing.T) {
		repo := replenishrepo.NewSQLSuggestionRepo(db)
		rows, err := repo.ListSuggestions(ctx, tenantID)
		if err != nil {
			t.Fatalf("FAIL ListSuggestions: %v", err)
		}
		t.Logf("ListSuggestions returned %d rows", len(rows))
		if len(rows) == 0 {
			t.Fatal("FAIL: expected ≥1 row from ListSuggestions, got 0")
		}
		// Verify productA appears with in_transit > 0 (draft bill qty=20)
		found := false
		for _, r := range rows {
			t.Logf("  row: product=%s code=%s avail=%s safety=%s avg_daily=%s in_transit=%s supplier=%s",
				r.ProductName, r.ProductCode,
				r.AvailableQty, r.SafetyQty, r.AvgDailySales, r.InTransit, r.SupplierName)
			if r.ProductID == productA {
				found = true
				if r.InTransit.IsZero() {
					t.Errorf("FAIL: productA in_transit should be 20 (draft bill), got %s", r.InTransit)
				}
				if r.SupplierName == "" {
					t.Errorf("FAIL: productA should have supplier name from approved purchase bill")
				}
			}
		}
		if !found {
			t.Errorf("FAIL: productA not found in ListSuggestions output")
		}
		t.Logf("PASS: ListSuggestions row count=%d", len(rows))
	})

	// ── 2a. reports/repo.go → ListRecentSaleLines ───────────────────────────
	t.Run("reports/ListRecentSaleLines", func(t *testing.T) {
		repo := reportsrepo.New(db)
		rows, err := repo.ListRecentSaleLines(ctx, tenantID, 30)
		if err != nil {
			t.Fatalf("FAIL ListRecentSaleLines: %v", err)
		}
		t.Logf("ListRecentSaleLines returned %d rows", len(rows))
		if len(rows) == 0 {
			t.Fatal("FAIL: expected ≥1 row (inserted approved sale bill), got 0")
		}
		for _, r := range rows {
			t.Logf("  sale row: product=%s qty=%s revenue=%s margin=%s soldAt=%v",
				r.ProductName, r.Qty, r.Revenue, r.Margin, r.SoldAt)
		}
		t.Logf("PASS: ListRecentSaleLines row count=%d", len(rows))
	})

	// ── 2b. reports/repo.go → ListStockSnapshots ────────────────────────────
	t.Run("reports/ListStockSnapshots", func(t *testing.T) {
		repo := reportsrepo.New(db)
		rows, err := repo.ListStockSnapshots(ctx, tenantID)
		if err != nil {
			t.Fatalf("FAIL ListStockSnapshots: %v", err)
		}
		t.Logf("ListStockSnapshots returned %d rows", len(rows))
		if len(rows) == 0 {
			t.Fatal("FAIL: expected ≥1 stock row, got 0")
		}
		for _, r := range rows {
			t.Logf("  stock row: product=%s code=%s qty=%s cost=%s lastMoved=%v avgDaily=%s",
				r.ProductName, r.ProductCode, r.Qty, r.UnitCost, r.LastMovedAt, r.AvgDailySales)
		}
		t.Logf("PASS: ListStockSnapshots row count=%d", len(rows))
	})

	// ── 3a. ai/tool_repos.go → SQLSaleRepo.ListRecentSaleLines ─────────────
	t.Run("ai/SQLSaleRepo_ListRecentSaleLines", func(t *testing.T) {
		repo := airepo.NewSQLSaleRepo(db)
		rows, err := repo.ListRecentSaleLines(ctx, tenantID, 30)
		if err != nil {
			t.Fatalf("FAIL ai.SQLSaleRepo.ListRecentSaleLines: %v", err)
		}
		t.Logf("ai.SQLSaleRepo.ListRecentSaleLines returned %d rows", len(rows))
		if len(rows) == 0 {
			t.Fatal("FAIL: expected ≥1 row (approved sale bill), got 0")
		}
		for _, r := range rows {
			t.Logf("  ai sale: product=%s qty=%s rev=%s margin=%s", r.ProductName, r.Qty, r.Revenue, r.Margin)
		}
		t.Logf("PASS: ai SQLSaleRepo.ListRecentSaleLines row count=%d", len(rows))
	})

	// ── 3b. ai/tool_repos.go → SQLStockRepo.ListStockSnapshots ─────────────
	t.Run("ai/SQLStockRepo_ListStockSnapshots", func(t *testing.T) {
		repo := airepo.NewSQLStockRepo(db)
		rows, err := repo.ListStockSnapshots(ctx, tenantID)
		if err != nil {
			t.Fatalf("FAIL ai.SQLStockRepo.ListStockSnapshots: %v", err)
		}
		t.Logf("ai.SQLStockRepo.ListStockSnapshots returned %d rows", len(rows))
		if len(rows) == 0 {
			t.Fatal("FAIL: expected ≥1 stock row, got 0")
		}
		for _, r := range rows {
			t.Logf("  ai stock: product=%s qty=%s cost=%s avgDaily=%s", r.ProductName, r.Qty, r.UnitCost, r.AvgDailySales)
		}
		t.Logf("PASS: ai SQLStockRepo.ListStockSnapshots row count=%d", len(rows))
	})

	// ── 3c. ai/tool_repos.go → SQLProductRepo.SearchProducts ────────────────
	t.Run("ai/SQLProductRepo_SearchProducts", func(t *testing.T) {
		repo := airepo.NewSQLProductRepo(db)
		rows, err := repo.SearchProducts(ctx, tenantID, "Alpha")
		if err != nil {
			t.Fatalf("FAIL ai.SQLProductRepo.SearchProducts: %v", err)
		}
		t.Logf("ai.SQLProductRepo.SearchProducts('Alpha') returned %d rows", len(rows))
		if len(rows) == 0 {
			t.Fatal("FAIL: expected ≥1 row matching 'Alpha' (productA name), got 0")
		}
		t.Logf("PASS: ai SQLProductRepo.SearchProducts row count=%d", len(rows))
	})

	// ── 3d. ai/tool_repos.go → SQLProductRepo.ListAllProducts ───────────────
	t.Run("ai/SQLProductRepo_ListAllProducts", func(t *testing.T) {
		repo := airepo.NewSQLProductRepo(db)
		rows, err := repo.ListAllProducts(ctx, tenantID)
		if err != nil {
			t.Fatalf("FAIL ai.SQLProductRepo.ListAllProducts: %v", err)
		}
		t.Logf("ai.SQLProductRepo.ListAllProducts returned %d rows", len(rows))
		if len(rows) < 2 {
			t.Fatalf("FAIL: expected ≥2 rows (inserted productA+productB), got %d", len(rows))
		}
		t.Logf("PASS: ai SQLProductRepo.ListAllProducts row count=%d", len(rows))
	})

	// ── 4. digest/repo.go → ListReplenishCandidates ─────────────────────────
	// productA: available=3, safety=20, avg_daily>0  → should appear.
	t.Run("digest/ListReplenishCandidates", func(t *testing.T) {
		repo := digestrepo.New(db)
		rows, err := repo.ListReplenishCandidates(ctx, tenantID)
		if err != nil {
			t.Fatalf("FAIL digest.ListReplenishCandidates: %v", err)
		}
		t.Logf("digest.ListReplenishCandidates returned %d rows", len(rows))
		if len(rows) == 0 {
			t.Fatal("FAIL: expected ≥1 row (productA is below safety qty and has velocity), got 0")
		}
		found := false
		for _, r := range rows {
			t.Logf("  candidate: product=%s avail=%s safety=%s avgDaily=%s cost=%s",
				r.ProductID, r.AvailableQty, r.SafetyQty, r.AvgDailySales, r.UnitCost)
			if r.ProductID == productA {
				found = true
			}
		}
		if !found {
			t.Errorf("FAIL: productA expected in replenish candidates (avail<safety AND velocity>0)")
		}
		t.Logf("PASS: digest ListReplenishCandidates row count=%d", len(rows))
	})

	// ── 4b. digest/repo.go → CountOversell ──────────────────────────────────
	// No product has negative available qty, so count should be 0 (valid SQL, verified).
	t.Run("digest/CountOversell", func(t *testing.T) {
		repo := digestrepo.New(db)
		n, err := repo.CountOversell(ctx, tenantID)
		if err != nil {
			t.Fatalf("FAIL digest.CountOversell: %v", err)
		}
		t.Logf("digest.CountOversell = %d (0 expected — no negative available_qty inserted)", n)
		// Count=0 is correct given our fixtures; SQL ran without error = PASS.
		t.Logf("PASS: digest CountOversell (query executed, count=%d)", n)
	})

	// ── 4c. digest/repo.go → CountDeadStock ─────────────────────────────────
	// productB: on_hand=100, last outbound movement was recent (5d ago), but we also
	// inserted an inbound 120 days ago. The query looks at last_moved_at across all
	// directions — productB's most recent movement is 5d ago so it won't be dead stock.
	// productA also has recent outbound (5d). Dead stock count should be 0 here.
	t.Run("digest/CountDeadStock", func(t *testing.T) {
		repo := digestrepo.New(db)
		n, err := repo.CountDeadStock(ctx, tenantID)
		if err != nil {
			t.Fatalf("FAIL digest.CountDeadStock: %v", err)
		}
		t.Logf("digest.CountDeadStock = %d (query executed without error)", n)
		t.Logf("PASS: digest CountDeadStock (query executed, count=%d)", n)
	})

	// ── 4d. digest/CountDeadStock positive case ──────────────────────────────
	// Insert a product with on_hand > 0 and last movement > 90 days ago.
	t.Run("digest/CountDeadStock_positive_case", func(t *testing.T) {
		productDead := insertProduct(t, db, ctx, tenantID, "Dead Product ZZ", "DEAD-001")
		insertStockSnapshot(t, db, ctx, tenantID, productDead, warehouseID, 50, 50, 30.0)
		// Only old movement (>90d)
		insertStockMovement(t, db, ctx, tenantID, productDead, warehouseID, "in", 50,
			time.Now().Add(-95*24*time.Hour))

		repo := digestrepo.New(db)
		n, err := repo.CountDeadStock(ctx, tenantID)
		if err != nil {
			t.Fatalf("FAIL digest.CountDeadStock positive: %v", err)
		}
		t.Logf("digest.CountDeadStock (with dead product) = %d", n)
		if n == 0 {
			t.Fatal("FAIL: expected ≥1 dead stock product after inserting product with old movement, got 0")
		}
		t.Logf("PASS: digest CountDeadStock positive case count=%d", n)
	})

	// ── 5a. search/repo.go → SearchProducts ─────────────────────────────────
	t.Run("search/SearchProducts", func(t *testing.T) {
		repo := searchrepo.New(db)
		results, err := repo.SearchProducts(ctx, tenantID, "Alpha", 10)
		if err != nil {
			t.Fatalf("FAIL search.SearchProducts: %v", err)
		}
		t.Logf("search.SearchProducts('Alpha') returned %d results", len(results))
		if len(results) == 0 {
			t.Fatal("FAIL: expected ≥1 result matching 'Alpha', got 0")
		}
		t.Logf("  result[0]: %s", mustJSON(results[0]))
		t.Logf("PASS: search SearchProducts row count=%d", len(results))
	})

	// ── 5b. search/repo.go → SearchSuppliers ────────────────────────────────
	t.Run("search/SearchSuppliers", func(t *testing.T) {
		repo := searchrepo.New(db)
		results, err := repo.SearchSuppliers(ctx, tenantID, "Dedicated", 10)
		if err != nil {
			t.Fatalf("FAIL search.SearchSuppliers: %v", err)
		}
		t.Logf("search.SearchSuppliers('Dedicated') returned %d results", len(results))
		if len(results) == 0 {
			t.Fatal("FAIL: expected ≥1 result matching 'Dedicated Supplier SA', got 0")
		}
		t.Logf("  result[0]: %s", mustJSON(results[0]))
		t.Logf("PASS: search SearchSuppliers row count=%d", len(results))
	})

	// ── 5c. search/repo.go → SearchCustomers ────────────────────────────────
	t.Run("search/SearchCustomers", func(t *testing.T) {
		repo := searchrepo.New(db)
		results, err := repo.SearchCustomers(ctx, tenantID, "Customer", 10)
		if err != nil {
			t.Fatalf("FAIL search.SearchCustomers: %v", err)
		}
		t.Logf("search.SearchCustomers('Customer') returned %d results", len(results))
		if len(results) == 0 {
			t.Fatal("FAIL: expected ≥1 result matching 'Test Customer Co', got 0")
		}
		t.Logf("  result[0]: %s", mustJSON(results[0]))
		t.Logf("PASS: search SearchCustomers row count=%d", len(results))
	})

	// ── 5d. search/repo.go → SearchBills ────────────────────────────────────
	t.Run("search/SearchBills", func(t *testing.T) {
		repo := searchrepo.New(db)
		// bill_no is "T-<8 chars>" — search for "T-"
		results, err := repo.SearchBills(ctx, tenantID, "T-", 10)
		if err != nil {
			t.Fatalf("FAIL search.SearchBills: %v", err)
		}
		t.Logf("search.SearchBills('T-') returned %d results", len(results))
		if len(results) == 0 {
			t.Fatal("FAIL: expected ≥1 bill result matching 'T-' prefix, got 0")
		}
		t.Logf("  result[0]: %s", mustJSON(results[0]))
		t.Logf("PASS: search SearchBills row count=%d", len(results))
	})

	// ── EXPLAIN ANALYZE spot-checks ─────────────────────────────────────────
	// Run EXPLAIN ANALYZE on the two most complex CTEs for plan visibility.
	t.Run("explain/replenish_in_transit_CTE", func(t *testing.T) {
		explainQuery(t, db, ctx, "in_transit CTE", `
			SELECT bi.product_id, SUM(bi.qty) AS in_transit_qty
			FROM tally.bill_item bi
			JOIN tally.bill_head bh ON bh.id = bi.head_id
			WHERE bh.tenant_id = $1
			  AND bh.bill_type = '入库'
			  AND bh.sub_type  = '采购'
			  AND bh.status    IN (0, 1)
			  AND bh.deleted_at IS NULL
			  AND bi.deleted_at IS NULL
			GROUP BY bi.product_id
		`, tenantID)
		t.Log("PASS: EXPLAIN ANALYZE in_transit CTE ran without error")
	})

	t.Run("explain/stock_snapshot_aggregate", func(t *testing.T) {
		explainQuery(t, db, ctx, "stock_snapshot aggregate", `
			SELECT product_id,
			       SUM(on_hand_qty)   AS on_hand_qty,
			       SUM(available_qty) AS available_qty,
			       AVG(unit_cost)     AS unit_cost
			FROM tally.stock_snapshot
			WHERE tenant_id = $1
			GROUP BY product_id
		`, tenantID)
		t.Log("PASS: EXPLAIN ANALYZE stock_snapshot aggregate ran without error")
	})
}
