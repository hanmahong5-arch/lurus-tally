package bill

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dbscope"
	apppayment "github.com/hanmahong5-arch/lurus-tally/internal/app/payment"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/decimalutil"
)

// Ensure Repo satisfies the partner ledger port at compile time.
var _ apppayment.PartnerLedger = (*Repo)(nil)

// purchaseReturnOffsetRemark starts the remark of the credit-type payment a purchase return
// books to cancel part of the payable; it tells a return offset from any other credit entry.
const purchaseReturnOffsetRemark = "采购退货冲抵"

// ledgerTypes maps a direction to the bill type / sub type of the documents that make
// the partner owe: sales for ar, purchases for ap.
func ledgerTypes(direction string) (billType, subType string) {
	if direction == apppayment.DirectionPayable {
		return "入库", "采购"
	}
	return "出库", "销售"
}

// openBillsQuery lists one partner's approved bills with a positive balance, oldest first.
// $1 tenant, $2 partner, $3 bill type, $4 sub type. The balance rule is the ageing's:
// max(total - sales returns, 0) - paid, rounded to cents, kept when positive.
func openBillsQuery(direction string) string {
	cte, balance, join := "", "(b.total_amount - b.paid_amount)", ""
	if direction == apppayment.DirectionReceivable {
		cte = "WITH" + saleReturnsCTE("")
		balance = "(GREATEST(b.total_amount - COALESCE(r.amt, 0), 0) - b.paid_amount)"
		join = " LEFT JOIN rsum r ON r.sale_id = b.id"
	}
	return cte + `
	SELECT id, bill_no, bill_date, total::text, bal::text FROM (
		SELECT b.id, b.bill_no, b.bill_date, b.created_at, b.total_amount AS total, ROUND(` + balance + `, 2) AS bal
		FROM tally.bill_head b` + join + `
		WHERE b.tenant_id = $1 AND b.partner_id = $2 AND b.deleted_at IS NULL AND b.status = 2
		  AND b.bill_type = $3 AND b.sub_type = $4
	) x
	WHERE bal > 0
	ORDER BY bill_date ASC, created_at ASC, id ASC`
}

