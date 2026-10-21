package reports

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	repobill "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/bill"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dbscope"
	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	apprep "github.com/hanmahong5-arch/lurus-tally/internal/app/report"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/decimalutil"
)

var _ apprep.Repo = (*SQLRepo)(nil)

// Approved bills dated in [$2, $3), summed per sub type. The four sub types are the only
// ones that move money or goods with a partner; transfers and stocktakes are not sales.
const monthlyBillsQ = `
	SELECT sub_type, COALESCE(SUM(total_amount), 0)::text
	FROM tally.bill_head
	WHERE tenant_id = $1 AND deleted_at IS NULL AND status = 2
	  AND bill_date >= $2 AND bill_date < $3
	  AND sub_type IN ('销售', '销售退货', '采购', '采购退货')
	GROUP BY sub_type`

// Cost of goods sold, cut by the month of the bill the movement belongs to (not the day it
// was approved), so it lines up with the net sales of the same window. A sale out-movement
// points at its bill, or at one of the bill's lines; either resolves to the bill head.
// Movements whose bill is gone, deleted or not approved are not counted. A sale reversed
// through a sale-reference inflow and a sales return (reference 'return' on a 销售退货 bill)
// both reduce it; a purchase return is stock leaving, not a sale cost.
const monthlyCostQ = `
	SELECT COALESCE(SUM(CASE
	         WHEN m.reference_type = 'sale' AND m.direction = 'out' THEN m.total_cost
	         WHEN m.reference_type = 'sale' AND m.direction = 'in' THEN -m.total_cost
	         WHEN m.reference_type = 'return' AND m.direction = 'in' AND bh.sub_type = '销售退货' THEN -m.total_cost
	         ELSE 0 END), 0)::text
	FROM tally.stock_movement m
	JOIN tally.bill_head bh ON bh.tenant_id = m.tenant_id AND bh.id = COALESCE(
	       (SELECT bi.head_id FROM tally.bill_item bi WHERE bi.id = m.reference_id AND bi.tenant_id = m.tenant_id),
	       m.reference_id)
	WHERE m.tenant_id = $1 AND m.reference_type IN ('sale', 'return')
	  AND bh.status = 2 AND bh.deleted_at IS NULL
	  AND bh.bill_date >= $2 AND bh.bill_date < $3`

// Money actually moved in the month, per the bill it belongs to. Refunds are stored as
// negative payments on the original bill, so they net off by plain summing. "credit" is a
// promise to pay later (or a purchase-return offset), never cash, so it is left out. A payment
// hung on a return bill flows the other way (cash back from the supplier on a purchase
// return, cash handed back to the customer on a sales return), so its sign is flipped. Each payment is rounded to the cent before summing, the
// same figure the payments CSV shows per row, so the rows add up to the card exactly.
const monthlyPaymentsQ = `
	SELECT bh.sub_type, COALESCE(SUM(ROUND(CASE WHEN bh.sub_type IN ('销售退货', '采购退货') THEN -ph.amount ELSE ph.amount END, 2)), 0)::text
	FROM tally.payment_head ph
	JOIN tally.bill_head bh ON bh.id = ph.related_bill_id AND bh.tenant_id = ph.tenant_id
	WHERE ph.tenant_id = $1 AND ph.deleted_at IS NULL AND ph.pay_type <> 'credit'
	  AND ph.pay_date >= $2 AND ph.pay_date < $3
	  AND bh.sub_type IN ('销售', '销售退货', '采购', '采购退货')
	GROUP BY bh.sub_type`

const monthlyInventoryQ = `
	SELECT COALESCE(SUM(on_hand_qty * unit_cost), 0)::text
	FROM tally.stock_snapshot
	WHERE tenant_id = $1`

// MonthlyFigures implements apprep.Repo for the half-open window [from, to). Receivable and
// payable come from the ageing read model as of the month's last day, so the report and the
// ageing page cannot disagree.
func (r *SQLRepo) MonthlyFigures(ctx context.Context, tenantID uuid.UUID, from, to time.Time) (apprep.Figures, error) {
	var f apprep.Figures
	dbh := dbscope.From(ctx, r.db)

	sums := func(q string) (map[string]decimal.Decimal, error) {
		rows, err := dbh.QueryContext(ctx, q, tenantID, from, to)
		if err != nil {
			return nil, err
		}
		defer func() { _ = rows.Close() }()
		out := map[string]decimal.Decimal{}
		for rows.Next() {
			var k, v string
			if err := rows.Scan(&k, &v); err != nil {
				return nil, err
			}
			d, err := decimalutil.Parse(v, "monthly sum")
			if err != nil {
				return nil, err
			}
			out[k] = d
		}
		return out, rows.Err()
	}

	bills, err := sums(monthlyBillsQ)
	if err != nil {
		return f, fmt.Errorf("monthly report: bills: %w", err)
	}
	f.Sales, f.SaleReturns = bills["销售"], bills["销售退货"]
	f.Purchases, f.PurchaseReturns = bills["采购"], bills["采购退货"]

	pays, err := sums(monthlyPaymentsQ)
	if err != nil {
		return f, fmt.Errorf("monthly report: payments: %w", err)
	}
	f.Receipts = pays["销售"].Add(pays["销售退货"])
	f.Payments = pays["采购"].Add(pays["采购退货"])

	var cost string
	if err := dbh.QueryRowContext(ctx, monthlyCostQ, tenantID, from, to).Scan(&cost); err != nil {
		return f, fmt.Errorf("monthly report: cost of goods: %w", err)
	}
	if f.CostOfGoods, err = decimalutil.Parse(cost, "cost of goods"); err != nil {
		return f, err
	}

	var inv string
	if err := dbh.QueryRowContext(ctx, monthlyInventoryQ, tenantID).Scan(&inv); err != nil {
		return f, fmt.Errorf("monthly report: inventory: %w", err)
	}
	if f.InventoryValue, err = decimalutil.Parse(inv, "inventory value"); err != nil {
		return f, err
	}

	asOf := to.AddDate(0, 0, -1)
	billRepo := repobill.New(r.db)
	for _, d := range []struct {
		dir string
		dst *decimal.Decimal
	}{{appbill.ReceivableAR, &f.Receivable}, {appbill.ReceivableAP, &f.Payable}} {
		rows, err := billRepo.PartnerReceivables(ctx, appbill.ReceivablesQuery{TenantID: tenantID, Direction: d.dir, AsOf: asOf})
		if err != nil {
			return f, fmt.Errorf("monthly report: %w", err)
		}
		for _, row := range rows {
			*d.dst = d.dst.Add(row.Unpaid)
		}
	}
	return f, nil
}
