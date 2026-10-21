package bill

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dbscope"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/decimalutil"
)

// SetLinkBill points a return bill at the sale it gives back (bill_head.link_bill_id)
// within tx. The sale must belong to the same tenant: the column's FK check bypasses RLS,
// so the tenant filter in the UPDATE is what keeps a foreign sale id out.
func (r *Repo) SetLinkBill(ctx context.Context, tx *sql.Tx, tenantID, billID, linkedBillID uuid.UUID) error {
	const q = `
		UPDATE tally.bill_head SET link_bill_id = $3
		WHERE id = $1 AND tenant_id = $2
		  AND EXISTS (SELECT 1 FROM tally.bill_head s WHERE s.id = $3 AND s.tenant_id = $2)`
	res, err := tx.ExecContext(ctx, q, billID, tenantID, linkedBillID)
	if err != nil {
		return fmt.Errorf("bill repo: set link bill: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("bill repo: set link bill: bill or linked sale not found in tenant")
	}
	return nil
}

// returnedAmountQuery totals the approved sales returns against $2 for tenant $1.
const returnedAmountQuery = `
		SELECT COALESCE(SUM(h.total_amount), 0)::text
		FROM tally.bill_head h
		WHERE h.tenant_id = $1 AND h.sub_type = '销售退货' AND h.status = 2 AND h.deleted_at IS NULL
		  AND (
		        h.link_bill_id = $2
		     OR h.remark LIKE 'cancel:%:original_bill_id=' || $2::text
		     OR EXISTS (
		          SELECT 1 FROM tally.import_order_seen s
		          WHERE s.tenant_id = h.tenant_id AND s.bill_id = $2
		            AND starts_with(h.remark, 'refund:' || s.platform || ':' || s.platform_order_no || ':refund_id=')
		        )
		  )`

// ReturnedAmount totals the value of the approved sales returns against saleBillID,
// counted with the same rule as ReturnedQty (linked, cancellation remark or platform refund).
func (r *Repo) ReturnedAmount(ctx context.Context, tenantID, saleBillID uuid.UUID) (decimal.Decimal, error) {
	var sum string
	if err := dbscope.From(ctx, r.db).QueryRowContext(ctx, returnedAmountQuery, tenantID, saleBillID).Scan(&sum); err != nil {
		return decimal.Zero, fmt.Errorf("bill repo: returned amount: %w", err)
	}
	return decimalutil.Parse(sum, "returned_amount")
}

// ReturnedAmountTx is ReturnedAmount read on the caller's transaction, so a payment or refund
// that holds the sale's lock sees exactly the returns committed before it.
func (r *Repo) ReturnedAmountTx(ctx context.Context, tx *sql.Tx, tenantID, saleBillID uuid.UUID) (decimal.Decimal, error) {
	var sum string
	if err := tx.QueryRowContext(ctx, returnedAmountQuery, tenantID, saleBillID).Scan(&sum); err != nil {
		return decimal.Zero, fmt.Errorf("bill repo: returned amount (tx): %w", err)
	}
	return decimalutil.Parse(sum, "returned_amount")
}

// SetLinkItems points return lines at the sale lines they give back
// (bill_item.link_item_id) within tx. links maps return line id to sale line id. Each sale
// line must belong to saleBillID in the same tenant, which the UPDATE enforces itself
// because the column's FK check bypasses RLS.
func (r *Repo) SetLinkItems(ctx context.Context, tx *sql.Tx, tenantID, saleBillID uuid.UUID, links map[uuid.UUID]uuid.UUID) error {
	const q = `
		UPDATE tally.bill_item SET link_item_id = $3
		WHERE id = $1 AND tenant_id = $2
		  AND EXISTS (SELECT 1 FROM tally.bill_item s WHERE s.id = $3 AND s.tenant_id = $2 AND s.head_id = $4)`
	for itemID, saleItemID := range links {
		res, err := tx.ExecContext(ctx, q, itemID, tenantID, saleItemID, saleBillID)
		if err != nil {
			return fmt.Errorf("bill repo: set link item: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("bill repo: set link item: line or linked sale line not found in tenant")
		}
	}
	return nil
}

// ReturnLinks maps each line of billID that points at a sale line to that sale line.
func (r *Repo) ReturnLinks(ctx context.Context, tenantID, billID uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	const q = `
		SELECT id, link_item_id FROM tally.bill_item
		WHERE head_id = $1 AND tenant_id = $2 AND link_item_id IS NOT NULL`
	rows, err := dbscope.From(ctx, r.db).QueryContext(ctx, q, billID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("bill repo: return links: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[uuid.UUID]uuid.UUID{}
	for rows.Next() {
		var id, link uuid.UUID
		if err := rows.Scan(&id, &link); err != nil {
			return nil, fmt.Errorf("bill repo: return links: scan: %w", err)
		}
		out[id] = link
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("bill repo: return links: %w", err)
	}
	return out, nil
}

// ReturnedQtyByLine totals, per sale line and in the line's own unit, what approved
// returns linked to saleBillID already gave back through a line pointer.
func (r *Repo) ReturnedQtyByLine(ctx context.Context, tenantID, saleBillID uuid.UUID) (map[uuid.UUID]decimal.Decimal, error) {
	const q = `
		SELECT i.link_item_id, SUM(i.qty)::text
		FROM tally.bill_head h
		JOIN tally.bill_item i ON i.head_id = h.id AND i.tenant_id = h.tenant_id
		WHERE h.tenant_id = $1 AND h.sub_type = '销售退货' AND h.status = 2 AND h.deleted_at IS NULL
		  AND h.link_bill_id = $2 AND i.link_item_id IS NOT NULL
		GROUP BY i.link_item_id`
	rows, err := dbscope.From(ctx, r.db).QueryContext(ctx, q, tenantID, saleBillID)
	if err != nil {
		return nil, fmt.Errorf("bill repo: returned qty by line: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[uuid.UUID]decimal.Decimal{}
	for rows.Next() {
		var id uuid.UUID
		var qty string
		if err := rows.Scan(&id, &qty); err != nil {
			return nil, fmt.Errorf("bill repo: returned qty by line: scan: %w", err)
		}
		if out[id], err = decimalutil.Parse(qty, "qty"); err != nil {
			return nil, fmt.Errorf("bill repo: returned qty by line: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("bill repo: returned qty by line: %w", err)
	}
	return out, nil
}
