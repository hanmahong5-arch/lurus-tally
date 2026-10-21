package bill

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dbscope"
	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/decimalutil"
)

// Ensure Repo satisfies the receivables read port at compile time.
var _ appbill.ReceivablesRepo = (*Repo)(nil)

// uuidTailRe matches a strict 8-4-4-4-12 UUID at the end of a cancellation remark, so a
// malformed tail never reaches the ::uuid cast.
const uuidTailRe = `[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`

// saleReturnsCTE resolves, per approved sales return, the sale it gives back, using
// the same three pointers as ReturnedAmount (link, cancellation remark, platform refund),
// and sums them per sale. A return counts against exactly one sale (link wins).
// $1 is the tenant; extra narrows the returns (e.g. by date) and may reference h.
func saleReturnsCTE(extra string) string {
	return `
	rt AS (
		SELECT COALESCE(
		         h.link_bill_id,
		         CASE WHEN h.remark ~ '^cancel:.*:original_bill_id=` + uuidTailRe + `$' THEN right(h.remark, 36)::uuid END,
		         (SELECT s.bill_id FROM tally.import_order_seen s
		           WHERE h.remark LIKE 'refund:%:refund_id=%'
		             AND s.tenant_id = h.tenant_id
		             AND s.platform = split_part(h.remark, ':', 2)
		             AND s.platform_order_no = substring(h.remark from '^refund:[^:]*:(.*):refund_id=.*$')
		           LIMIT 1)
		       ) AS sale_id, h.total_amount
		FROM tally.bill_head h
		WHERE h.tenant_id = $1 AND h.sub_type = '销售退货' AND h.status = 2 AND h.deleted_at IS NULL` + extra + `
	), rsum AS (
		SELECT sale_id, SUM(total_amount) AS amt FROM rt WHERE sale_id IS NOT NULL GROUP BY sale_id
	)`
}

// receivablesQuery aggregates open balances per partner. $1 tenant, $2 bill type,
// $3 sub type, $4 exclusive end of the as-of day, $5 the as-of day (CST calendar date).
// Balance per bill = max(total - sales returns, 0) - paid, kept when positive (the same
// rule the sale detail uses); age = as-of day minus the bill's CST day, in whole days.
// Each bill balance is rounded to cents before summing, so buckets, rows and the grand
// total all add up exactly. Paid is the current paid_amount, not cut off at the as-of day.
func receivablesQuery(direction string) string {
	cte := "WITH"
	balance := "(b.total_amount - b.paid_amount)"
	join := ""
	if direction == appbill.ReceivableAR {
		cte = "WITH" + saleReturnsCTE(" AND h.bill_date < $4") + ","
		balance = "(GREATEST(b.total_amount - COALESCE(r.amt, 0), 0) - b.paid_amount)"
		join = " LEFT JOIN rsum r ON r.sale_id = b.id"
	}
	return cte + `
	open_bills AS (
		SELECT b.partner_id, COALESCE(p.name, '') AS partner_name, b.bill_date,
		       ROUND(` + balance + `, 2) AS bal,
		       ($5::date - (b.bill_date AT TIME ZONE 'Asia/Shanghai')::date) AS age
		FROM tally.bill_head b
		LEFT JOIN tally.partner p ON p.id = b.partner_id AND p.tenant_id = b.tenant_id` + join + `
		WHERE b.tenant_id = $1 AND b.deleted_at IS NULL AND b.status = 2
		  AND b.bill_type = $2 AND b.sub_type = $3 AND b.bill_date < $4
	)
	SELECT partner_id, MAX(partner_name),
	       SUM(bal)::text,
	       COALESCE(SUM(bal) FILTER (WHERE age <= 30), 0)::text,
	       COALESCE(SUM(bal) FILTER (WHERE age BETWEEN 31 AND 60), 0)::text,
	       COALESCE(SUM(bal) FILTER (WHERE age BETWEEN 61 AND 90), 0)::text,
	       COALESCE(SUM(bal) FILTER (WHERE age > 90), 0)::text,
	       MIN(bill_date)
	FROM open_bills
	WHERE bal > 0
	GROUP BY partner_id
	ORDER BY SUM(bal) DESC, MAX(partner_name), partner_id`
}

// PartnerReceivables implements appbill.ReceivablesRepo.
func (r *Repo) PartnerReceivables(ctx context.Context, q appbill.ReceivablesQuery) ([]appbill.PartnerReceivable, error) {
	billType, subType := "出库", "销售"
	if q.Direction == appbill.ReceivableAP {
		billType, subType = "入库", "采购"
	}
	endExclusive := q.AsOf.AddDate(0, 0, 1)
	// The AR query's first CTE reads $1 only; the AP query has no returns CTE and reads
	// all five, so one argument list serves both.
	rows, err := dbscope.From(ctx, r.db).QueryContext(ctx, receivablesQuery(q.Direction),
		q.TenantID, billType, subType, endExclusive, q.AsOf.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("bill repo: receivables: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := []appbill.PartnerReceivable{}
	for rows.Next() {
		var pr appbill.PartnerReceivable
		var pid *uuid.UUID
		var unpaid, b1, b2, b3, b4 string
		var oldest time.Time
		if err := rows.Scan(&pid, &pr.PartnerName, &unpaid, &b1, &b2, &b3, &b4, &oldest); err != nil {
			return nil, fmt.Errorf("bill repo: receivables scan: %w", err)
		}
		pr.PartnerID = pid
		pr.OldestBillDate = &oldest
		for _, f := range []struct {
			dst  *decimal.Decimal
			s, n string
		}{{&pr.Unpaid, unpaid, "unpaid"}, {&pr.B0to30, b1, "b0_30"}, {&pr.B31to60, b2, "b31_60"}, {&pr.B61to90, b3, "b61_90"}, {&pr.B90Plus, b4, "b90p"}} {
			if *f.dst, err = decimalutil.Parse(f.s, f.n); err != nil {
				return nil, fmt.Errorf("bill repo: receivables scan: %w", err)
			}
		}
		out = append(out, pr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("bill repo: receivables rows: %w", err)
	}
	return out, nil
}
