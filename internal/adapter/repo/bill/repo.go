// Package bill implements BillRepo backed by PostgreSQL.
// All mutating operations require a *sql.Tx and respect RLS (app.tenant_id must be set).
// WHERE tenant_id = $n is used as a defensive second filter even under RLS.
package bill

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

// ErrNotFound is returned when a bill_head row is not found.
var ErrNotFound = errors.New("bill repo: not found")

// Repo implements appbill.BillRepo.
type Repo struct {
	db *sql.DB
}

// New creates a Repo.
func New(db *sql.DB) *Repo {
	return &Repo{db: db}
}

// Ensure Repo satisfies the interface at compile time.
var _ appbill.BillRepo = (*Repo)(nil)

// ----- Transaction boundary -----

func (r *Repo) WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("bill repo: begin tx: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()
	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("bill repo: rollback (%v): %w", err, rbErr)
		}
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("bill repo: commit tx: %w", err)
	}
	return nil
}

// ----- Advisory lock -----

// advisoryKey hashes tenantID + billID into a stable int64 for pg_advisory_xact_lock.
func advisoryKey(tenantID, billID uuid.UUID) int64 {
	h := fnv.New64a()
	_, _ = h.Write(tenantID[:])
	_, _ = h.Write(billID[:])
	return int64(h.Sum64())
}

func (r *Repo) AcquireBillAdvisoryLock(ctx context.Context, tx *sql.Tx, tenantID, billID uuid.UUID) error {
	key := advisoryKey(tenantID, billID)
	_, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", key)
	if err != nil {
		return fmt.Errorf("bill repo: advisory lock: %w", err)
	}
	return nil
}

// ----- bill_no sequence -----

// NextBillNo generates the next bill number for the (tenantID, prefix, today) combination.
// Uses the existing tally.bill_sequence table (from migration 000010).
// Schema: (id UUID PK, tenant_id UUID, prefix VARCHAR(20), current_val BIGINT, UNIQUE(tenant_id, prefix)).
//
// We use a day-keyed prefix (prefix+date) stored as the sequence prefix so per-day reset works.
// Format: "PO-20260423-0001"
func (r *Repo) NextBillNo(ctx context.Context, tx *sql.Tx, tenantID uuid.UUID, prefix string) (string, error) {
	today := time.Now().UTC().Format("20060102")
	seqPrefix := prefix + "-" + today // "PO-20260423"

	// Upsert: insert seq=1 or increment by 1, return current value.
	const q = `
		INSERT INTO tally.bill_sequence (id, tenant_id, prefix, current_val)
		VALUES ($1, $2, $3, 1)
		ON CONFLICT (tenant_id, prefix) DO UPDATE
			SET current_val = tally.bill_sequence.current_val + 1
		RETURNING current_val`

	var seq int64
	var row *sql.Row
	if tx != nil {
		row = tx.QueryRowContext(ctx, q, uuid.New(), tenantID, seqPrefix)
	} else {
		row = r.db.QueryRowContext(ctx, q, uuid.New(), tenantID, seqPrefix)
	}
	if err := row.Scan(&seq); err != nil {
		return "", fmt.Errorf("bill repo: next bill no: %w", err)
	}

	return fmt.Sprintf("%s-%04d", seqPrefix, seq), nil
}

// ----- bill_head CRUD -----

func (r *Repo) CreateBill(ctx context.Context, tx *sql.Tx, head *domain.BillHead, items []*domain.BillItem) error {
	const headQ = `
		INSERT INTO tally.bill_head
			(id, tenant_id, bill_no, bill_type, sub_type, status, partner_id, warehouse_id,
			 creator_id, bill_date, subtotal, shipping_fee, tax_amount, total_amount, remark,
			 created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`

	_, err := tx.ExecContext(ctx, headQ,
		head.ID, head.TenantID, head.BillNo, string(head.BillType), string(head.SubType),
		int16(head.Status), head.PartnerID, head.WarehouseID,
		head.CreatorID, head.BillDate,
		head.Subtotal.String(), head.ShippingFee.String(), head.TaxAmount.String(), head.TotalAmount.String(),
		head.Remark, head.CreatedAt, head.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("bill repo: create head: %w", err)
	}

	if err := r.insertItems(ctx, tx, items); err != nil {
		return err
	}
	return nil
}

func (r *Repo) insertItems(ctx context.Context, tx *sql.Tx, items []*domain.BillItem) error {
	if len(items) == 0 {
		return nil
	}
	const itemQ = `
		INSERT INTO tally.bill_item
			(id, tenant_id, head_id, product_id, unit_id, unit_name, line_no, qty, unit_price, line_amount, remark)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`

	for _, it := range items {
		_, err := tx.ExecContext(ctx, itemQ,
			it.ID, it.TenantID, it.HeadID, it.ProductID,
			it.UnitID, it.UnitName, it.LineNo,
			it.Qty.String(), it.UnitPrice.String(), it.LineAmount.String(),
			it.Remark,
		)
		if err != nil {
			return fmt.Errorf("bill repo: create item line_no=%d: %w", it.LineNo, err)
		}
	}
	return nil
}

