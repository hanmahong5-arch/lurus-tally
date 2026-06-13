//go:build integration

// Replenish learning real-SQL tests (W1.F1/F2): learned lead time median over
// approved purchase bills (≥12h samples only) and CNY-converted last purchase
// price, exercised against a real PostgreSQL schema. Run with:
//
//	go test -v -tags integration -timeout 180s ./tests/integration/ -run TestSQLRealReplenishLearning
package integration

import (
	"context"
	"database/sql"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/shopspring/decimal"

	replenishrepo "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/replenish"
	appreplenish "github.com/hanmahong5-arch/lurus-tally/internal/app/replenish"
)

// insertApprovedPurchaseBill inserts an approved (status=2) purchase bill with
// explicit created_at/approved_at — required because the lead-time learner
// samples approved_at − created_at and created_at otherwise defaults to now().
func insertApprovedPurchaseBill(t *testing.T, db *sql.DB, ctx context.Context,
	tenantID, partnerID uuid.UUID,
	billDate, createdAt, approvedAt time.Time, exchangeRate float64,
) uuid.UUID {
	t.Helper()
	id := uuid.New()
	creatorID := uuid.New()
	billNo := "TL-" + id.String()[:8]
	_, err := db.ExecContext(ctx, `
		INSERT INTO tally.bill_head
		    (id, tenant_id, bill_no, bill_type, sub_type, status, partner_id,
		     creator_id, bill_date, created_at, approved_at, exchange_rate)
		VALUES ($1, $2, $3, '入库', '采购', 2, $4, $5, $6, $7, $8, $9)
	`, id, tenantID, billNo, partnerID, creatorID, billDate, createdAt, approvedAt, exchangeRate)
	if err != nil {
		t.Fatalf("insertApprovedPurchaseBill: %v", err)
	}
	return id
}

