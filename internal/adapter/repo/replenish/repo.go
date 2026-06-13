// Package replenish provides the SQL-backed SuggestionRepo for the replenishment use case.
// It aggregates stock snapshot, 30-day sales velocity, and the most recent purchase supplier
// for each active product belonging to the tenant.
package replenish

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dbscope"
	appreplenish "github.com/hanmahong5-arch/lurus-tally/internal/app/replenish"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/decimalutil"
	"github.com/shopspring/decimal"
)

// Lead-time learning thresholds (F2). Values are interpolated into the SQL
// below via fmt.Sprintf so the query and the documented policy cannot drift.
const (
	// leadLearnMaxSamples caps how many recent approved purchase bills feed the
	// median — older history reflects suppliers/logistics that no longer apply.
	leadLearnMaxSamples = 5
	// leadLearnMinSamples is the floor below which we refuse to learn; a single
	// arrival is noise. Must match appreplenish's minLeadTimeSamples policy.
	leadLearnMinSamples = 2
	// leadLearnMinSampleHours excludes near-instant approvals: SMBs often
	// back-fill the purchase bill on goods arrival (created≈approved), and a
	// ~0-day sample would poison the learned lead time toward zero.
	leadLearnMinSampleHours = 12
)

// SQLSuggestionRepo is the PostgreSQL-backed SuggestionRepo.
type SQLSuggestionRepo struct {
	db *sql.DB
}

// NewSQLSuggestionRepo creates a new SQLSuggestionRepo.
func NewSQLSuggestionRepo(db *sql.DB) *SQLSuggestionRepo {
	return &SQLSuggestionRepo{db: db}
}

var _ appreplenish.SuggestionRepo = (*SQLSuggestionRepo)(nil)