func (r *Repo) GetBillForUpdate(ctx context.Context, tx *sql.Tx, tenantID, billID uuid.UUID) (*domain.BillHead, error) {
	const q = `
		SELECT id, tenant_id, bill_no, bill_type, sub_type, status, partner_id, warehouse_id,
		       creator_id, bill_date, subtotal, shipping_fee, tax_amount, total_amount,
		       paid_amount, approved_at, approved_by, remark, created_at, updated_at
		FROM tally.bill_head
		WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL
		FOR UPDATE`

	return scanBillHead(tx.QueryRowContext(ctx, q, billID, tenantID))
}

func (r *Repo) GetBill(ctx context.Context, tenantID, billID uuid.UUID) (*domain.BillHead, error) {
	const q = `
		SELECT id, tenant_id, bill_no, bill_type, sub_type, status, partner_id, warehouse_id,
		       creator_id, bill_date, subtotal, shipping_fee, tax_amount, total_amount,
		       paid_amount, approved_at, approved_by, remark, created_at, updated_at
		FROM tally.bill_head
		WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL`

	return scanBillHead(r.db.QueryRowContext(ctx, q, billID, tenantID))
}

func scanBillHead(row *sql.Row) (*domain.BillHead, error) {
	var h domain.BillHead
	var billType, subType string
	var status int16
	var subtotal, shippingFee, taxAmount, totalAmount, paidAmount string
	err := row.Scan(
		&h.ID, &h.TenantID, &h.BillNo, &billType, &subType, &status,
		&h.PartnerID, &h.WarehouseID,
		&h.CreatorID, &h.BillDate,
		&subtotal, &shippingFee, &taxAmount, &totalAmount,
		&paidAmount,
		&h.ApprovedAt, &h.ApprovedBy,
		&h.Remark, &h.CreatedAt, &h.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, appbill.ErrBillNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("bill repo: scan head: %w", err)
	}
	h.BillType = domain.BillType(billType)
	h.SubType = domain.BillSubType(subType)
	h.Status = domain.BillStatus(status)
	h.Subtotal, _ = decimal.NewFromString(subtotal)
	h.ShippingFee, _ = decimal.NewFromString(shippingFee)
	h.TaxAmount, _ = decimal.NewFromString(taxAmount)
	h.TotalAmount, _ = decimal.NewFromString(totalAmount)
	h.PaidAmount, _ = decimal.NewFromString(paidAmount)
	return &h, nil
}

