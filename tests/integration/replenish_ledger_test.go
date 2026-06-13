//go:build integration

// Suggestion result ledger real-SQL tests (W2.F3): daily snapshot upsert
// immutability, adoption stamping idempotency, and scorecard aggregation,
// exercised against a real PostgreSQL schema. Run with:
//
//	go test -v -tags integration -timeout 180s ./tests/integration/ -run TestSQLRealReplenishLedger
package integration

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/shopspring/decimal"

	replenishrepo "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/replenish"
	appreplenish "github.com/hanmahong5-arch/lurus-tally/internal/app/replenish"
)

// snapRow builds a SnapshotRow with the qty fields that matter for the tests.
func snapRow(productID uuid.UUID, suggested, available float64) appreplenish.SnapshotRow {
	return appreplenish.SnapshotRow{
		ProductID:      productID,
		SuggestedQty:   decimal.NewFromFloat(suggested),
		AvailableQty:   decimal.NewFromFloat(available),
		AvgDailySales:  decimal.NewFromFloat(2),
		LeadTimeDays:   7,
		LeadTimeSource: appreplenish.LeadTimeSourceDefault,
		DaysOfSupply:   decimal.NewFromFloat(available / 2),
	}
}

// ledgerRow reads back the single ledger row for a product (today).
func ledgerRow(t *testing.T, db *sql.DB, ctx context.Context, tenantID, productID uuid.UUID) (count int, suggestedQty decimal.Decimal, adoptedBill *uuid.UUID) {
	t.Helper()
	var qtyStr string
	var bill sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) OVER (), suggested_qty, adopted_bill_id
		FROM tally.replenish_suggestion_log
		WHERE tenant_id = $1 AND product_id = $2 AND suggested_on = CURRENT_DATE
	`, tenantID, productID).Scan(&count, &qtyStr, &bill)
	if err != nil {
		t.Fatalf("ledgerRow: %v", err)
	}
	q, err := decimal.NewFromString(qtyStr)
	if err != nil {
		t.Fatalf("ledgerRow parse qty: %v", err)
	}
	if bill.Valid {
		id, perr := uuid.Parse(bill.String)
		if perr != nil {
			t.Fatalf("ledgerRow parse bill: %v", perr)
		}
		adoptedBill = &id
	}
	return count, q, adoptedBill
}

func TestSQLRealReplenishLedger(t *testing.T) {
	db, cleanup := sqlRealDB(t)
	defer cleanup()

	ctx := context.Background()
	tenantID := insertTenant(t, db, ctx)
	repo := replenishrepo.NewSQLSuggestionRepo(db)

	// ── same-day double upsert: 1 row, quantities refreshed ─────────────────
	t.Run("UpsertSnapshots_same_day_twice_one_row_refreshed", func(t *testing.T) {
		p := insertProduct(t, db, ctx, tenantID, "Ledger Upsert", "LED-A")

		if err := repo.UpsertSnapshots(ctx, tenantID, []appreplenish.SnapshotRow{snapRow(p, 10, 3)}); err != nil {
			t.Fatalf("FAIL first upsert: %v", err)
		}
		if err := repo.UpsertSnapshots(ctx, tenantID, []appreplenish.SnapshotRow{snapRow(p, 25, 1)}); err != nil {
			t.Fatalf("FAIL second upsert: %v", err)
		}
		count, qty, _ := ledgerRow(t, db, ctx, tenantID, p)
		if count != 1 {
			t.Errorf("FAIL: %d rows for same (product, day), want 1", count)
		}
		if !qty.Equal(decimal.NewFromInt(25)) {
			t.Errorf("FAIL: suggested_qty = %s, want 25 (refreshed)", qty)
		}
	})

	// ── adopted row immutable under later upserts ────────────────────────────
	t.Run("UpsertSnapshots_adopted_row_not_overwritten", func(t *testing.T) {
		p := insertProduct(t, db, ctx, tenantID, "Ledger Adopted", "LED-B")
		bill := insertBillHead(t, db, ctx, tenantID, "入库", "采购", 0, nil, time.Now().UTC())

		if err := repo.UpsertSnapshots(ctx, tenantID, []appreplenish.SnapshotRow{snapRow(p, 12, 5)}); err != nil {
			t.Fatalf("FAIL upsert: %v", err)
		}
		if err := repo.MarkAdopted(ctx, tenantID, []uuid.UUID{p}, bill); err != nil {
			t.Fatalf("FAIL mark adopted: %v", err)
		}
		// Later refresh attempt must be a no-op: keep the numbers acted on.
		if err := repo.UpsertSnapshots(ctx, tenantID, []appreplenish.SnapshotRow{snapRow(p, 99, 0)}); err != nil {
			t.Fatalf("FAIL post-adoption upsert: %v", err)
		}
		count, qty, adoptedBill := ledgerRow(t, db, ctx, tenantID, p)
		if count != 1 {
			t.Errorf("FAIL: %d rows, want 1", count)
		}
		if !qty.Equal(decimal.NewFromInt(12)) {
			t.Errorf("FAIL: suggested_qty = %s, want 12 (adopted row immutable)", qty)
		}
		if adoptedBill == nil || *adoptedBill != bill {
			t.Errorf("FAIL: adopted_bill_id = %v, want %s", adoptedBill, bill)
		}
	})

	// ── MarkAdopted idempotent: a retry must not re-stamp another bill ──────
	t.Run("MarkAdopted_idempotent_second_call_noop", func(t *testing.T) {
		p := insertProduct(t, db, ctx, tenantID, "Ledger Retry", "LED-C")
		bill1 := insertBillHead(t, db, ctx, tenantID, "入库", "采购", 0, nil, time.Now().UTC())
		bill2 := insertBillHead(t, db, ctx, tenantID, "入库", "采购", 0, nil, time.Now().UTC())

		if err := repo.UpsertSnapshots(ctx, tenantID, []appreplenish.SnapshotRow{snapRow(p, 8, 2)}); err != nil {
			t.Fatalf("FAIL upsert: %v", err)
		}
		if err := repo.MarkAdopted(ctx, tenantID, []uuid.UUID{p}, bill1); err != nil {
			t.Fatalf("FAIL first mark: %v", err)
		}
		// Retry with a different bill (worst-case replay): adopted_at IS NULL
		// guard means the row keeps the FIRST bill — same result twice.
		if err := repo.MarkAdopted(ctx, tenantID, []uuid.UUID{p}, bill2); err != nil {
			t.Fatalf("FAIL second mark: %v", err)
		}
		_, _, adoptedBill := ledgerRow(t, db, ctx, tenantID, p)
		if adoptedBill == nil || *adoptedBill != bill1 {
			t.Errorf("FAIL: adopted_bill_id = %v, want first bill %s (retry must be no-op)", adoptedBill, bill1)
		}
	})

	// ── scorecard counts vs direct SQL, including a stockout miss ───────────
	t.Run("Scorecard_counts_match_direct_sql_with_stockout_miss", func(t *testing.T) {
		scTenant := insertTenant(t, db, ctx) // fresh tenant: clean counts
		w := insertWarehouse(t, db, ctx, scTenant)
		pAdopted := insertProduct(t, db, ctx, scTenant, "SC Adopted", "SC-A")
		pMiss := insertProduct(t, db, ctx, scTenant, "SC Miss", "SC-B")
		pStocked := insertProduct(t, db, ctx, scTenant, "SC Stocked", "SC-C")
		bill := insertBillHead(t, db, ctx, scTenant, "入库", "采购", 0, nil, time.Now().UTC())

		if err := repo.UpsertSnapshots(ctx, scTenant, []appreplenish.SnapshotRow{
			snapRow(pAdopted, 10, 0),
			snapRow(pMiss, 6, 0),
			snapRow(pStocked, 4, 9),
		}); err != nil {
			t.Fatalf("FAIL upsert: %v", err)
		}
		if err := repo.MarkAdopted(ctx, scTenant, []uuid.UUID{pAdopted}, bill); err != nil {
			t.Fatalf("FAIL mark adopted: %v", err)
		}
		// Current stock: pMiss is stocked out (0), pStocked is fine (9).
		// pAdopted gets no snapshot row — adopted products are not miss
		// candidates regardless of stock.
		insertStockSnapshot(t, db, ctx, scTenant, pMiss, w, 0, 0, 1)
		insertStockSnapshot(t, db, ctx, scTenant, pStocked, w, 9, 9, 1)

		raw, err := repo.Scorecard(ctx, scTenant, 28)
		if err != nil {
			t.Fatalf("FAIL scorecard: %v", err)
		}

		// Independent direct-SQL cross-check of the two plain counts.
		var wantSuggestions, wantAdopted int
		if err := db.QueryRowContext(ctx, `
			SELECT COUNT(*), COUNT(*) FILTER (WHERE adopted_at IS NOT NULL)
			FROM tally.replenish_suggestion_log
			WHERE tenant_id = $1 AND suggested_on >= CURRENT_DATE - 28
		`, scTenant).Scan(&wantSuggestions, &wantAdopted); err != nil {
			t.Fatalf("direct sql: %v", err)
		}

		if raw.SuggestionsCount != wantSuggestions || wantSuggestions != 3 {
			t.Errorf("FAIL: suggestions = %d (direct %d), want 3", raw.SuggestionsCount, wantSuggestions)
		}
		if raw.AdoptedCount != wantAdopted || wantAdopted != 1 {
			t.Errorf("FAIL: adopted = %d (direct %d), want 1", raw.AdoptedCount, wantAdopted)
		}
		if raw.StockoutMisses != 1 {
			t.Errorf("FAIL: stockout_misses = %d, want 1 (only pMiss: unadopted + zero stock)", raw.StockoutMisses)
		}
	})
}
