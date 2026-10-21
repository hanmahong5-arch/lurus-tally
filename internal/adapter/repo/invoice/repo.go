// Package invoice implements the invoice-registry repository using PostgreSQL.
// Both tables (tally.invoice, tally.invoice_bill) are FORCE-RLS tenant tables;
// every query also carries an explicit tenant_id predicate.
package invoice

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"

	repobill "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/bill"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dbscope"
	appinv "github.com/hanmahong5-arch/lurus-tally/internal/app/invoice"
	domainbill "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/invoice"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/decimalutil"
)

const dateLayout = "2006-01-02"

// Repo implements appinv.Repository.
type Repo struct {
	db *sql.DB
}

// New creates a Repo backed by the shared connection pool.
func New(db *sql.DB) *Repo { return &Repo{db: db} }

var _ appinv.Repository = (*Repo)(nil)

// WithTx runs fn in a transaction on the tenant-pinned connection when the
// request pinned one (so RLS binds every statement), else on the pool.
func (r *Repo) WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := dbscope.BeginTx(ctx, r.db, nil)
	if err != nil {
		return fmt.Errorf("invoice repo: begin tx: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()
	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("invoice repo: rollback (%v): %w", err, rbErr)
		}
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("invoice repo: commit tx: %w", err)
	}
	return nil
}

// uuidArray renders ids as a PG array literal for `$n::uuid[]`.
func uuidArray(ids []uuid.UUID) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = id.String()
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// PartnerName returns the live partner's name, or ErrNotFound.
func (r *Repo) PartnerName(ctx context.Context, tx *sql.Tx, tenantID, partnerID uuid.UUID) (string, error) {
	const q = `SELECT name FROM tally.partner WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL`
	var name string
	err := tx.QueryRowContext(ctx, q, partnerID, tenantID).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", appinv.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("invoice repo: partner name: %w", err)
	}
	return name, nil
}

// LockBills row-locks the tenant's live bills among ids and reads their invoiced amounts.
func (r *Repo) LockBills(ctx context.Context, tx *sql.Tx, tenantID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]appinv.BillRef, error) {
	const q = `
		SELECT id, bill_no, sub_type, status, partner_id, total_amount::text, COALESCE(currency, 'CNY')
		FROM tally.bill_head
		WHERE tenant_id = $1 AND deleted_at IS NULL AND id = ANY($2::uuid[])
		ORDER BY id
		FOR UPDATE`
	arr := uuidArray(ids)
	rows, err := tx.QueryContext(ctx, q, tenantID, arr)
	if err != nil {
		return nil, fmt.Errorf("invoice repo: lock bills: %w", err)
	}
	out := make(map[uuid.UUID]appinv.BillRef, len(ids))
	for rows.Next() {
		var b appinv.BillRef
		var sub string
		var status int16
		var total string
		if err := rows.Scan(&b.ID, &b.BillNo, &sub, &status, &b.PartnerID, &total, &b.Currency); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("invoice repo: lock bills scan: %w", err)
		}
		b.SubType = domainbill.BillSubType(sub)
		b.Status = domainbill.BillStatus(status)
		if b.TotalAmount, err = decimalutil.Parse(total, "total_amount"); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("invoice repo: lock bills: %w", err)
		}
		out[b.ID] = b
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("invoice repo: lock bills rows: %w", err)
	}
	_ = rows.Close()

	// A sale is invoiceable only up to what stays sold: take approved sales returns off its
	// total (floored at zero), by the same rule as the bill list and the receivables ageing.
	rrows, err := tx.QueryContext(ctx, "WITH"+repobill.SaleReturnsCTE("")+` SELECT sale_id, amt::text FROM rsum WHERE sale_id = ANY($2::uuid[])`, tenantID, arr)
	if err != nil {
		return nil, fmt.Errorf("invoice repo: sale returns: %w", err)
	}
	for rrows.Next() {
		var id uuid.UUID
		var sum string
		if err := rrows.Scan(&id, &sum); err != nil {
			_ = rrows.Close()
			return nil, fmt.Errorf("invoice repo: sale returns scan: %w", err)
		}
		v, err := decimalutil.Parse(sum, "returned")
		if err != nil {
			_ = rrows.Close()
			return nil, fmt.Errorf("invoice repo: sale returns: %w", err)
		}
		if b, ok := out[id]; ok {
			b.TotalAmount = decimal.Max(b.TotalAmount.Sub(v), decimal.Zero)
			out[id] = b
		}
	}
	if err := rrows.Err(); err != nil {
		_ = rrows.Close()
		return nil, fmt.Errorf("invoice repo: sale returns rows: %w", err)
	}
	_ = rrows.Close()

	const iq = `
		SELECT ib.bill_id, SUM(ib.amount)::text
		FROM tally.invoice_bill ib
		JOIN tally.invoice i ON i.id = ib.invoice_id
		WHERE ib.tenant_id = $1 AND ib.bill_id = ANY($2::uuid[])
		  AND i.status = 'issued' AND i.deleted_at IS NULL
		GROUP BY ib.bill_id`
	irows, err := tx.QueryContext(ctx, iq, tenantID, arr)
	if err != nil {
		return nil, fmt.Errorf("invoice repo: invoiced amounts: %w", err)
	}
	defer func() { _ = irows.Close() }()
	for irows.Next() {
		var id uuid.UUID
		var sum string
		if err := irows.Scan(&id, &sum); err != nil {
			return nil, fmt.Errorf("invoice repo: invoiced scan: %w", err)
		}
		v, err := decimalutil.Parse(sum, "invoiced")
		if err != nil {
			return nil, fmt.Errorf("invoice repo: invoiced: %w", err)
		}
		if b, ok := out[id]; ok {
			b.Invoiced = v
			out[id] = b
		}
	}
	if err := irows.Err(); err != nil {
		return nil, fmt.Errorf("invoice repo: invoiced rows: %w", err)
	}
	return out, nil
}