func (r *Repo) GetBillItems(ctx context.Context, tenantID, billID uuid.UUID) ([]*domain.BillItem, error) {
	const q = `
		SELECT id, tenant_id, head_id, product_id, unit_id, unit_name, line_no, qty, unit_price, line_amount, remark
		FROM tally.bill_item
		WHERE head_id = $1 AND tenant_id = $2
		ORDER BY line_no ASC`

	rows, err := r.db.QueryContext(ctx, q, billID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("bill repo: get items: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []*domain.BillItem
	for rows.Next() {
		var it domain.BillItem
		var qty, unitPrice, lineAmount string
		if err := rows.Scan(
			&it.ID, &it.TenantID, &it.HeadID, &it.ProductID, &it.UnitID, &it.UnitName,
			&it.LineNo, &qty, &unitPrice, &lineAmount, &it.Remark,
		); err != nil {
			return nil, fmt.Errorf("bill repo: scan item: %w", err)
		}
		it.Qty, _ = decimal.NewFromString(qty)
		it.UnitPrice, _ = decimal.NewFromString(unitPrice)
		it.LineAmount, _ = decimal.NewFromString(lineAmount)
		items = append(items, &it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("bill repo: get items rows: %w", err)
	}
	return items, nil
}

func (r *Repo) UpdateBillStatus(ctx context.Context, tx *sql.Tx, tenantID, billID uuid.UUID, status domain.BillStatus, meta map[string]any) error {
	args := []any{int16(status), time.Now().UTC(), billID, tenantID}
	q := `UPDATE tally.bill_head SET status = $1, updated_at = $2`

	if meta != nil {
		if at, ok := meta["approved_at"]; ok {
			q += fmt.Sprintf(", approved_at = $%d", len(args)+1)
			args = append(args, at.(time.Time))
		}
		if by, ok := meta["approved_by"]; ok {
			q += fmt.Sprintf(", approved_by = $%d", len(args)+1)
			args = append(args, by.(uuid.UUID))
		}
	}

	q += fmt.Sprintf(" WHERE id = $%d AND tenant_id = $%d AND deleted_at IS NULL", len(args)-1, len(args))

	_, err := tx.ExecContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("bill repo: update status: %w", err)
	}
	return nil
}

func (r *Repo) UpdateBill(ctx context.Context, tx *sql.Tx, head *domain.BillHead, items []*domain.BillItem) error {
	const headQ = `
		UPDATE tally.bill_head SET
			partner_id   = $1,
			warehouse_id = $2,
			bill_date    = $3,
			subtotal     = $4,
			shipping_fee = $5,
			tax_amount   = $6,
			total_amount = $7,
			remark       = $8,
			updated_at   = $9
		WHERE id = $10 AND tenant_id = $11 AND deleted_at IS NULL`

	_, err := tx.ExecContext(ctx, headQ,
		head.PartnerID, head.WarehouseID, head.BillDate,
		head.Subtotal.String(), head.ShippingFee.String(), head.TaxAmount.String(), head.TotalAmount.String(),
		head.Remark, head.UpdatedAt, head.ID, head.TenantID,
	)
	if err != nil {
		return fmt.Errorf("bill repo: update head: %w", err)
	}

	// Delete existing items and re-insert.
	if _, err := tx.ExecContext(ctx, `DELETE FROM tally.bill_item WHERE head_id = $1 AND tenant_id = $2`, head.ID, head.TenantID); err != nil {
		return fmt.Errorf("bill repo: delete items on update: %w", err)
	}

	return r.insertItems(ctx, tx, items)
}

// UpdatePaidAmount sets bill_head.paid_amount within tx.
func (r *Repo) UpdatePaidAmount(ctx context.Context, tx *sql.Tx, tenantID, billID uuid.UUID, paidAmount decimal.Decimal) error {
	const q = `UPDATE tally.bill_head SET paid_amount = $1, updated_at = $2 WHERE id = $3 AND tenant_id = $4 AND deleted_at IS NULL`
	_, err := tx.ExecContext(ctx, q, paidAmount.String(), time.Now().UTC(), billID, tenantID)
	if err != nil {
		return fmt.Errorf("bill repo: update paid_amount: %w", err)
	}
	return nil
}

func (r *Repo) ListBills(ctx context.Context, f appbill.BillListFilter) ([]domain.BillHead, int64, error) {
	args := []any{f.TenantID}
	idx := 2
	where := []string{"tenant_id = $1", "deleted_at IS NULL"}

	if f.BillType != "" {
		where = append(where, fmt.Sprintf("bill_type = $%d", idx))
		args = append(args, string(f.BillType))
		idx++
	}
	if f.Status != nil {
		where = append(where, fmt.Sprintf("status = $%d", idx))
		args = append(args, int16(*f.Status))
		idx++
	}
	if f.PartnerID != nil {
		where = append(where, fmt.Sprintf("partner_id = $%d", idx))
		args = append(args, *f.PartnerID)
		idx++
	}
	if f.DateFrom != nil {
		where = append(where, fmt.Sprintf("bill_date >= $%d", idx))
		args = append(args, *f.DateFrom)
		idx++
	}
	if f.DateTo != nil {
		where = append(where, fmt.Sprintf("bill_date <= $%d", idx))
		args = append(args, *f.DateTo)
		idx++
	}

	page := f.Page
	if page <= 0 {
		page = 1
	}
	size := f.Size
	if size <= 0 {
		size = 20
	}
	offset := (page - 1) * size

	whereClause := strings.Join(where, " AND ")
	countQ := `SELECT COUNT(*) FROM tally.bill_head WHERE ` + whereClause
	var total int64
	if err := r.db.QueryRowContext(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("bill repo: list count: %w", err)
	}

	args = append(args, size, offset)
	q := `
		SELECT id, tenant_id, bill_no, bill_type, sub_type, status, partner_id, warehouse_id,
		       creator_id, bill_date, subtotal, shipping_fee, tax_amount, total_amount,
		       paid_amount, approved_at, approved_by, remark, created_at, updated_at
		FROM tally.bill_head
		WHERE ` + whereClause + `
		ORDER BY created_at DESC
		LIMIT $` + fmt.Sprintf("%d OFFSET $%d", idx, idx+1)

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("bill repo: list bills: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var bills []domain.BillHead
	for rows.Next() {
		var h domain.BillHead
		var billType, subType string
		var status int16
		var subtotal, shippingFee, taxAmount, totalAmount, paidAmount string
		if err := rows.Scan(
			&h.ID, &h.TenantID, &h.BillNo, &billType, &subType, &status,
			&h.PartnerID, &h.WarehouseID,
			&h.CreatorID, &h.BillDate,
			&subtotal, &shippingFee, &taxAmount, &totalAmount,
			&paidAmount,
			&h.ApprovedAt, &h.ApprovedBy,
			&h.Remark, &h.CreatedAt, &h.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("bill repo: list scan: %w", err)
		}
		h.BillType = domain.BillType(billType)
		h.SubType = domain.BillSubType(subType)
		h.Status = domain.BillStatus(status)
		h.Subtotal, _ = decimal.NewFromString(subtotal)
		h.ShippingFee, _ = decimal.NewFromString(shippingFee)
		h.TaxAmount, _ = decimal.NewFromString(taxAmount)
		h.TotalAmount, _ = decimal.NewFromString(totalAmount)
		h.PaidAmount, _ = decimal.NewFromString(paidAmount)
		bills = append(bills, h)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("bill repo: list bills rows: %w", err)
	}
	return bills, total, nil
}
