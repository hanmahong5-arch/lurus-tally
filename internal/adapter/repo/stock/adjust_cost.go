package stock

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/decimalutil"
)

// Ensure Repo satisfies the stock-count price lookup at compile time.
var _ appstock.AdjustCostSource = (*Repo)(nil)

// FallbackCost implements appstock.AdjustCostSource: the price to value stock counted into an
// empty book at. The default SKU's purchase price wins; otherwise the cost the product
// carries in another warehouse. Zero means neither is on file.
func (r *Repo) FallbackCost(ctx context.Context, tx *sql.Tx, tenantID, productID, warehouseID uuid.UUID) (decimal.Decimal, error) {
	const skuQ = `
		SELECT purchase_price::text FROM tally.product_sku
		WHERE tenant_id = $1 AND product_id = $2 AND is_default AND deleted_at IS NULL AND purchase_price > 0
		LIMIT 1`
	const otherQ = `
		SELECT unit_cost::text FROM tally.stock_snapshot
		WHERE tenant_id = $1 AND product_id = $2 AND warehouse_id <> $3 AND unit_cost > 0
		ORDER BY updated_at DESC LIMIT 1`
	for _, q := range []struct {
		sql  string
		args []any
	}{{skuQ, []any{tenantID, productID}}, {otherQ, []any{tenantID, productID, warehouseID}}} {
		var s string
		err := tx.QueryRowContext(ctx, q.sql, q.args...).Scan(&s)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return decimal.Zero, fmt.Errorf("stock repo: fallback cost: %w", err)
		}
		return decimalutil.Parse(s, "unit_cost")
	}
	return decimal.Zero, nil
}
