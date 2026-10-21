package bill

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dbscope"
	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/decimalutil"
)

// return_cost.go answers the cost questions a sales return asks (see appbill's
// returnCostSource): what did the goods cost when they left, and what do they cost now.

// LinkedBillID returns bill_head.link_bill_id of billID when it is set.
func (r *Repo) LinkedBillID(ctx context.Context, tenantID, billID uuid.UUID) (uuid.UUID, bool, error) {
	const q = `SELECT link_bill_id FROM tally.bill_head WHERE id = $1 AND tenant_id = $2 AND link_bill_id IS NOT NULL`
	var id uuid.UUID
	err := dbscope.From(ctx, r.db).QueryRowContext(ctx, q, billID, tenantID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("bill repo: linked bill id: %w", err)
	}
	return id, true, nil
}

// ImportedOrderBillID resolves the sale bill an imported platform order became.
func (r *Repo) ImportedOrderBillID(ctx context.Context, tenantID uuid.UUID, platform, orderNo string) (uuid.UUID, bool, error) {
	const q = `SELECT bill_id FROM tally.import_order_seen WHERE tenant_id = $1 AND platform = $2 AND platform_order_no = $3`
	var id uuid.UUID
	err := dbscope.From(ctx, r.db).QueryRowContext(ctx, q, tenantID, platform, orderNo).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("bill repo: imported order bill id: %w", err)
	}
	return id, true, nil
}

// SaleOutUnitCost is the quantity-weighted per-base-unit cost of the stock-out movements
// the sale bill booked for productID. ok is false when the sale booked none.
func (r *Repo) SaleOutUnitCost(ctx context.Context, tenantID, saleBillID, productID uuid.UUID) (decimal.Decimal, bool, error) {
	const q = `
		SELECT SUM(total_cost)::text, SUM(qty_base)::text
		FROM tally.stock_movement
		WHERE tenant_id = $1 AND reference_type = 'sale' AND reference_id = $2
		  AND product_id = $3 AND direction = 'out'`
	var cost, qty sql.NullString
	if err := dbscope.From(ctx, r.db).QueryRowContext(ctx, q, tenantID, saleBillID, productID).Scan(&cost, &qty); err != nil {
		return decimal.Zero, false, fmt.Errorf("bill repo: sale out cost: %w", err)
	}
	if !cost.Valid || !qty.Valid {
		return decimal.Zero, false, nil
	}
	c, err := decimalutil.Parse(cost.String, "total_cost")
	if err != nil {
		return decimal.Zero, false, fmt.Errorf("bill repo: sale out cost: %w", err)
	}
	q2, err := decimalutil.Parse(qty.String, "qty_base")
	if err != nil {
		return decimal.Zero, false, fmt.Errorf("bill repo: sale out cost: %w", err)
	}
	if !q2.IsPositive() {
		return decimal.Zero, false, nil
	}
	return c.Div(q2).Round(6), true, nil
}

// SnapshotUnitCost is the current average cost of productID in warehouseID.
func (r *Repo) SnapshotUnitCost(ctx context.Context, tenantID, productID, warehouseID uuid.UUID) (decimal.Decimal, bool, error) {
	const q = `
		SELECT unit_cost::text FROM tally.stock_snapshot
		WHERE tenant_id = $1 AND product_id = $2 AND warehouse_id = $3`
	var cost string
	err := dbscope.From(ctx, r.db).QueryRowContext(ctx, q, tenantID, productID, warehouseID).Scan(&cost)
	if errors.Is(err, sql.ErrNoRows) {
		return decimal.Zero, false, nil
	}
	if err != nil {
		return decimal.Zero, false, fmt.Errorf("bill repo: snapshot unit cost: %w", err)
	}
	c, err := decimalutil.Parse(cost, "unit_cost")
	if err != nil {
		return decimal.Zero, false, fmt.Errorf("bill repo: snapshot unit cost: %w", err)
	}
	return c, true, nil
}

// ReturnedQty totals, per product and unit, what approved sales returns already gave
// back against saleBillID. A return counts when it links the sale (link_bill_id), names
// it in a cancellation remark (original_bill_id=<id>), or is a platform refund remarked
// with any order number the sale was imported under (the order name and the numeric id
// are both recorded in import_order_seen).
func (r *Repo) ReturnedQty(ctx context.Context, tenantID, saleBillID uuid.UUID) ([]appbill.ReturnedLine, error) {
	const q = `
		SELECT i.product_id, i.unit_id, SUM(i.qty)::text
		FROM tally.bill_head h
		JOIN tally.bill_item i ON i.head_id = h.id AND i.tenant_id = h.tenant_id
		WHERE h.tenant_id = $1 AND h.sub_type = '销售退货' AND h.status = 2 AND h.deleted_at IS NULL
		  AND (
		        h.link_bill_id = $2
		     OR h.remark LIKE 'cancel:%:original_bill_id=' || $2::text
		     OR EXISTS (
		          SELECT 1 FROM tally.import_order_seen s
		          WHERE s.tenant_id = h.tenant_id AND s.bill_id = $2
		            AND starts_with(h.remark, 'refund:' || s.platform || ':' || s.platform_order_no || ':refund_id=')
		        )
		  )
		GROUP BY i.product_id, i.unit_id`
	rows, err := dbscope.From(ctx, r.db).QueryContext(ctx, q, tenantID, saleBillID)
	if err != nil {
		return nil, fmt.Errorf("bill repo: returned qty: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []appbill.ReturnedLine
	for rows.Next() {
		var l appbill.ReturnedLine
		var qty string
		if err := rows.Scan(&l.ProductID, &l.UnitID, &qty); err != nil {
			return nil, fmt.Errorf("bill repo: returned qty: scan: %w", err)
		}
		if l.Qty, err = decimalutil.Parse(qty, "qty"); err != nil {
			return nil, fmt.Errorf("bill repo: returned qty: %w", err)
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("bill repo: returned qty: %w", err)
	}
	return out, nil
}
