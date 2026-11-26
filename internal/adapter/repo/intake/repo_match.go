package intake

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	domainbill "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/intake"
)

func (r *Repo) FindNear(ctx context.Context, tenant uuid.UUID, taxNo string, day time.Time, total decimal.Decimal) (*domain.Hit, error) {
	var id uuid.UUID
	var status, issued, number string
	err := r.q(ctx).QueryRowContext(ctx, `
		SELECT id, status, COALESCE(to_char(issue_date,'YYYY-MM-DD'),''), COALESCE(invoice_no,'')
		FROM tally.intake_document
		WHERE tenant_id=$1 AND seller_tax_no=$2 AND issue_date=$3
		  AND total_amount IS NOT NULL AND abs(total_amount - $4::numeric) <= 0.01
		  AND status <> 'rejected'
		LIMIT 1`, tenant, taxNo, day, total.String()).Scan(&id, &status, &issued, &number)
	if errors.Is(err, sql.ErrNoRows) {
		return r.findNearInvoice(ctx, tenant, taxNo, day, total)
	}
	if err != nil {
		return nil, err
	}
	return &domain.Hit{DocumentID: id, Status: status, IssueDate: issued, InvoiceNo: number, Booked: status == domain.StatusAccepted}, nil
}

func (r *Repo) findNearInvoice(ctx context.Context, tenant uuid.UUID, taxNo string, day time.Time, total decimal.Decimal) (*domain.Hit, error) {
	var id uuid.UUID
	var status, issued, number string
	err := r.q(ctx).QueryRowContext(ctx, `
		SELECT i.id, i.status, to_char(i.issue_date,'YYYY-MM-DD'), i.invoice_no
		FROM tally.invoice i
		JOIN tally.partner p ON p.id = i.partner_id AND p.tenant_id = i.tenant_id
		WHERE i.tenant_id=$1 AND p.tax_no=$2 AND i.issue_date=$3 AND i.deleted_at IS NULL
		  AND abs(i.total_amount - $4::numeric) <= 0.01
		LIMIT 1`, tenant, taxNo, day, total.String()).Scan(&id, &status, &issued, &number)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &domain.Hit{InvoiceID: id, Status: status, IssueDate: issued, InvoiceNo: number, Booked: true}, nil
}

func (r *Repo) FindInvoice(ctx context.Context, tenant uuid.UUID, direction, number string) (*domain.Hit, error) {
	var id uuid.UUID
	var status, issued string
	err := r.q(ctx).QueryRowContext(ctx, `
		SELECT id, status, to_char(issue_date,'YYYY-MM-DD')
		FROM tally.invoice
		WHERE tenant_id=$1 AND direction=$2 AND invoice_no=$3 AND deleted_at IS NULL`,
		tenant, direction, number).Scan(&id, &status, &issued)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &domain.Hit{InvoiceID: id, Status: status, IssueDate: issued, InvoiceNo: number, Booked: true}, nil
}

func (r *Repo) SuggestPartner(ctx context.Context, tenant uuid.UUID, taxNo string) (uuid.UUID, string, error) {
	rows, err := r.q(ctx).QueryContext(ctx, `
		SELECT id, name FROM tally.partner
		WHERE tenant_id=$1 AND tax_no=$2 AND deleted_at IS NULL LIMIT 2`, tenant, taxNo)
	if err != nil {
		return uuid.Nil, "", err
	}
	defer rows.Close()
	var id uuid.UUID
	var name string
	n := 0
	for rows.Next() {
		n++
		if n == 1 {
			if err := rows.Scan(&id, &name); err != nil {
				return uuid.Nil, "", err
			}
		}
	}
	if n != 1 {
		return uuid.Nil, "", rows.Err()
	}
	return id, name, rows.Err()
}

func (r *Repo) ProductNames(ctx context.Context, tenant uuid.UUID) (map[string]uuid.UUID, error) {
	rows, err := r.q(ctx).QueryContext(ctx, `
		SELECT id, name FROM tally.product
		WHERE tenant_id=$1 AND deleted_at IS NULL AND name <> ''
		ORDER BY length(name) DESC LIMIT 500`, tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[name] = id
	}
	return out, rows.Err()
}

const liveInvoicedSQL = `COALESCE((SELECT SUM(ib.amount) FROM tally.invoice_bill ib
	JOIN tally.invoice iv ON iv.id = ib.invoice_id
	WHERE ib.bill_id = b.id AND iv.status = 'issued' AND iv.deleted_at IS NULL), 0)`

func (r *Repo) OpenBills(ctx context.Context, tenant, partner uuid.UUID, direction string) ([]domain.OpenBill, error) {
	sub := domainbill.BillSubTypePurchase
	if direction == domain.DirectionOut {
		sub = domainbill.BillSubTypeSale
	}
	rows, err := r.q(ctx).QueryContext(ctx, saleReturnsQuery(`
		SELECT id, bill_no, status, bill_date, total_amount, remaining FROM (
			SELECT b.id, b.bill_no, b.status,
				COALESCE(to_char(b.bill_date,'YYYY-MM-DD'),'') AS bill_date,
				TRUNC(b.total_amount, 2)::text AS total_amount,
				`+remainingSQL+`::text AS remaining
			FROM tally.bill_head b
			LEFT JOIN rsum r ON r.sale_id = b.id
			WHERE b.tenant_id=$1 AND b.partner_id=$2 AND b.sub_type=$3 AND b.deleted_at IS NULL AND b.status=$4
		) s WHERE remaining::numeric > 0
		ORDER BY bill_date, bill_no
		LIMIT 50`), tenant, partner, string(sub), int16(domainbill.StatusApproved))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanOpenBills(rows)
}

func (r *Repo) ApprovedLinks(ctx context.Context, tenant, partner uuid.UUID, ids []uuid.UUID, amount decimal.Decimal) ([]domain.BillLink, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var bills []domain.OpenBill
	for _, id := range ids {
		var status int16
		var gotPartner, total, remaining sql.NullString
		err := r.q(ctx).QueryRowContext(ctx, saleReturnsQuery(`
			SELECT b.status, b.partner_id::text, TRUNC(b.total_amount, 2)::text, `+remainingSQL+`::text
			FROM tally.bill_head b
			LEFT JOIN rsum r ON r.sale_id = b.id
			WHERE b.tenant_id=$1 AND b.id=$2 AND b.deleted_at IS NULL`), tenant, id).Scan(&status, &gotPartner, &total, &remaining)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if status != int16(domainbill.StatusApproved) || !gotPartner.Valid || gotPartner.String != partner.String() {
			continue
		}
		bills = append(bills, openBill(id, "", status, "", total.String, remaining.String))
	}
	return domain.Allocate(amount, bills, ids), nil
}

func (r *Repo) MatchReceipts(ctx context.Context, tenant uuid.UUID, partner *uuid.UUID, total decimal.Decimal) ([]domain.Match, error) {
	partnerID := any(nil)
	if partner != nil {
		partnerID = *partner
	}
	rows, err := r.q(ctx).QueryContext(ctx, `
		SELECT id::text, bill_no, total_amount::text, status::text
		FROM tally.bill_head
		WHERE tenant_id=$1 AND deleted_at IS NULL AND status=2 AND sub_type=$2
		  AND ($3::uuid IS NULL OR partner_id=$3)
		  AND abs(total_amount - $4::numeric) <= 0.01
		LIMIT 5`, tenant, string(domainbill.BillSubTypeSale), partnerID, total.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Match
	for rows.Next() {
		var m domain.Match
		if err := rows.Scan(&m.BillID, &m.BillNo, &m.Amount, &m.Status); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
