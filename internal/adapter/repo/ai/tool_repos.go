package ai

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	appai "github.com/hanmahong5-arch/lurus-tally/internal/app/ai"
	"github.com/shopspring/decimal"
)

// SQLProductRepo implements appai.ProductRepo using PostgreSQL.
type SQLProductRepo struct {
	db *sql.DB
}

// NewSQLProductRepo creates a new SQL-backed ProductRepo for the AI tools.
func NewSQLProductRepo(db *sql.DB) *SQLProductRepo {
	return &SQLProductRepo{db: db}
}

var _ appai.ProductRepo = (*SQLProductRepo)(nil)

// SearchProducts returns products matching the query string (name/code/mnemonic/brand ILIKE).
func (r *SQLProductRepo) SearchProducts(ctx context.Context, tenantID uuid.UUID, query string) ([]appai.ProductRow, error) {
	const q = `
		SELECT id, name, code, COALESCE(brand,''), COALESCE(mnemonic,'')
		FROM tally.product
		WHERE tenant_id = $1
		  AND deleted_at IS NULL
		  AND enabled = true
		  AND (name ILIKE $2 OR code ILIKE $2 OR mnemonic ILIKE $2 OR brand ILIKE $2)
		ORDER BY name
		LIMIT 200`

	rows, err := r.db.QueryContext(ctx, q, tenantID, "%"+query+"%")
	if err != nil {
		return nil, fmt.Errorf("ai product search: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanProductRows(rows)
}

// ListAllProducts returns all active products for a tenant.
func (r *SQLProductRepo) ListAllProducts(ctx context.Context, tenantID uuid.UUID) ([]appai.ProductRow, error) {
	const q = `
		SELECT id, name, code, COALESCE(brand,''), COALESCE(mnemonic,'')
		FROM tally.product
		WHERE tenant_id = $1 AND deleted_at IS NULL AND enabled = true
		ORDER BY name
		LIMIT 5000`

	rows, err := r.db.QueryContext(ctx, q, tenantID)
	if err != nil {
		return nil, fmt.Errorf("ai product list: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanProductRows(rows)
}

func scanProductRows(rows *sql.Rows) ([]appai.ProductRow, error) {
	var out []appai.ProductRow
	for rows.Next() {
		var p appai.ProductRow
		if err := rows.Scan(&p.ID, &p.Name, &p.Code, &p.Brand, &p.Mnemonic); err != nil {
			return nil, fmt.Errorf("ai product scan: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SQLStockRepo implements appai.StockRepo using PostgreSQL.
type SQLStockRepo struct {
	db *sql.DB
}

// NewSQLStockRepo creates a new SQL-backed StockRepo for the AI tools.
func NewSQLStockRepo(db *sql.DB) *SQLStockRepo {
	return &SQLStockRepo{db: db}
}

var _ appai.StockRepo = (*SQLStockRepo)(nil)

// ListStockSnapshots returns current stock for all products of a tenant.
// AvgDailySales is computed from the past 30 days of sale movements.
// LeadTimeDays defaults to 7 when not configured.
func (r *SQLStockRepo) ListStockSnapshots(ctx context.Context, tenantID uuid.UUID) ([]appai.StockRow, error) {
	// Real schema (000006 + 000022): stock_snapshot has on_hand_qty/unit_cost (no
	// quantity/unit_cost_avg/last_movement_at); stock_movement uses qty_base +
	// occurred_at. Snapshots are per-warehouse → aggregate to one row per product.
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
		return nil, fmt.Errorf("ai stock snapshots: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []appai.StockRow
	for rows.Next() {
		var s appai.StockRow
		var qtyStr, costStr, avgStr string
		if err := rows.Scan(&s.ProductID, &s.ProductName, &s.ProductCode,
			&qtyStr, &costStr, &s.LastMovedAt, &avgStr); err != nil {
			return nil, fmt.Errorf("ai stock scan: %w", err)
		}
		s.Qty, _ = decimal.NewFromString(qtyStr)
		s.UnitCost, _ = decimal.NewFromString(costStr)
		s.AvgDailySales, _ = decimal.NewFromString(avgStr)
		s.LeadTimeDays = 7 // default; per-product config deferred
		out = append(out, s)
	}
	return out, rows.Err()
}

// SQLSaleRepo implements appai.SaleRepo using PostgreSQL.
type SQLSaleRepo struct {
	db *sql.DB
}

// NewSQLSaleRepo creates a new SQL-backed SaleRepo for the AI tools.
func NewSQLSaleRepo(db *sql.DB) *SQLSaleRepo {
	return &SQLSaleRepo{db: db}
}

var _ appai.SaleRepo = (*SQLSaleRepo)(nil)

// ListRecentSaleLines returns sale lines within the past N days for a tenant.
// Revenue = qty × unit_price. Margin = (unit_price - unit_cost) / unit_price.
func (r *SQLSaleRepo) ListRecentSaleLines(ctx context.Context, tenantID uuid.UUID, days int) ([]appai.SaleRow, error) {
	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	// Real schema (000007): bill_head (bill_type='出库' sub_type='销售' status=2
	// 已审核, bill_date is the business date — no approved_at) + bill_item
	// (head_id, qty, unit_price, purchase_price=cost snapshot).
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
		return nil, fmt.Errorf("ai sale lines: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []appai.SaleRow
	for rows.Next() {
		var s appai.SaleRow
		var revStr, marginStr string
		var qtyStr string
		if err := rows.Scan(&s.ProductID, &s.ProductName,
			&qtyStr, &revStr, &marginStr, &s.SoldAt); err != nil {
			return nil, fmt.Errorf("ai sale scan: %w", err)
		}
		s.Qty, _ = decimal.NewFromString(qtyStr)
		s.Revenue, _ = decimal.NewFromString(revStr)
		s.Margin, _ = decimal.NewFromString(marginStr)
		out = append(out, s)
	}
	return out, rows.Err()
}

// SQLExchangeRateRepo implements appai.ExchangeRateRepo using PostgreSQL.
type SQLExchangeRateRepo struct {
	db *sql.DB
}

// NewSQLExchangeRateRepo creates a new SQL-backed ExchangeRateRepo for the AI tools.
func NewSQLExchangeRateRepo(db *sql.DB) *SQLExchangeRateRepo {
	return &SQLExchangeRateRepo{db: db}
}

var _ appai.ExchangeRateRepo = (*SQLExchangeRateRepo)(nil)

// GetRate returns the most recent exchange rate from → to for the given tenant.
func (r *SQLExchangeRateRepo) GetRate(ctx context.Context, tenantID uuid.UUID, from, to string) (decimal.Decimal, error) {
	const q = `
		SELECT rate
		FROM tally.exchange_rate
		WHERE tenant_id = $1
		  AND from_currency = $2
		  AND to_currency = $3
		ORDER BY effective_date DESC
		LIMIT 1`

	var rateStr string
	err := r.db.QueryRowContext(ctx, q, tenantID, from, to).Scan(&rateStr)
	if err != nil {
		return decimal.Zero, fmt.Errorf("ai exchange rate %s→%s: %w", from, to, err)
	}
	rate, err := decimal.NewFromString(rateStr)
	if err != nil {
		return decimal.Zero, fmt.Errorf("ai exchange rate parse: %w", err)
	}
	return rate, nil
}
