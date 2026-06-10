// Suggestion result ledger (F3) — SQL adapters for tally.replenish_suggestion_log
// (migration 000050). The ledger is the trust surface behind the scorecard:
// every actionable suggestion is recorded daily, adoption is stamped when a
// suggestion becomes a PO draft, and the scorecard aggregates the window.
package replenish

import (
	"context"
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dbscope"
	appreplenish "github.com/hanmahong5-arch/lurus-tally/internal/app/replenish"
)

// adoptionLookbackDays bounds MarkAdopted to recent ledger rows: a draft
// created today adopts at most the last week of suggestions for its products,
// so stale rows from an abandoned earlier cycle are not retroactively claimed
// as wins. Interpolated via fmt.Sprintf (intervals cannot be bind parameters).
const adoptionLookbackDays = 7

var _ appreplenish.SnapshotLedger = (*SQLSuggestionRepo)(nil)
var _ appreplenish.AdoptionMarker = (*SQLSuggestionRepo)(nil)
var _ appreplenish.ScorecardRepo = (*SQLSuggestionRepo)(nil)

// upsertSnapshotsQuery inserts today's snapshot rows in ONE multi-row INSERT
// via parallel unnest arrays. The conflict arm refreshes quantities only for
// rows not yet adopted — an adopted row is immutable so the ledger keeps the
// exact numbers the user acted on.
const upsertSnapshotsQuery = `
INSERT INTO tally.replenish_suggestion_log
    (tenant_id, product_id, suggested_on, suggested_qty, available_qty,
     avg_daily_sales, lead_time_days, lead_time_source, days_of_supply)
SELECT $1, r.product_id::uuid, CURRENT_DATE, r.suggested_qty::numeric,
       r.available_qty::numeric, r.avg_daily_sales::numeric,
       r.lead_time_days::numeric, r.lead_time_source, r.days_of_supply::numeric
FROM unnest($2::text[], $3::text[], $4::text[], $5::text[], $6::text[], $7::text[], $8::text[])
     AS r(product_id, suggested_qty, available_qty, avg_daily_sales,
          lead_time_days, lead_time_source, days_of_supply)
ON CONFLICT (tenant_id, product_id, suggested_on) DO UPDATE SET
    suggested_qty    = EXCLUDED.suggested_qty,
    available_qty    = EXCLUDED.available_qty,
    avg_daily_sales  = EXCLUDED.avg_daily_sales,
    lead_time_days   = EXCLUDED.lead_time_days,
    lead_time_source = EXCLUDED.lead_time_source,
    days_of_supply   = EXCLUDED.days_of_supply
WHERE replenish_suggestion_log.adopted_at IS NULL
`

// UpsertSnapshots implements appreplenish.SnapshotLedger with one batch
// statement regardless of row count (no per-product loop).
func (r *SQLSuggestionRepo) UpsertSnapshots(ctx context.Context, tenantID uuid.UUID, rows []appreplenish.SnapshotRow) error {
	if len(rows) == 0 {
		return nil
	}

	productIDs := make([]string, len(rows))
	suggestedQtys := make([]string, len(rows))
	availableQtys := make([]string, len(rows))
	avgDailySales := make([]string, len(rows))
	leadTimeDays := make([]string, len(rows))
	leadTimeSources := make([]string, len(rows))
	daysOfSupply := make([]string, len(rows))
	for i, row := range rows {
		productIDs[i] = row.ProductID.String()
		suggestedQtys[i] = row.SuggestedQty.String()
		availableQtys[i] = row.AvailableQty.String()
		avgDailySales[i] = row.AvgDailySales.String()
		leadTimeDays[i] = strconv.Itoa(row.LeadTimeDays)
		leadTimeSources[i] = row.LeadTimeSource
		daysOfSupply[i] = row.DaysOfSupply.String()
	}

	dbh := dbscope.From(ctx, r.db)
	if _, err := dbh.ExecContext(ctx, upsertSnapshotsQuery, tenantID,
		productIDs, suggestedQtys, availableQtys, avgDailySales,
		leadTimeDays, leadTimeSources, daysOfSupply); err != nil {
		return fmt.Errorf("replenish upsert snapshots: %w", err)
	}
	return nil
}

