package bill

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dbscope"
	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/decimalutil"
)

// Ensure Repo satisfies the POS summary read port at compile time.
var _ appbill.POSSummaryRepo = (*Repo)(nil)

// posSalesTotalsQuery sums the approved sale bills dated in [$2, $3). $1 is the tenant.
// Every query below also takes $4, the signed-in person for a per-cashier shift (NULL = the
// whole shop): bills are matched on their creator, payments on whoever took the money.
const posSalesTotalsQuery = `
	SELECT COUNT(*), COALESCE(SUM(total_amount), 0)::text, COALESCE(SUM(paid_amount), 0)::text
	FROM tally.bill_head
	WHERE tenant_id = $1 AND bill_type = '出库' AND sub_type = '销售' AND status = 2
	  AND deleted_at IS NULL AND bill_date >= $2 AND bill_date < $3
	  AND ($4::uuid IS NULL OR creator_id = $4)`

// posCreditQuery sums what is still owed on those bills: per bill max(total - returns, 0)
// - paid, floored at zero, so a returned credit sale stops showing as debt. $1 tenant,
// $2/$3 the day window.
var posCreditQuery = `WITH` + saleReturnsCTE("") + `
	SELECT COALESCE(SUM(GREATEST(GREATEST(b.total_amount - COALESCE(r.amt, 0), 0) - b.paid_amount, 0)), 0)::text
	FROM tally.bill_head b
	LEFT JOIN rsum r ON r.sale_id = b.id
	WHERE b.tenant_id = $1 AND b.bill_type = '出库' AND b.sub_type = '销售' AND b.status = 2
	  AND b.deleted_at IS NULL AND b.bill_date >= $2 AND b.bill_date < $3
	  AND ($4::uuid IS NULL OR b.creator_id = $4)`

// posPayTypeQuery groups the payments of sale bills by method, largest first. It counts by
// the day the money moved (p.pay_date), not the day the bill was written: a refund paid out
// today lowers today's takings and leaves yesterday's closed shift untouched. Refunds are
// negative payments, so they net off inside each method; the count only takes the
// amount > 0 rows, so a next-day refund does not add a "sale" to the method (refunds are
// listed apart as RefundCount).
const posPayTypeQuery = `
	SELECT p.pay_type, COUNT(DISTINCT p.related_bill_id) FILTER (WHERE p.amount > 0), SUM(p.amount)::text
	FROM tally.payment_head p
	JOIN tally.bill_head b ON b.id = p.related_bill_id AND b.tenant_id = p.tenant_id
	WHERE p.tenant_id = $1 AND p.deleted_at IS NULL
	  AND b.bill_type = '出库' AND b.sub_type = '销售' AND b.status = 2
	  AND b.deleted_at IS NULL AND p.pay_date >= $2 AND p.pay_date < $3
	  AND ($4::uuid IS NULL OR p.creator_id = $4)
	GROUP BY p.pay_type
	ORDER BY SUM(p.amount) DESC, p.pay_type`

// posRefundCountQuery counts and totals the refund payments (negative payments on sale
// bills) paid out in [$2, $3), by the day the money moved, to match posPayTypeQuery.
const posRefundCountQuery = `
	SELECT COUNT(*), COALESCE(SUM(-p.amount), 0)::text
	FROM tally.payment_head p
	JOIN tally.bill_head b ON b.id = p.related_bill_id AND b.tenant_id = p.tenant_id
	WHERE p.tenant_id = $1 AND p.deleted_at IS NULL AND p.amount < 0
	  AND b.bill_type = '出库' AND b.sub_type = '销售' AND b.status = 2
	  AND b.deleted_at IS NULL AND p.pay_date >= $2 AND p.pay_date < $3
	  AND ($4::uuid IS NULL OR p.creator_id = $4)`

