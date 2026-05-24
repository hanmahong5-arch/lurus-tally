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
// Revenue = qty × unit_price. Margin = (unit_price − unit_cost) / unit_price.
func (r *SQLRepo) ListRecentSaleLines(ctx context.Context, tenantID uuid.UUID, days int) ([]appreports.SaleRow, error) {
	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	const q = `
		SELECT
			bl.product_id,
			p.name,
			bl.quantity,
			bl.quantity * bl.unit_price                               AS revenue,
			CASE WHEN bl.unit_price > 0
				THEN (bl.unit_price - COALESCE(bl.unit_cost, 0)) / bl.unit_price
				ELSE 0
			END                                                        AS margin,
			b.approved_at
		FROM tally.bill_line bl
		JOIN tally.bill b ON b.id = bl.bill_id
		JOIN tally.product p ON p.id = bl.product_id
		WHERE b.tenant_id = $1
		  AND b.bill_type  = 'sale'
		  AND b.status     = 'approved'
		  AND b.approved_at >= $2
		ORDER BY b.approved_at DESC
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
	const q = `
		SELECT
			ss.product_id,
			p.name,
			p.code,
			COALESCE(ss.quantity, 0)                                  AS qty,
			COALESCE(ss.unit_cost_avg, 0)                             AS unit_cost,
			COALESCE(ss.last_movement_at, now() - interval '200 days') AS last_moved_at,
			COALESCE(avg_sales.avg_daily, 0)                          AS avg_daily_sales
		FROM tally.stock_snapshot ss
		JOIN tally.product p ON p.id = ss.product_id
		LEFT JOIN (
			SELECT product_id, SUM(quantity) / 30.0 AS avg_daily
			FROM tally.stock_movement
			WHERE tenant_id = $1
			  AND direction = 'out'
			  AND created_at >= now() - interval '30 days'
			GROUP BY product_id
		) avg_sales ON avg_sales.product_id = ss.product_id
		WHERE ss.tenant_id = $1
		  AND p.deleted_at IS NULL
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
