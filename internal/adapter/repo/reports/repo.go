// Package reports provides SQL-backed repositories for the reports use case.
// Queries mirror internal/adapter/repo/ai/tool_repos.go but are isolated here
// so the reports package has no dependency on the ai package.
package reports

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	appreports "github.com/hanmahong5-arch/lurus-tally/internal/app/reports"
	"github.com/shopspring/decimal"
)

// DB is the minimal interface over *sql.DB that the reports repo needs.
// Using an interface keeps the repo testable without a live database.
type DB interface {
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
}

// SQLRepo implements appreports.Repo using PostgreSQL.
type SQLRepo struct {
	db DB
}

// New constructs a SQLRepo backed by the given DB connection.
func New(db DB) *SQLRepo {
	return &SQLRepo{db: db}
}

var _ appreports.Repo = (*SQLRepo)(nil)

// ListRecentSaleLines returns approved sale line rows within the past N days.
// Revenue = qty × unit_price. Margin = (unit_price − purchase_price) / unit_price,
// where purchase_price is the cost snapshot stored on the sale line.
// Real schema: bill_head (bill_type='出库' sub_type='销售' status=2 已审核, no
// approved_at — bill_date is the business date) + bill_item (head_id, qty,
// unit_price, purchase_price).
func (r *SQLRepo) ListRecentSaleLines(ctx context.Context, tenantID uuid.UUID, days int) ([]appreports.SaleRow, error) {
	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	const q = `
		SELECT
			bi.product_id,
			p.name,
			bi.qty,
			bi.qty * bi.unit_price                                      AS revenue,
			CASE WHEN bi.unit_price > 0
				THEN (bi.unit_price - COALESCE(bi.purchase_price, 0)) / bi.unit_price
				ELSE 0
			END                                                        AS margin,
			bh.bill_date
		FROM tally.bill_item bi
		JOIN tally.bill_head bh ON bh.id = bi.head_id
		JOIN tally.product   p  ON p.id  = bi.product_id
		WHERE bh.tenant_id = $1
		  AND bh.bill_type  = '出库'
		  AND bh.sub_type   = '销售'
		  AND bh.status     = 2
		  AND bh.deleted_at IS NULL
		  AND bi.deleted_at IS NULL
		  AND bh.bill_date >= $2
		ORDER BY bh.bill_date DESC
		LIMIT 10000`

	rows, err := r.db.QueryContext(ctx, q, tenantID, cutoff)
	if err != nil {
		return nil, fmt.Errorf("reports sale lines: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []appreports.SaleRow
	for rows.Next() {
		var s appreports.SaleRow
		var qtyStr, revStr, marginStr string
		if err := rows.Scan(&s.ProductID, &s.ProductName, &qtyStr, &revStr, &marginStr, &s.SoldAt); err != nil {
			return nil, fmt.Errorf("reports sale scan: %w", err)
		}
		s.Qty, _ = decimal.NewFromString(qtyStr)
		s.Revenue, _ = decimal.NewFromString(revStr)
		s.Margin, _ = decimal.NewFromString(marginStr)
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListStockSnapshots returns current stock rows for all products of a tenant.
// AvgDailySales is derived from the past 30 days of outbound movements.
// LeadTimeDays defaults to 7 when not configured per-product.
func (r *SQLRepo) ListStockSnapshots(ctx context.Context, tenantID uuid.UUID) ([]appreports.StockRow, error) {
	// Real schema: stock_snapshot has on_hand_qty/available_qty/unit_cost (no
	// quantity/unit_cost_avg/last_movement_at). "Last movement" is derived from
	// MAX(stock_movement.occurred_at). Snapshots are per-warehouse, so aggregate
	// to one row per product to avoid double-counting dead-stock value.
	const q = `
		SELECT
			s.product_id,
			p.name,
			p.code,
			s.on_hand_qty                                             AS qty,
			s.unit_cost                                               AS unit_cost,
			COALESCE(lm.last_moved_at, now() - interval '200 days')   AS last_moved_at,
			COALESCE(av.avg_daily, 0)                                 AS avg_daily_sales
		FROM (
			SELECT
				product_id,
				SUM(on_hand_qty) AS on_hand_qty,
				CASE WHEN SUM(on_hand_qty) > 0
					THEN SUM(unit_cost * on_hand_qty) / SUM(on_hand_qty)
					ELSE AVG(unit_cost)
				END AS unit_cost
			FROM tally.stock_snapshot
			WHERE tenant_id = $1
			GROUP BY product_id
		) s
		JOIN tally.product p ON p.id = s.product_id
		LEFT JOIN (
			SELECT product_id, MAX(occurred_at) AS last_moved_at
			FROM tally.stock_movement
			WHERE tenant_id = $1
			GROUP BY product_id
		) lm ON lm.product_id = s.product_id
		LEFT JOIN (
			SELECT product_id, SUM(qty_base) / 30.0 AS avg_daily
			FROM tally.stock_movement
			WHERE tenant_id = $1
			  AND direction = 'out'
			  AND occurred_at >= now() - interval '30 days'
			GROUP BY product_id
		) av ON av.product_id = s.product_id
		WHERE p.deleted_at IS NULL
		ORDER BY p.name`

	rows, err := r.db.QueryContext(ctx, q, tenantID)
	if err != nil {
		return nil, fmt.Errorf("reports stock snapshots: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []appreports.StockRow
	for rows.Next() {
		var s appreports.StockRow
		var qtyStr, costStr, avgStr string
		if err := rows.Scan(
			&s.ProductID, &s.ProductName, &s.ProductCode,
			&qtyStr, &costStr, &s.LastMovedAt, &avgStr,
		); err != nil {
			return nil, fmt.Errorf("reports stock scan: %w", err)
		}
		s.Qty, _ = decimal.NewFromString(qtyStr)
		s.UnitCost, _ = decimal.NewFromString(costStr)
		s.AvgDailySales, _ = decimal.NewFromString(avgStr)
		s.LeadTimeDays = 7 // default; per-product config deferred
		out = append(out, s)
	}
	return out, rows.Err()
}