// Insert writes the invoice head and its links.
func (r *Repo) Insert(ctx context.Context, tx *sql.Tx, inv *domain.Invoice) error {
	const q = `
		INSERT INTO tally.invoice
			(id, tenant_id, direction, invoice_no, invoice_code, invoice_type, partner_id, issue_date,
			 amount_excl_tax, tax_amount, total_amount, status, red_of_id, remark, created_by, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8::date,$9,$10,$11,$12,$13,$14,$15,$16,$17)`
	_, err := tx.ExecContext(ctx, q,
		inv.ID, inv.TenantID, string(inv.Direction), inv.InvoiceNo, nullableString(inv.InvoiceCode), inv.InvoiceType,
		inv.PartnerID, inv.IssueDate.Format(dateLayout),
		inv.AmountExclTax.String(), inv.TaxAmount.String(), inv.TotalAmount.String(),
		string(inv.Status), inv.RedOfID, nullableString(inv.Remark), inv.CreatedBy, inv.CreatedAt, inv.UpdatedAt)
	if err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "23505" {
			return appinv.ErrDuplicateNo
		}
		return fmt.Errorf("invoice repo: insert: %w", err)
	}
	const lq = `INSERT INTO tally.invoice_bill (invoice_id, bill_id, tenant_id, amount) VALUES ($1,$2,$3,$4)`
	for _, l := range inv.Links {
		if _, err := tx.ExecContext(ctx, lq, inv.ID, l.BillID, inv.TenantID, l.Amount.String()); err != nil {
			return fmt.Errorf("invoice repo: insert link: %w", err)
		}
	}
	return nil
}

const headCols = `i.id, i.tenant_id, i.direction, i.invoice_no, COALESCE(i.invoice_code,''), i.invoice_type, i.partner_id,
	i.issue_date, i.amount_excl_tax::text, i.tax_amount::text, i.total_amount::text, i.status, i.red_of_id,
	COALESCE(i.remark,''), i.created_by, i.created_at, i.updated_at, COALESCE(p.name,'')`

const headFrom = ` FROM tally.invoice i
	LEFT JOIN tally.partner p ON p.id = i.partner_id AND p.tenant_id = i.tenant_id`

type scanner interface{ Scan(dest ...any) error }

func scanHead(s scanner) (*domain.Invoice, error) {
	var inv domain.Invoice
	var dir, status, excl, tax, total string
	if err := s.Scan(&inv.ID, &inv.TenantID, &dir, &inv.InvoiceNo, &inv.InvoiceCode, &inv.InvoiceType, &inv.PartnerID,
		&inv.IssueDate, &excl, &tax, &total, &status, &inv.RedOfID,
		&inv.Remark, &inv.CreatedBy, &inv.CreatedAt, &inv.UpdatedAt, &inv.PartnerName); err != nil {
		return nil, err
	}
	inv.Direction = domain.Direction(dir)
	inv.Status = domain.Status(status)
	var err error
	if inv.AmountExclTax, err = decimalutil.Parse(excl, "amount_excl_tax"); err != nil {
		return nil, err
	}
	if inv.TaxAmount, err = decimalutil.Parse(tax, "tax_amount"); err != nil {
		return nil, err
	}
	if inv.TotalAmount, err = decimalutil.Parse(total, "total_amount"); err != nil {
		return nil, err
	}
	return &inv, nil
}