// posReturnsQuery counts the approved sales returns dated in [$2, $3).
const posReturnsQuery = `
	SELECT COUNT(*), COALESCE(SUM(total_amount), 0)::text
	FROM tally.bill_head
	WHERE tenant_id = $1 AND sub_type = '销售退货' AND status = 2
	  AND deleted_at IS NULL AND bill_date >= $2 AND bill_date < $3
	  AND ($4::uuid IS NULL OR creator_id = $4)`

// POSSummary implements appbill.POSSummaryRepo.
func (r *Repo) POSSummary(ctx context.Context, q appbill.POSSummaryRange) (appbill.POSSummary, error) {
	var out appbill.POSSummary
	db := dbscope.From(ctx, r.db)
	var who any // NULL = the whole shop
	if q.CreatorID != nil {
		who = *q.CreatorID
	}

	var total, paid string
	if err := db.QueryRowContext(ctx, posSalesTotalsQuery, q.TenantID, q.From, q.To, who).Scan(&out.Count, &total, &paid); err != nil {
		return out, fmt.Errorf("bill repo: pos summary totals: %w", err)
	}
	var err error
	if out.TotalAmount, err = decimalutil.Parse(total, "total_amount"); err != nil {
		return out, fmt.Errorf("bill repo: pos summary totals: %w", err)
	}
	if out.PaidAmount, err = decimalutil.Parse(paid, "paid_amount"); err != nil {
		return out, fmt.Errorf("bill repo: pos summary totals: %w", err)
	}

	var credit string
	if err := db.QueryRowContext(ctx, posCreditQuery, q.TenantID, q.From, q.To, who).Scan(&credit); err != nil {
		return out, fmt.Errorf("bill repo: pos summary credit: %w", err)
	}
	if out.CreditAmount, err = decimalutil.Parse(credit, "credit_amount"); err != nil {
		return out, fmt.Errorf("bill repo: pos summary credit: %w", err)
	}

	rows, err := db.QueryContext(ctx, posPayTypeQuery, q.TenantID, q.From, q.To, who)
	if err != nil {
		return out, fmt.Errorf("bill repo: pos summary pay types: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out.ByPayType = []appbill.POSPayTypeTotal{}
	for rows.Next() {
		var t appbill.POSPayTypeTotal
		var amt string
		if err := rows.Scan(&t.PayType, &t.Count, &amt); err != nil {
			return out, fmt.Errorf("bill repo: pos summary pay types scan: %w", err)
		}
		if t.Amount, err = decimalutil.Parse(amt, "amount"); err != nil {
			return out, fmt.Errorf("bill repo: pos summary pay types scan: %w", err)
		}
		out.ByPayType = append(out.ByPayType, t)
	}
	if err := rows.Err(); err != nil {
		return out, fmt.Errorf("bill repo: pos summary pay types rows: %w", err)
	}
	_ = rows.Close()

	var refAmt string
	if err := db.QueryRowContext(ctx, posRefundCountQuery, q.TenantID, q.From, q.To, who).Scan(&out.RefundCount, &refAmt); err != nil {
		return out, fmt.Errorf("bill repo: pos summary refunds: %w", err)
	}
	if out.RefundAmount, err = decimalutil.Parse(refAmt, "refund_amount"); err != nil {
		return out, fmt.Errorf("bill repo: pos summary refunds: %w", err)
	}

	var retAmt string
	if err := db.QueryRowContext(ctx, posReturnsQuery, q.TenantID, q.From, q.To, who).Scan(&out.ReturnCount, &retAmt); err != nil {
		return out, fmt.Errorf("bill repo: pos summary returns: %w", err)
	}
	if out.ReturnAmount, err = decimalutil.Parse(retAmt, "return_amount"); err != nil {
		return out, fmt.Errorf("bill repo: pos summary returns: %w", err)
	}
	return out, nil
}

// posCashierSalesQuery groups the approved sale bills of [$2, $3) by who wrote them. $1 is the tenant.
const posCashierSalesQuery = `
	SELECT creator_id, COUNT(*), COALESCE(SUM(total_amount), 0)::text, COALESCE(SUM(paid_amount), 0)::text
	FROM tally.bill_head
	WHERE tenant_id = $1 AND bill_type = '出库' AND sub_type = '销售' AND status = 2
	  AND deleted_at IS NULL AND bill_date >= $2 AND bill_date < $3
	GROUP BY creator_id`

// posCashierRefundsQuery groups the refund payments paid out in [$2, $3) by who handed the
// money back, on the same footing as posRefundCountQuery.
const posCashierRefundsQuery = `
	SELECT p.creator_id, COALESCE(SUM(-p.amount), 0)::text
	FROM tally.payment_head p
	JOIN tally.bill_head b ON b.id = p.related_bill_id AND b.tenant_id = p.tenant_id
	WHERE p.tenant_id = $1 AND p.deleted_at IS NULL AND p.amount < 0
	  AND b.bill_type = '出库' AND b.sub_type = '销售' AND b.status = 2
	  AND b.deleted_at IS NULL AND p.pay_date >= $2 AND p.pay_date < $3
	GROUP BY p.creator_id`

var _ appbill.POSCashierRepo = (*Repo)(nil)

// POSCashiers implements appbill.POSCashierRepo: sales by bill creator and refunds by payment
// creator, merged per person.
func (r *Repo) POSCashiers(ctx context.Context, q appbill.POSSummaryRange) ([]appbill.POSCashierTotal, error) {
	db := dbscope.From(ctx, r.db)
	byID := map[uuid.UUID]*appbill.POSCashierTotal{}
	var order []uuid.UUID
	slot := func(id uuid.NullUUID) *appbill.POSCashierTotal {
		key := id.UUID // uuid.Nil when the row has no creator
		if t, ok := byID[key]; ok {
			return t
		}
		t := &appbill.POSCashierTotal{CreatorID: key}
		byID[key] = t
		order = append(order, key)
		return t
	}

	rows, err := db.QueryContext(ctx, posCashierSalesQuery, q.TenantID, q.From, q.To)
	if err != nil {
		return nil, fmt.Errorf("bill repo: pos cashiers sales: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id uuid.NullUUID
		var cnt int64
		var total, paid string
		if err := rows.Scan(&id, &cnt, &total, &paid); err != nil {
			return nil, fmt.Errorf("bill repo: pos cashiers sales scan: %w", err)
		}
		t := slot(id)
		t.Count = cnt
		if t.TotalAmount, err = decimalutil.Parse(total, "total_amount"); err != nil {
			return nil, fmt.Errorf("bill repo: pos cashiers sales: %w", err)
		}
		if t.PaidAmount, err = decimalutil.Parse(paid, "paid_amount"); err != nil {
			return nil, fmt.Errorf("bill repo: pos cashiers sales: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("bill repo: pos cashiers sales rows: %w", err)
	}
	_ = rows.Close()

	rrows, err := db.QueryContext(ctx, posCashierRefundsQuery, q.TenantID, q.From, q.To)
	if err != nil {
		return nil, fmt.Errorf("bill repo: pos cashiers refunds: %w", err)
	}
	defer func() { _ = rrows.Close() }()
	for rrows.Next() {
		var id uuid.NullUUID
		var amt string
		if err := rrows.Scan(&id, &amt); err != nil {
			return nil, fmt.Errorf("bill repo: pos cashiers refunds scan: %w", err)
		}
		t := slot(id)
		if t.RefundAmount, err = decimalutil.Parse(amt, "refund_amount"); err != nil {
			return nil, fmt.Errorf("bill repo: pos cashiers refunds: %w", err)
		}
	}
	if err := rrows.Err(); err != nil {
		return nil, fmt.Errorf("bill repo: pos cashiers refunds rows: %w", err)
	}

	out := make([]appbill.POSCashierTotal, 0, len(order))
	for _, k := range order {
		out = append(out, *byID[k])
	}
	return out, nil
}