func TestSQLRealReplenishLearning(t *testing.T) {
	db, cleanup := sqlRealDB(t)
	defer cleanup()

	ctx := context.Background()

	tenantID := insertTenant(t, db, ctx)
	productID := insertProduct(t, db, ctx, tenantID, "Learn Product", "LEARN-A")
	supplierA := insertPartner(t, db, ctx, tenantID, "Learn Supplier A", "supplier")
	supplierB := insertPartner(t, db, ctx, tenantID, "Learn Supplier B", "supplier")

	now := time.Now().UTC()

	// Bill 1 (supplier A): created 30d ago, approved 25d ago → 5.0-day sample.
	bill1 := insertApprovedPurchaseBill(t, db, ctx, tenantID, supplierA,
		now.Add(-30*24*time.Hour), now.Add(-30*24*time.Hour), now.Add(-25*24*time.Hour), 1)
	insertBillItem(t, db, ctx, tenantID, bill1, productID, 10, 80.0, 80.0)

	// Bill 2 (supplier A): created 10d ago, approved 6d ago → 4.0-day sample.
	// Latest bill_date → drives last_price; exchange_rate 7.2 (USD purchase),
	// so last price must come back CNY-converted: 100 × 7.2 = 720.
	bill2 := insertApprovedPurchaseBill(t, db, ctx, tenantID, supplierA,
		now.Add(-10*24*time.Hour), now.Add(-10*24*time.Hour), now.Add(-6*24*time.Hour), 7.2)
	insertBillItem(t, db, ctx, tenantID, bill2, productID, 10, 100.0, 100.0)

	// Bill 3 (supplier A): created≈approved (1h apart, <12h) — a back-filled
	// bill that must be EXCLUDED from lead-time learning. Old bill_date keeps
	// it out of the last_price pick too.
	bill3 := insertApprovedPurchaseBill(t, db, ctx, tenantID, supplierA,
		now.Add(-40*24*time.Hour), now.Add(-2*24*time.Hour), now.Add(-2*24*time.Hour).Add(time.Hour), 1)
	insertBillItem(t, db, ctx, tenantID, bill3, productID, 10, 999.0, 999.0)

	// Bill 4 (supplier B): older bill_date, price 50 — exists so the batch
	// price lookup can prove same-supplier preference beats recency.
	bill4 := insertApprovedPurchaseBill(t, db, ctx, tenantID, supplierB,
		now.Add(-60*24*time.Hour), now.Add(-60*24*time.Hour), now.Add(-55*24*time.Hour), 1)
	insertBillItem(t, db, ctx, tenantID, bill4, productID, 10, 50.0, 50.0)

	repo := replenishrepo.NewSQLSuggestionRepo(db)

	// ── learned lead time + CNY last price via ListSuggestions ──────────────
	t.Run("ListSuggestions_learned_median_and_cny_last_price", func(t *testing.T) {
		rows, err := repo.ListSuggestions(ctx, tenantID)
		if err != nil {
			t.Fatalf("FAIL ListSuggestions: %v", err)
		}
		var found *appreplenish.RawRow
		for i := range rows {
			if rows[i].ProductID == productID {
				found = &rows[i]
				break
			}
		}
		if found == nil {
			t.Fatal("FAIL: learn product not in ListSuggestions output")
		}
		// Bills 1+2+4 qualify (≥12h); bill 3 is excluded → 3 samples, median 5.0d
		// over {5.0, 4.0, 5.0}... bill 4 is supplier B but learning is per-product:
		// samples = {5.0 (b1), 4.0 (b2), 5.0 (b4)} → count 3, median 5.0.
		if found.LeadTimeSamples != 3 {
			t.Errorf("FAIL: LeadTimeSamples = %d, want 3 (bill 3 <12h must be excluded)", found.LeadTimeSamples)
		}
		if math.Abs(found.LearnedLeadDays-5.0) > 0.1 {
			t.Errorf("FAIL: LearnedLeadDays = %f, want ~5.0 (median of 5,4,5)", found.LearnedLeadDays)
		}
		if found.LastPurchasePrice == nil {
			t.Fatal("FAIL: LastPurchasePrice is nil, want 720 (100 × rate 7.2)")
		}
		if !found.LastPurchasePrice.Equal(decimal.NewFromInt(720)) {
			t.Errorf("FAIL: LastPurchasePrice = %s, want 720 (CNY-converted)", found.LastPurchasePrice)
		}
		t.Logf("PASS: samples=%d median=%.2f lastPrice=%s",
			found.LeadTimeSamples, found.LearnedLeadDays, found.LastPurchasePrice)
	})

	// ── batch price lookup: same-supplier preference + any-supplier fallback ─
	t.Run("LastPurchasePrices_same_supplier_preferred", func(t *testing.T) {
		prices, err := repo.LastPurchasePrices(ctx, tenantID, []appreplenish.ProductSupplier{
			{ProductID: productID, SupplierID: &supplierB},
		})
		if err != nil {
			t.Fatalf("FAIL LastPurchasePrices: %v", err)
		}
		got, ok := prices[productID]
		if !ok {
			t.Fatal("FAIL: no price returned for product")
		}
		// Supplier B's own (older) bill must win over supplier A's newer one.
		if !got.Equal(decimal.NewFromInt(50)) {
			t.Errorf("FAIL: price = %s, want 50 (same-supplier preference)", got)
		}
	})

	t.Run("LastPurchasePrices_nil_supplier_falls_back_to_latest", func(t *testing.T) {
		prices, err := repo.LastPurchasePrices(ctx, tenantID, []appreplenish.ProductSupplier{
			{ProductID: productID, SupplierID: nil},
		})
		if err != nil {
			t.Fatalf("FAIL LastPurchasePrices: %v", err)
		}
		got, ok := prices[productID]
		if !ok {
			t.Fatal("FAIL: no price returned for product")
		}
		// No preferred supplier → latest bill_date wins (bill 2, CNY 720).
		if !got.Equal(decimal.NewFromInt(720)) {
			t.Errorf("FAIL: price = %s, want 720 (latest any-supplier)", got)
		}
	})

	// ── exclusion regression: ONLY same-day bills → no learning at all ──────
	t.Run("ListSuggestions_backfilled_only_product_not_learned", func(t *testing.T) {
		backfilled := insertProduct(t, db, ctx, tenantID, "Backfill Product", "LEARN-B")
		for i := 0; i < 3; i++ {
			created := now.Add(-time.Duration(i+1) * 24 * time.Hour)
			b := insertApprovedPurchaseBill(t, db, ctx, tenantID, supplierA,
				created, created, created.Add(2*time.Hour), 1) // 2h < 12h → excluded
			insertBillItem(t, db, ctx, tenantID, b, backfilled, 5, 10.0, 10.0)
		}
		rows, err := repo.ListSuggestions(ctx, tenantID)
		if err != nil {
			t.Fatalf("FAIL ListSuggestions: %v", err)
		}
		for _, r := range rows {
			if r.ProductID == backfilled {
				if r.LeadTimeSamples != 0 {
					t.Errorf("FAIL: LeadTimeSamples = %d, want 0 (all samples <12h)", r.LeadTimeSamples)
				}
				return
			}
		}
		t.Fatal("FAIL: backfill product not in ListSuggestions output")
	})
}