// ListSuggestions returns one row per active product with:
//   - available_qty from tally.stock_snapshot (sum across warehouses)
//   - low_safe_qty  from tally.stock_initial (minimum across warehouses)
//   - unit_cost     from tally.stock_snapshot (avg across warehouses weighted by available_qty)
//   - avg_daily_sales computed from tally.stock_movement direction='out' last 30 days
//   - lead_time_days from tally.product (per-product; per-supplier refinement is deferred)
//   - in_transit qty: open purchase drafts/submitted orders not yet approved (status IN 0,1)
//   - supplier_id / supplier_name from the most recent approved purchase bill for the product
//     (falls back to NULL / “” when no purchase history exists)
//   - learned_lead_days / lead_time_samples: median approved_at−created_at over the
//     most recent ≤5 approved purchase bills (≥12h samples only, ≥2 required)
//   - last_purchase_price: most recent approved purchase unit price converted to CNY
var listSuggestionsQuery = fmt.Sprintf(`
WITH velocity AS (
    SELECT
        product_id,
        SUM(qty_base) / 30.0 AS avg_daily
    FROM tally.stock_movement
    WHERE tenant_id  = $1
      AND direction  = 'out'
      AND occurred_at >= now() - INTERVAL '30 days'
    GROUP BY product_id
),
stock AS (
    SELECT
        product_id,
        SUM(available_qty)                                      AS available_qty,
        CASE
            WHEN SUM(available_qty) > 0
            THEN SUM(unit_cost * available_qty) / SUM(available_qty)
            ELSE AVG(unit_cost)
        END                                                     AS unit_cost
    FROM tally.stock_snapshot
    WHERE tenant_id = $1
    GROUP BY product_id
),
safety AS (
    SELECT product_id, MIN(low_safe_qty) AS low_safe_qty
    FROM tally.stock_initial
    WHERE tenant_id = $1
    GROUP BY product_id
),
in_transit AS (
    -- Open purchase orders (draft=0 or submitted=1) not yet approved (status=2).
    -- These represent goods already ordered but not received; subtract from suggested qty.
    SELECT
        bi.product_id,
        SUM(bi.qty) AS in_transit_qty
    FROM tally.bill_item  bi
    JOIN tally.bill_head  bh ON bh.id = bi.head_id
    WHERE bh.tenant_id  = $1
      AND bh.bill_type  = '入库'
      AND bh.sub_type   = '采购'
      AND bh.status     IN (0, 1)
      AND bh.deleted_at IS NULL
      AND bi.deleted_at IS NULL
    GROUP BY bi.product_id
),
last_supplier AS (
    SELECT DISTINCT ON (bi.product_id)
        bi.product_id,
        bh.partner_id   AS supplier_id,
        p.name          AS supplier_name
    FROM tally.bill_item  bi
    JOIN tally.bill_head  bh ON bh.id = bi.head_id
    JOIN tally.partner    p  ON p.id  = bh.partner_id
    WHERE bh.tenant_id   = $1
      AND bh.bill_type   = '入库'
      AND bh.sub_type    = '采购'
      AND bh.status      = 2
      AND bh.deleted_at  IS NULL
      AND p.partner_type IN ('supplier','both')
      AND p.deleted_at   IS NULL
    ORDER BY bi.product_id, bh.bill_date DESC
),
lead_learned AS (
    -- Median actual lead time (approved_at − created_at, in days) over the
    -- most recent %[1]d approved purchase bills per product. Samples shorter
    -- than %[3]d hours are excluded: SMBs often back-fill the bill on goods
    -- arrival, and created≈approved would poison the learned median.
    SELECT
        product_id,
        percentile_cont(0.5) WITHIN GROUP (ORDER BY sample_days) AS learned_lead_days,
        COUNT(*)                                                 AS lead_time_samples
    FROM (
        SELECT
            bi.product_id,
            -- 86400 = seconds per day; EXTRACT(EPOCH ...) yields seconds.
            EXTRACT(EPOCH FROM (bh.approved_at - bh.created_at)) / 86400.0 AS sample_days,
            ROW_NUMBER() OVER (PARTITION BY bi.product_id ORDER BY bh.approved_at DESC) AS rn
        FROM tally.bill_item  bi
        JOIN tally.bill_head  bh ON bh.id = bi.head_id
        WHERE bh.tenant_id   = $1
          AND bh.bill_type   = '入库'
          AND bh.sub_type    = '采购'
          AND bh.status      = 2
          AND bh.deleted_at  IS NULL
          AND bi.deleted_at  IS NULL
          AND bh.approved_at IS NOT NULL
          AND bh.approved_at - bh.created_at >= INTERVAL '%[3]d hours'
    ) samples
    WHERE rn <= %[1]d
    GROUP BY product_id
    HAVING COUNT(*) >= %[2]d
),
last_price AS (
    -- Most recent approved purchase unit price per product, converted to CNY
    -- so multi-currency tenants get comparable estimated amounts.
    SELECT DISTINCT ON (bi.product_id)
        bi.product_id,
        bi.unit_price * COALESCE(bh.exchange_rate, 1) AS last_purchase_price
    FROM tally.bill_item  bi
    JOIN tally.bill_head  bh ON bh.id = bi.head_id
    WHERE bh.tenant_id  = $1
      AND bh.bill_type  = '入库'
      AND bh.sub_type   = '采购'
      AND bh.status     = 2
      AND bh.deleted_at IS NULL
      AND bi.deleted_at IS NULL
    ORDER BY bi.product_id, bh.bill_date DESC
)
SELECT
    pr.id                                    AS product_id,
    pr.name                                  AS product_name,
    pr.code                                  AS product_code,
    COALESCE(st.available_qty, 0)            AS available_qty,
    COALESCE(sf.low_safe_qty,  0)            AS safety_qty,
    COALESCE(st.unit_cost,     0)            AS unit_cost,
    COALESCE(vel.avg_daily,    0)            AS avg_daily_sales,
    pr.lead_time_days                        AS lead_time_days,
    COALESCE(it.in_transit_qty, 0)           AS in_transit_qty,
    ls.supplier_id,
    COALESCE(ls.supplier_name, '')           AS supplier_name,
    ll.learned_lead_days,
    COALESCE(ll.lead_time_samples, 0)        AS lead_time_samples,
    lp.last_purchase_price
FROM tally.product pr
LEFT JOIN stock       st  ON st.product_id  = pr.id
LEFT JOIN safety      sf  ON sf.product_id  = pr.id
LEFT JOIN velocity    vel ON vel.product_id = pr.id
LEFT JOIN in_transit  it  ON it.product_id  = pr.id
LEFT JOIN last_supplier ls ON ls.product_id = pr.id
LEFT JOIN lead_learned  ll ON ll.product_id = pr.id
LEFT JOIN last_price    lp ON lp.product_id = pr.id
WHERE pr.tenant_id  = $1
  AND pr.deleted_at IS NULL
  AND pr.enabled    = true
ORDER BY pr.name
LIMIT 10000
`, leadLearnMaxSamples, leadLearnMinSamples, leadLearnMinSampleHours)

