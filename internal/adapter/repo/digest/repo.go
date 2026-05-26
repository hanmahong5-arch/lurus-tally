// Package digest implements DigestRepo backed by PostgreSQL using database/sql.
// Queries use the verified real schema: stock_snapshot, stock_movement, stock_initial, product.
package digest

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	appdigest "github.com/hanmahong5-arch/lurus-tally/internal/app/digest"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/decimalutil"
)

// DB is the narrow interface the repo needs; *sql.DB satisfies it.
type DB interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// SQLDigestRepo implements appdigest.DigestRepo against PostgreSQL.
type SQLDigestRepo struct {
	db DB
}

// New constructs a SQLDigestRepo.
func New(db DB) *SQLDigestRepo {
	return &SQLDigestRepo{db: db}
}

var _ appdigest.DigestRepo = (*SQLDigestRepo)(nil)

// ListReplenishCandidates returns products where the aggregated available_qty
// is below their low_safe_qty (from stock_initial) and the 30-day average
// daily sales velocity is positive.
//
// Aggregation rationale:
//   - stock_snapshot is per-warehouse → SUM across warehouses per product.
//   - stock_initial is also per-warehouse → SUM low_safe_qty per product.
//   - avg_daily_sales = SUM(qty_base WHERE direction='out', last 30d) / 30.
//   - unit_cost: warehouse-weighted average (same formula as ai/tool_repos.go).
func (r *SQLDigestRepo) ListReplenishCandidates(ctx context.Context, tenantID uuid.UUID) ([]appdigest.ReplenishRow, error) {
	const q = `
		SELECT
			s.product_id,
			s.available_qty,
			COALESCE(si.low_safe_qty, 0)   AS low_safe_qty,
			COALESCE(av.avg_daily, 0)       AS avg_daily_sales,
			s.unit_cost
		FROM (
			SELECT
				product_id,
				SUM(available_qty) AS available_qty,
				CASE WHEN SUM(on_hand_qty) > 0
					THEN SUM(unit_cost * on_hand_qty) / SUM(on_hand_qty)
					ELSE AVG(unit_cost)
				END AS unit_cost
			FROM tally.stock_snapshot
			WHERE tenant_id = $1
			GROUP BY product_id
		) s
		JOIN tally.product p ON p.id = s.product_id AND p.deleted_at IS NULL AND p.enabled = true
		LEFT JOIN (
			SELECT product_id, SUM(low_safe_qty) AS low_safe_qty
			FROM tally.stock_initial
			WHERE tenant_id = $1
			GROUP BY product_id
		) si ON si.product_id = s.product_id
		LEFT JOIN (
			SELECT product_id, SUM(qty_base) / 30.0 AS avg_daily
			FROM tally.stock_movement
			WHERE tenant_id = $1
			  AND direction = 'out'
			  AND occurred_at >= now() - interval '30 days'
			GROUP BY product_id
		) av ON av.product_id = s.product_id
		WHERE s.available_qty < COALESCE(si.low_safe_qty, 0)
		  AND COALESCE(av.avg_daily, 0) > 0`

	rows, err := r.db.QueryContext(ctx, q, tenantID)
	if err != nil {
		return nil, fmt.Errorf("digest replenish candidates: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []appdigest.ReplenishRow
	for rows.Next() {
		var row appdigest.ReplenishRow
		var availStr, safetyStr, avgStr, costStr string
		if err := rows.Scan(&row.ProductID, &availStr, &safetyStr, &avgStr, &costStr); err != nil {
			return nil, fmt.Errorf("digest replenish scan: %w", err)
		}
		if row.AvailableQty, err = decimalutil.Parse(availStr, "available_qty"); err != nil {
			return nil, err
		}
		if row.SafetyQty, err = decimalutil.Parse(safetyStr, "safety_qty"); err != nil {
			return nil, err
		}
		if row.AvgDailySales, err = decimalutil.Parse(avgStr, "avg_daily_sales"); err != nil {
			return nil, err
		}
		if row.UnitCost, err = decimalutil.Parse(costStr, "unit_cost"); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// CountOversell returns the number of distinct products whose total
// available_qty across all warehouses is negative.
func (r *SQLDigestRepo) CountOversell(ctx context.Context, tenantID uuid.UUID) (int, error) {
	const q = `
		SELECT COUNT(*) FROM (
			SELECT product_id
			FROM tally.stock_snapshot
			WHERE tenant_id = $1
			GROUP BY product_id
			HAVING SUM(available_qty) < 0
		) sub`

	var n int
	err := r.db.QueryRowContext(ctx, q, tenantID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("digest oversell count: %w", err)
	}
	return n, nil
}

// CountDeadStock returns the number of distinct products with on_hand > 0
// and whose last stock movement occurred more than 90 days ago (or never).
func (r *SQLDigestRepo) CountDeadStock(ctx context.Context, tenantID uuid.UUID) (int, error) {
	const q = `
		SELECT COUNT(*) FROM (
			SELECT s.product_id
			FROM (
				SELECT product_id, SUM(on_hand_qty) AS on_hand_qty
				FROM tally.stock_snapshot
				WHERE tenant_id = $1
				GROUP BY product_id
			) s
			JOIN tally.product p ON p.id = s.product_id AND p.deleted_at IS NULL AND p.enabled = true
			LEFT JOIN (
				SELECT product_id, MAX(occurred_at) AS last_moved_at
				FROM tally.stock_movement
				WHERE tenant_id = $1
				GROUP BY product_id
			) lm ON lm.product_id = s.product_id
			WHERE s.on_hand_qty > 0
			  AND (lm.last_moved_at IS NULL OR lm.last_moved_at < now() - interval '90 days')
		) sub`

	var n int
	err := r.db.QueryRowContext(ctx, q, tenantID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("digest dead stock count: %w", err)
	}
	return n, nil
}
