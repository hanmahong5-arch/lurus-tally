package bill

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dbscope"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/decimalutil"
)

// PurchaseReturnedQtyByLine totals, per purchase line and in the line's own unit, what
// approved purchase returns linked to purchaseBillID already sent back to the supplier.
func (r *Repo) PurchaseReturnedQtyByLine(ctx context.Context, tenantID, purchaseBillID uuid.UUID) (map[uuid.UUID]decimal.Decimal, error) {
	const q = `
		SELECT i.link_item_id, SUM(i.qty)::text
		FROM tally.bill_head h
		JOIN tally.bill_item i ON i.head_id = h.id AND i.tenant_id = h.tenant_id
		WHERE h.tenant_id = $1 AND h.sub_type = '采购退货' AND h.status = 2 AND h.deleted_at IS NULL
		  AND h.link_bill_id = $2 AND i.link_item_id IS NOT NULL
		GROUP BY i.link_item_id`
	rows, err := dbscope.From(ctx, r.db).QueryContext(ctx, q, tenantID, purchaseBillID)
	if err != nil {
		return nil, fmt.Errorf("bill repo: purchase returned qty by line: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[uuid.UUID]decimal.Decimal{}
	for rows.Next() {
		var id uuid.UUID
		var qty string
		if err := rows.Scan(&id, &qty); err != nil {
			return nil, fmt.Errorf("bill repo: purchase returned qty by line: scan: %w", err)
		}
		if out[id], err = decimalutil.Parse(qty, "qty"); err != nil {
			return nil, fmt.Errorf("bill repo: purchase returned qty by line: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("bill repo: purchase returned qty by line: %w", err)
	}
	return out, nil
}
