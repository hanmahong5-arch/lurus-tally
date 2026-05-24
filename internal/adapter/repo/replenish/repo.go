// Package replenish provides the SQL-backed SuggestionRepo for the replenishment use case.
// It aggregates stock snapshot, 30-day sales velocity, and the most recent purchase supplier
// for each active product belonging to the tenant.
package replenish

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	appreplenish "github.com/hanmahong5-arch/lurus-tally/internal/app/replenish"
	"github.com/shopspring/decimal"
)

// DB is the narrow database interface the repo requires.
// *sql.DB satisfies this; tests can substitute a lighter implementation.
type DB interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// SQLSuggestionRepo is the PostgreSQL-backed SuggestionRepo.
type SQLSuggestionRepo struct {
	db DB
}

// NewSQLSuggestionRepo creates a new SQLSuggestionRepo.
func NewSQLSuggestionRepo(db DB) *SQLSuggestionRepo {
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
const listSuggestionsQuery = `
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
    COALESCE(ls.supplier_name, '')           AS supplier_name
FROM tally.product pr
LEFT JOIN stock       st  ON st.product_id  = pr.id
LEFT JOIN safety      sf  ON sf.product_id  = pr.id
LEFT JOIN velocity    vel ON vel.product_id = pr.id
LEFT JOIN in_transit  it  ON it.product_id  = pr.id
LEFT JOIN last_supplier ls ON ls.product_id = pr.id
WHERE pr.tenant_id  = $1
  AND pr.deleted_at IS NULL
  AND pr.enabled    = true
ORDER BY pr.name
`

// ListSuggestions implements appreplenish.SuggestionRepo.
func (r *SQLSuggestionRepo) ListSuggestions(ctx context.Context, tenantID uuid.UUID) ([]appreplenish.RawRow, error) {
	rows, err := r.db.QueryContext(ctx, listSuggestionsQuery, tenantID)
	if err != nil {
		return nil, fmt.Errorf("replenish list suggestions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []appreplenish.RawRow
	for rows.Next() {
		var row appreplenish.RawRow
		var availStr, safetyStr, costStr, avgStr, inTransitStr string
		var supplierID sql.NullString

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
		); err != nil {
			return nil, fmt.Errorf("replenish scan row: %w", err)
		}

		row.AvailableQty, _ = decimal.NewFromString(availStr)
		row.SafetyQty, _ = decimal.NewFromString(safetyStr)
		row.UnitCost, _ = decimal.NewFromString(costStr)
		row.AvgDailySales, _ = decimal.NewFromString(avgStr)
		row.InTransit, _ = decimal.NewFromString(inTransitStr)

		if supplierID.Valid {
			id, err := uuid.Parse(supplierID.String)
			if err == nil {
				row.SupplierID = &id
			}
		}

		out = append(out, row)
	}
	return out, rows.Err()
}