// markAdoptedQuery stamps unadopted recent rows for the given products.
// The adopted_at IS NULL + lookback guards make a retried call (same
// Idempotency-Key replays) match zero rows — a no-op, not a re-stamp.
var markAdoptedQuery = fmt.Sprintf(`
UPDATE tally.replenish_suggestion_log
   SET adopted_at = now(), adopted_bill_id = $3
 WHERE tenant_id  = $1
   AND product_id = ANY($2::uuid[])
   AND adopted_at IS NULL
   AND created_at >= now() - INTERVAL '%d days'
`, adoptionLookbackDays)

// MarkAdopted implements appreplenish.AdoptionMarker with one batch UPDATE.
func (r *SQLSuggestionRepo) MarkAdopted(ctx context.Context, tenantID uuid.UUID, productIDs []uuid.UUID, billID uuid.UUID) error {
	if len(productIDs) == 0 {
		return nil
	}
	ids := make([]string, len(productIDs))
	for i, id := range productIDs {
		ids[i] = id.String()
	}
	dbh := dbscope.From(ctx, r.db)
	if _, err := dbh.ExecContext(ctx, markAdoptedQuery, tenantID, ids, billID); err != nil {
		return fmt.Errorf("replenish mark adopted: %w", err)
	}
	return nil
}

// scorecardQuery aggregates the window in one round-trip.
//
//   - suggestions_count counts ledger ROWS (one per product-day), not distinct
//     products: a product suggested on 5 separate days represents 5 chances to
//     act, so repeated need weighs more than a one-off blip.
//   - stockout_misses = products suggested in the window, never adopted in the
//     window, whose CURRENT total available stock is <= 0. This deliberately
//     uses the current snapshot only: historical stock replay is a follow-up
//     because stock_adjust sign ambiguity makes replay unreliable — a trust
//     feature must under-claim, not over-claim. Products with no snapshot row
//     at all are NOT counted as misses for the same reason (unknown ≠ zero).
const scorecardQuery = `
WITH win AS (
    SELECT product_id, adopted_at
    FROM tally.replenish_suggestion_log
    WHERE tenant_id = $1
      AND suggested_on >= CURRENT_DATE - $2::int
),
never_adopted AS (
    SELECT product_id
    FROM win
    GROUP BY product_id
    HAVING bool_and(adopted_at IS NULL)
),
cur AS (
    SELECT product_id, SUM(available_qty) AS available_qty
    FROM tally.stock_snapshot
    WHERE tenant_id = $1
    GROUP BY product_id
)
SELECT
    (SELECT COUNT(*) FROM win)                              AS suggestions_count,
    (SELECT COUNT(*) FROM win WHERE adopted_at IS NOT NULL) AS adopted_count,
    (SELECT COUNT(*)
       FROM never_adopted na
       JOIN cur c ON c.product_id = na.product_id
      WHERE c.available_qty <= 0)                           AS stockout_misses
`

// Scorecard implements appreplenish.ScorecardRepo.
func (r *SQLSuggestionRepo) Scorecard(ctx context.Context, tenantID uuid.UUID, windowDays int) (appreplenish.ScorecardRaw, error) {
	var raw appreplenish.ScorecardRaw
	dbh := dbscope.From(ctx, r.db)
	if err := dbh.QueryRowContext(ctx, scorecardQuery, tenantID, windowDays).Scan(
		&raw.SuggestionsCount, &raw.AdoptedCount, &raw.StockoutMisses,
	); err != nil {
		return appreplenish.ScorecardRaw{}, fmt.Errorf("replenish scorecard: %w", err)
	}
	return raw, nil
}