// ListSuggestions implements appreplenish.SuggestionRepo.
func (r *SQLSuggestionRepo) ListSuggestions(ctx context.Context, tenantID uuid.UUID) ([]appreplenish.RawRow, error) {
	dbh := dbscope.From(ctx, r.db)
	rows, err := dbh.QueryContext(ctx, listSuggestionsQuery, tenantID)
	if err != nil {
		return nil, fmt.Errorf("replenish list suggestions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []appreplenish.RawRow
	for rows.Next() {
		var row appreplenish.RawRow
		var availStr, safetyStr, costStr, avgStr, inTransitStr string
		var supplierID sql.NullString
		var learnedDays sql.NullFloat64
		var lastPrice sql.NullString

		if err := rows.Scan(
			&row.ProductID,
			&row.ProductName,
			&row.ProductCode,
			&availStr,
			&safetyStr,
			&costStr,
			&avgStr,
			&row.LeadTimeDays,
			&inTransitStr,
			&supplierID,
			&row.SupplierName,
			&learnedDays,
			&row.LeadTimeSamples,
			&lastPrice,
		); err != nil {
			return nil, fmt.Errorf("replenish scan row: %w", err)
		}

		if row.AvailableQty, err = decimalutil.Parse(availStr, "available_qty"); err != nil {
			return nil, err
		}
		if row.SafetyQty, err = decimalutil.Parse(safetyStr, "safety_qty"); err != nil {
			return nil, err
		}
		if row.UnitCost, err = decimalutil.Parse(costStr, "unit_cost"); err != nil {
			return nil, err
		}
		if row.AvgDailySales, err = decimalutil.Parse(avgStr, "avg_daily_sales"); err != nil {
			return nil, err
		}
		if row.InTransit, err = decimalutil.Parse(inTransitStr, "in_transit"); err != nil {
			return nil, err
		}

		if supplierID.Valid {
			id, err := uuid.Parse(supplierID.String)
			if err == nil {
				row.SupplierID = &id
			}
		}
		if learnedDays.Valid {
			row.LearnedLeadDays = learnedDays.Float64
		}
		if lastPrice.Valid {
			p, err := decimalutil.Parse(lastPrice.String, "last_purchase_price")
			if err != nil {
				return nil, err
			}
			row.LastPurchasePrice = &p
		}

		out = append(out, row)
	}
	return out, rows.Err()
}

// lastPurchasePricesQuery resolves one CNY price per product over a set of
// (product, supplier) pairs in a single round-trip. DISTINCT ON ordering
// prefers a price from the SAME supplier as the pair, then falls back to the
// latest any-supplier price. Pairs arrive as two parallel text[] arrays; the
// supplier slot is ” (→ NULL) when the line has no supplier.
const lastPurchasePricesQuery = `
WITH pairs AS (
    SELECT p.product_id::uuid          AS product_id,
           NULLIF(p.supplier_id, '')::uuid AS supplier_id
    FROM unnest($2::text[], $3::text[]) AS p(product_id, supplier_id)
)
SELECT DISTINCT ON (pr.product_id)
    pr.product_id,
    bi.unit_price * COALESCE(bh.exchange_rate, 1) AS price_cny
FROM pairs pr
JOIN tally.bill_item bi ON bi.product_id = pr.product_id AND bi.deleted_at IS NULL
JOIN tally.bill_head bh ON bh.id = bi.head_id
WHERE bh.tenant_id  = $1
  AND bh.bill_type  = '入库'
  AND bh.sub_type   = '采购'
  AND bh.status     = 2
  AND bh.deleted_at IS NULL
ORDER BY pr.product_id,
         -- true (same supplier) sorts before false/NULL under DESC NULLS LAST,
         -- giving same-supplier preference with latest-any-supplier fallback.
         (bh.partner_id = pr.supplier_id) DESC NULLS LAST,
         bh.bill_date DESC
`

var _ appreplenish.PriceLookup = (*SQLSuggestionRepo)(nil)

// LastPurchasePrices implements appreplenish.PriceLookup with ONE batch query
// regardless of how many pairs are passed (no per-product loop). The returned
// map is keyed by product ID only — within a draft batch each product belongs
// to exactly one supplier group, so the product is a sufficient key. Products
// with no approved purchase history are simply absent from the map.
func (r *SQLSuggestionRepo) LastPurchasePrices(ctx context.Context, tenantID uuid.UUID, pairs []appreplenish.ProductSupplier) (map[uuid.UUID]decimal.Decimal, error) {
	out := make(map[uuid.UUID]decimal.Decimal, len(pairs))
	if len(pairs) == 0 {
		return out, nil
	}

	productIDs := make([]string, len(pairs))
	supplierIDs := make([]string, len(pairs))
	for i, p := range pairs {
		productIDs[i] = p.ProductID.String()
		if p.SupplierID != nil {
			supplierIDs[i] = p.SupplierID.String()
		} // else "" → NULL via NULLIF in SQL
	}

	dbh := dbscope.From(ctx, r.db)
	rows, err := dbh.QueryContext(ctx, lastPurchasePricesQuery, tenantID, productIDs, supplierIDs)
	if err != nil {
		return nil, fmt.Errorf("replenish last purchase prices: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var productID uuid.UUID
		var priceStr string
		if err := rows.Scan(&productID, &priceStr); err != nil {
			return nil, fmt.Errorf("replenish last purchase prices scan: %w", err)
		}
		price, err := decimalutil.Parse(priceStr, "price_cny")
		if err != nil {
			return nil, err
		}
		out[productID] = price
	}
	return out, rows.Err()
}