// OpenBills implements apppayment.PartnerLedger.
func (r *Repo) OpenBills(ctx context.Context, tenantID uuid.UUID, direction string, partnerID uuid.UUID) ([]apppayment.OpenBill, error) {
	billType, subType := ledgerTypes(direction)
	rows, err := dbscope.From(ctx, r.db).QueryContext(ctx, openBillsQuery(direction), tenantID, partnerID, billType, subType)
	if err != nil {
		return nil, fmt.Errorf("bill repo: open bills: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := []apppayment.OpenBill{}
	for rows.Next() {
		var b apppayment.OpenBill
		var total, bal string
		if err := rows.Scan(&b.BillID, &b.BillNo, &b.BillDate, &total, &bal); err != nil {
			return nil, fmt.Errorf("bill repo: open bills scan: %w", err)
		}
		if b.Total, err = decimalutil.Parse(total, "total"); err != nil {
			return nil, fmt.Errorf("bill repo: open bills scan: %w", err)
		}
		if b.Outstanding, err = decimalutil.Parse(bal, "balance"); err != nil {
			return nil, fmt.Errorf("bill repo: open bills scan: %w", err)
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("bill repo: open bills rows: %w", err)
	}
	return out, nil
}

// balanceAtQuery sums one partner's per-bill balances over bills dated before $4, counting
// only the returns dated before $4 and, unless $6 (the cut-off is in the future), only the
// payments dated before $4. With $6 it reads paid_amount, which is exactly what the ageing
// reads, so a statement ending today closes on the ageing figure. $1 tenant, $2 partner,
// $3 bill type, $5 sub type.
func balanceAtQuery(direction string) string {
	cte, net, join := "WITH", "b.total_amount", ""
	if direction == apppayment.DirectionReceivable {
		cte = "WITH" + saleReturnsCTE(" AND h.bill_date < $4") + ","
		net = "GREATEST(b.total_amount - COALESCE(r.amt, 0), 0)"
		join = " LEFT JOIN rsum r ON r.sale_id = b.id"
	}
	return cte + `
	pays AS (
		SELECT related_bill_id, SUM(amount) AS amt FROM tally.payment_head
		WHERE tenant_id = $1 AND deleted_at IS NULL AND pay_date < $4 GROUP BY related_bill_id
	)
	SELECT COALESCE(SUM(bal), 0)::text FROM (
		SELECT ROUND(` + net + ` - CASE WHEN $6::boolean THEN b.paid_amount ELSE COALESCE(pa.amt, 0) END, 2) AS bal
		FROM tally.bill_head b` + join + `
		LEFT JOIN pays pa ON pa.related_bill_id = b.id
		WHERE b.tenant_id = $1 AND b.partner_id = $2 AND b.deleted_at IS NULL AND b.status = 2
		  AND b.bill_type = $3 AND b.sub_type = $5 AND b.bill_date < $4
	) x
	WHERE bal > 0`
}

func (r *Repo) balanceAt(ctx context.Context, q apppayment.StatementQuery, cutoff time.Time) (decimal.Decimal, error) {
	billType, subType := ledgerTypes(q.Direction)
	var s string
	err := dbscope.From(ctx, r.db).QueryRowContext(ctx, balanceAtQuery(q.Direction),
		q.TenantID, q.PartnerID, billType, cutoff, subType, cutoff.After(time.Now())).Scan(&s)
	if err != nil {
		return decimal.Zero, fmt.Errorf("bill repo: balance at: %w", err)
	}
	return decimalutil.Parse(s, "balance")
}

// Statement implements apppayment.PartnerLedger: the opening and closing balances of the
// period plus every bill, return and payment inside it, oldest first.
func (r *Repo) Statement(ctx context.Context, q apppayment.StatementQuery) (*apppayment.Statement, error) {
	db := dbscope.From(ctx, r.db)
	st := &apppayment.Statement{Direction: q.Direction, PartnerID: q.PartnerID, From: q.From, To: q.To,
		Lines: []apppayment.StatementLine{}}

	err := db.QueryRowContext(ctx, `SELECT name FROM tally.partner WHERE id = $1 AND tenant_id = $2`, q.PartnerID, q.TenantID).Scan(&st.PartnerName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apppayment.ErrPartnerNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("bill repo: statement partner: %w", err)
	}

	end := q.To.AddDate(0, 0, 1)
	if st.Opening, err = r.balanceAt(ctx, q, q.From); err != nil {
		return nil, err
	}
	if st.Closing, err = r.balanceAt(ctx, q, end); err != nil {
		return nil, err
	}

	billType, subType := ledgerTypes(q.Direction)
	lines, err := r.statementLines(ctx, db, q, billType, subType, end)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(lines, func(i, j int) bool {
		if !lines[i].Date.Equal(lines[j].Date) {
			return lines[i].Date.Before(lines[j].Date)
		}
		return lineRank(lines[i].Kind) < lineRank(lines[j].Kind)
	})
	running := st.Opening
	for i := range lines {
		running = running.Add(lines[i].Delta)
		lines[i].Balance = running
		switch lines[i].Kind {
		case apppayment.LineBill:
			st.Billed = st.Billed.Add(lines[i].Delta)
		case apppayment.LineReturn:
			st.Returned = st.Returned.Add(lines[i].Delta.Neg())
		default:
			st.Paid = st.Paid.Add(lines[i].Delta.Neg())
		}
	}
	st.Lines = append(st.Lines, lines...)
	st.Adjustment = st.Closing.Sub(st.Opening.Add(st.Billed).Sub(st.Returned).Sub(st.Paid))
	return st, nil
}

func lineRank(kind string) int {
	switch kind {
	case apppayment.LineBill:
		return 0
	case apppayment.LineReturn:
		return 1
	}
	return 2
}

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func (r *Repo) statementLines(ctx context.Context, db queryer, q apppayment.StatementQuery, billType, subType string, end time.Time) ([]apppayment.StatementLine, error) {
	var lines []apppayment.StatementLine

	scan := func(query string, kind string, args ...any) error {
		rows, err := db.QueryContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("bill repo: statement %s: %w", kind, err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var l apppayment.StatementLine
			var id uuid.UUID
			var method, remark sql.NullString
			var amt string
			if err := rows.Scan(&l.Date, &id, &l.BillNo, &method, &remark, &amt); err != nil {
				return fmt.Errorf("bill repo: statement %s scan: %w", kind, err)
			}
			d, err := decimalutil.Parse(amt, "amount")
			if err != nil {
				return fmt.Errorf("bill repo: statement %s scan: %w", kind, err)
			}
			l.Kind, l.BillID, l.Method, l.Remark = kind, &id, method.String, remark.String
			// A purchase return is booked as a credit-type payment on the purchase; it is a
			// return offset, not money paid out.
			if kind == apppayment.LinePayment && q.Direction == apppayment.DirectionPayable && l.Method == "credit" &&
				strings.HasPrefix(l.Remark, purchaseReturnOffsetRemark) {
				l.Kind, l.Method = apppayment.LineReturn, ""
			}
			if kind == apppayment.LineBill {
				l.Delta = d
			} else {
				l.Delta = d.Neg()
			}
			lines = append(lines, l)
		}
		return rows.Err()
	}

	const billsQ = `
		SELECT bill_date, id, bill_no, NULL::text, remark, total_amount::text
		FROM tally.bill_head
		WHERE tenant_id = $1 AND partner_id = $2 AND deleted_at IS NULL AND status = 2
		  AND bill_type = $3 AND sub_type = $4 AND bill_date >= $5 AND bill_date < $6`
	if err := scan(billsQ, apppayment.LineBill, q.TenantID, q.PartnerID, billType, subType, q.From, end); err != nil {
		return nil, err
	}
	if q.Direction == apppayment.DirectionReceivable {
		// A sales return lowers what the customer owes; a purchase return is already
		// booked as an offsetting credit payment on the purchase and is listed from there.
		if err := scan(billsQ, apppayment.LineReturn, q.TenantID, q.PartnerID, "入库", "销售退货", q.From, end); err != nil {
			return nil, err
		}
	}
	const paysQ = `
		SELECT p.pay_date, b.id, b.bill_no, p.pay_type, p.remark, p.amount::text
		FROM tally.payment_head p
		JOIN tally.bill_head b ON b.id = p.related_bill_id AND b.tenant_id = p.tenant_id
		WHERE p.tenant_id = $1 AND b.partner_id = $2 AND p.deleted_at IS NULL AND b.deleted_at IS NULL
		  AND b.bill_type = $3 AND b.sub_type = $4 AND p.pay_date >= $5 AND p.pay_date < $6`
	if err := scan(paysQ, apppayment.LinePayment, q.TenantID, q.PartnerID, billType, subType, q.From, end); err != nil {
		return nil, err
	}
	return lines, nil
}
