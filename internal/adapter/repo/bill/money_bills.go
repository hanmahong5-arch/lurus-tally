package bill

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dbscope"
	domainpayment "github.com/hanmahong5-arch/lurus-tally/internal/domain/payment"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/decimalutil"
)

func moneyBillsSQL(where string) string {
	return "WITH" + saleReturnsCTE("") + `
	SELECT id, bill_no, bill_date, partner_name, direction, bal::text FROM (
		SELECT b.id, b.bill_no, b.bill_date, b.created_at, COALESCE(p.name, '') AS partner_name,
		       '` + domainpayment.MoneyAR + `' AS direction,
		       ROUND(GREATEST(b.total_amount - COALESCE(r.amt, 0), 0) - b.paid_amount, 2) AS bal
		FROM tally.bill_head b
		LEFT JOIN tally.partner p ON p.id = b.partner_id AND p.tenant_id = b.tenant_id
		LEFT JOIN rsum r ON r.sale_id = b.id
		WHERE b.tenant_id = $1 AND b.deleted_at IS NULL AND b.status = 2
		  AND b.bill_type = '出库' AND b.sub_type = '销售'
		UNION ALL
		SELECT b.id, b.bill_no, b.bill_date, b.created_at, COALESCE(p.name, ''),
		       '` + domainpayment.MoneyAP + `',
		       ROUND(b.total_amount - b.paid_amount, 2)
		FROM tally.bill_head b
		LEFT JOIN tally.partner p ON p.id = b.partner_id AND p.tenant_id = b.tenant_id
		WHERE b.tenant_id = $1 AND b.deleted_at IS NULL AND b.status = 2
		  AND b.bill_type = '入库' AND b.sub_type = '采购'
	) x
	WHERE ` + where + `
	ORDER BY bill_date ASC, created_at ASC, id ASC`
}

func (r *Repo) ListOpenMoneyBills(ctx context.Context, tenant uuid.UUID) ([]domainpayment.MoneyBill, error) {
	rows, err := dbscope.From(ctx, r.db).QueryContext(ctx, moneyBillsSQL("bal > 0"), tenant)
	if err != nil {
		return nil, fmt.Errorf("bill repo: money bills: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domainpayment.MoneyBill
	for rows.Next() {
		b, err := scanMoneyBill(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("bill repo: money bills: %w", err)
	}
	return out, nil
}

func (r *Repo) OpenMoneyBill(ctx context.Context, tenant, bill uuid.UUID) (domainpayment.MoneyBill, error) {
	b, err := scanMoneyBill(dbscope.From(ctx, r.db).QueryRowContext(ctx, moneyBillsSQL("id = $2"), tenant, bill))
	if errors.Is(err, sql.ErrNoRows) {
		return domainpayment.MoneyBill{}, domainpayment.ErrMoneyBill
	}
	if err != nil {
		return domainpayment.MoneyBill{}, fmt.Errorf("bill repo: money bill: %w", err)
	}
	return b, nil
}

func scanMoneyBill(sc interface{ Scan(...any) error }) (domainpayment.MoneyBill, error) {
	var b domainpayment.MoneyBill
	var bal string
	if err := sc.Scan(&b.ID, &b.BillNo, &b.BillDate, &b.PartnerName, &b.Direction, &bal); err != nil {
		return domainpayment.MoneyBill{}, err
	}
	var err error
	if b.Outstanding, err = decimalutil.Parse(bal, "balance"); err != nil {
		return domainpayment.MoneyBill{}, fmt.Errorf("bill repo: money bill: %w", err)
	}
	return b, nil
}