// GetForUpdate loads and row-locks one invoice head.
func (r *Repo) GetForUpdate(ctx context.Context, tx *sql.Tx, tenantID, id uuid.UUID) (*domain.Invoice, error) {
	q := `SELECT ` + headCols + headFrom + `
		WHERE i.id = $1 AND i.tenant_id = $2 AND i.deleted_at IS NULL
		FOR UPDATE OF i`
	inv, err := scanHead(tx.QueryRowContext(ctx, q, id, tenantID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, appinv.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("invoice repo: get for update: %w", err)
	}
	return inv, nil
}

// SetStatus moves an invoice from one status to another.
func (r *Repo) SetStatus(ctx context.Context, tx *sql.Tx, tenantID, id uuid.UUID, from, to domain.Status) (bool, error) {
	const q = `
		UPDATE tally.invoice SET status = $1, updated_at = now()
		WHERE id = $2 AND tenant_id = $3 AND status = $4 AND deleted_at IS NULL`
	res, err := tx.ExecContext(ctx, q, string(to), id, tenantID, string(from))
	if err != nil {
		return false, fmt.Errorf("invoice repo: set status: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("invoice repo: set status rows: %w", err)
	}
	return n == 1, nil
}

// GetByID loads one invoice with its links.
func (r *Repo) GetByID(ctx context.Context, tenantID, id uuid.UUID) (*domain.Invoice, error) {
	q := `SELECT ` + headCols + headFrom + ` WHERE i.id = $1 AND i.tenant_id = $2 AND i.deleted_at IS NULL`
	db := dbscope.From(ctx, r.db)
	inv, err := scanHead(db.QueryRowContext(ctx, q, id, tenantID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, appinv.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("invoice repo: get: %w", err)
	}
	const lq = `
		SELECT ib.bill_id, COALESCE(b.bill_no,''), ib.amount::text
		FROM tally.invoice_bill ib
		LEFT JOIN tally.bill_head b ON b.id = ib.bill_id AND b.tenant_id = ib.tenant_id
		WHERE ib.invoice_id = $1 AND ib.tenant_id = $2
		ORDER BY b.bill_no, ib.bill_id`
	rows, err := db.QueryContext(ctx, lq, id, tenantID)
	if err != nil {
		return nil, fmt.Errorf("invoice repo: get links: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var l domain.Link
		var amt string
		if err := rows.Scan(&l.BillID, &l.BillNo, &amt); err != nil {
			return nil, fmt.Errorf("invoice repo: get links scan: %w", err)
		}
		if l.Amount, err = decimalutil.Parse(amt, "amount"); err != nil {
			return nil, fmt.Errorf("invoice repo: get links: %w", err)
		}
		inv.Links = append(inv.Links, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("invoice repo: get links rows: %w", err)
	}
	return inv, nil
}

// buildWhere renders the shared WHERE (alias i = invoice, p = partner). Date bounds
// are compared as calendar days: the filter's times already sit in Asia/Shanghai,
// so their wall-clock date is the day the caller meant.
func buildWhere(f domain.ListFilter) (string, []any, int) {
	args := []any{f.TenantID}
	idx := 2
	where := []string{"i.tenant_id = $1", "i.deleted_at IS NULL"}
	add := func(cond string, arg any) {
		where = append(where, fmt.Sprintf(cond, idx))
		args = append(args, arg)
		idx++
	}
	if f.Direction != "" {
		add("i.direction = $%d", string(f.Direction))
	}
	if f.Status != "" {
		add("i.status = $%d", string(f.Status))
	}
	if f.PartnerID != nil {
		add("i.partner_id = $%d", *f.PartnerID)
	}
	if f.BillID != nil {
		add("EXISTS (SELECT 1 FROM tally.invoice_bill ib WHERE ib.invoice_id = i.id AND ib.tenant_id = i.tenant_id AND ib.bill_id = $%d)", *f.BillID)
	}
	if f.DateFrom != nil {
		add("i.issue_date >= $%d::date", f.DateFrom.Format(dateLayout))
	}
	if f.DateTo != nil {
		add("i.issue_date < $%d::date", f.DateTo.Format(dateLayout))
	}
	if f.Keyword != "" {
		add(`i.invoice_no ILIKE $%d ESCAPE '\'`, "%"+escapeLike(f.Keyword)+"%")
	}
	return strings.Join(where, " AND "), args, idx
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// List returns a page of invoices (newest issue date first) and the total count.
func (r *Repo) List(ctx context.Context, f domain.ListFilter) ([]*domain.Invoice, int, error) {
	where, args, idx := buildWhere(f)
	db := dbscope.From(ctx, r.db)

	var total int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*)`+headFrom+` WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("invoice repo: list count: %w", err)
	}
	q := `SELECT ` + headCols + headFrom + ` WHERE ` + where +
		fmt.Sprintf(` ORDER BY i.issue_date DESC, i.created_at DESC, i.id DESC LIMIT $%d OFFSET $%d`, idx, idx+1)
	args = append(args, f.Limit, f.Offset)
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("invoice repo: list: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var items []*domain.Invoice
	for rows.Next() {
		inv, err := scanHead(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("invoice repo: list scan: %w", err)
		}
		items = append(items, inv)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("invoice repo: list rows: %w", err)
	}
	if err := r.attachLinks(ctx, f.TenantID, items); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// attachLinks loads the bill links of a page of invoices in one query.
func (r *Repo) attachLinks(ctx context.Context, tenantID uuid.UUID, items []*domain.Invoice) error {
	if len(items) == 0 {
		return nil
	}
	byID := make(map[uuid.UUID]*domain.Invoice, len(items))
	ids := make([]uuid.UUID, len(items))
	for i, inv := range items {
		byID[inv.ID] = inv
		ids[i] = inv.ID
	}
	const lq = `
		SELECT ib.invoice_id, ib.bill_id, COALESCE(b.bill_no,''), ib.amount::text
		FROM tally.invoice_bill ib
		LEFT JOIN tally.bill_head b ON b.id = ib.bill_id AND b.tenant_id = ib.tenant_id
		WHERE ib.tenant_id = $1 AND ib.invoice_id = ANY($2::uuid[])
		ORDER BY b.bill_no, ib.bill_id`
	rows, err := dbscope.From(ctx, r.db).QueryContext(ctx, lq, tenantID, uuidArray(ids))
	if err != nil {
		return fmt.Errorf("invoice repo: list links: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var invID uuid.UUID
		var l domain.Link
		var amt string
		if err := rows.Scan(&invID, &l.BillID, &l.BillNo, &amt); err != nil {
			return fmt.Errorf("invoice repo: list links scan: %w", err)
		}
		if l.Amount, err = decimalutil.Parse(amt, "amount"); err != nil {
			return fmt.Errorf("invoice repo: list links: %w", err)
		}
		if inv, ok := byID[invID]; ok {
			inv.Links = append(inv.Links, l)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("invoice repo: list links rows: %w", err)
	}
	return nil
}

// Summarize aggregates the invoices matching f. Void invoices are left out of the
// amounts; a struck original (status red) stays in so it nets to zero against its
// negative red-letter invoice.
func (r *Repo) Summarize(ctx context.Context, f domain.ListFilter) (domain.Summary, error) {
	where, args, _ := buildWhere(f)
	q := `
		SELECT COUNT(*),
		       COALESCE(SUM(i.amount_excl_tax) FILTER (WHERE i.status <> 'void'), 0)::text,
		       COALESCE(SUM(i.tax_amount) FILTER (WHERE i.status <> 'void'), 0)::text,
		       COALESCE(SUM(i.total_amount) FILTER (WHERE i.status <> 'void'), 0)::text` +
		headFrom + ` WHERE ` + where
	var s domain.Summary
	var excl, tax, total string
	if err := dbscope.From(ctx, r.db).QueryRowContext(ctx, q, args...).Scan(&s.Count, &excl, &tax, &total); err != nil {
		return s, fmt.Errorf("invoice repo: summarize: %w", err)
	}
	var err error
	if s.AmountExclTax, err = decimalutil.Parse(excl, "amount_excl_tax"); err != nil {
		return s, err
	}
	if s.TaxAmount, err = decimalutil.Parse(tax, "tax_amount"); err != nil {
		return s, err
	}
	if s.TotalAmount, err = decimalutil.Parse(total, "total_amount"); err != nil {
		return s, err
	}
	return s, nil
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
