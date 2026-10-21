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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dbscope"
	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/decimalutil"
)

// clientRefConstraint is the unique index (migration 000060) behind checkout idempotency.
const clientRefConstraint = "bill_head_tenant_client_ref_uidx"

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
	// Begin on the tenant-pinned connection when the request pinned one, so the
	// tx inherits app.tenant_id and the RLS policies bind every write in the
	// approval flow (bill_head/item, stock_movement/snapshot/lot, outbox), which
	// all share this tx. Falls back to the pool when nothing is pinned
	// (background callers, tests) — behaviour is then unchanged.
	tx, err := dbscope.BeginTx(ctx, r.db, nil)
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

// billNoZone is the shop's wall clock: bill numbers and date filters share it so a
// bill opened at 00:30 Beijing time carries that day's date, not the previous UTC day.
var billNoZone = time.FixedZone("CST", 8*3600)

// BillNoDay returns the YYYYMMDD part of a bill number for the instant t.
func BillNoDay(t time.Time) string {
	return t.In(billNoZone).Format("20060102")
}

// NextBillNo generates the next bill number for the (tenantID, prefix, today) combination.
// Uses the existing tally.bill_sequence table (from migration 000010).
// Schema: (id UUID PK, tenant_id UUID, prefix VARCHAR(20), current_val BIGINT, UNIQUE(tenant_id, prefix)).
//
// We use a day-keyed prefix (prefix+date) stored as the sequence prefix so per-day reset works.
// Format: "PO-20260423-0001"
func (r *Repo) NextBillNo(ctx context.Context, tx *sql.Tx, tenantID uuid.UUID, prefix string) (string, error) {
	today := BillNoDay(time.Now())
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
		row = dbscope.From(ctx, r.db).QueryRowContext(ctx, q, uuid.New(), tenantID, seqPrefix)
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
			 created_at, updated_at, currency, exchange_rate, amount_local,
			 client_ref, source, occurred_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,
			NULLIF($21,''), COALESCE(NULLIF($22,''), 'web'), $23)`

	// Multi-currency columns (migration 000024). Persist the rate the bill was booked
	// at so the approval path can convert foreign-currency unit prices to base currency;
	// before this, CreateBill omitted these columns and a DB-loaded head always carried
	// the column default (rate 1), making the approval-time FX conversion inert. Default
	// to domestic when a caller leaves the fields unset so the row still satisfies the
	// currency FK and a positive rate.
	currency := head.Currency
	if currency == "" {
		currency = "CNY"
	}
	exchangeRate := head.ExchangeRateVal
	if exchangeRate.IsZero() || exchangeRate.IsNegative() {
		exchangeRate = decimal.NewFromInt(1)
	}
	amountLocal := head.AmountLocal
	if amountLocal.IsZero() {
		amountLocal = head.TotalAmount
	}

	_, err := tx.ExecContext(ctx, headQ,
		head.ID, head.TenantID, head.BillNo, string(head.BillType), string(head.SubType),
		int16(head.Status), head.PartnerID, head.WarehouseID,
		head.CreatorID, head.BillDate,
		head.Subtotal.String(), head.ShippingFee.String(), head.TaxAmount.String(), head.TotalAmount.String(),
		head.Remark, head.CreatedAt, head.UpdatedAt,
		currency, exchangeRate.String(), amountLocal.String(),
		head.ClientRef, head.Source, head.OccurredAt,
	)
	if err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == clientRefConstraint {
			return fmt.Errorf("bill repo: create head: %w", appbill.ErrDuplicateClientRef)
		}
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
		       paid_amount, approved_at, approved_by, revision, remark, created_at, updated_at,
		       COALESCE(currency, 'CNY'), exchange_rate, COALESCE(amount_local, total_amount),
			       occurred_at
		FROM tally.bill_head
		WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL
		FOR UPDATE`

	return scanBillHead(tx.QueryRowContext(ctx, q, billID, tenantID))
}

func (r *Repo) GetBill(ctx context.Context, tenantID, billID uuid.UUID) (*domain.BillHead, error) {
	const q = `
		SELECT id, tenant_id, bill_no, bill_type, sub_type, status, partner_id, warehouse_id,
		       creator_id, bill_date, subtotal, shipping_fee, tax_amount, total_amount,
		       paid_amount, approved_at, approved_by, revision, remark, created_at, updated_at,
		       COALESCE(currency, 'CNY'), exchange_rate, COALESCE(amount_local, total_amount),
			       occurred_at
		FROM tally.bill_head
		WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL`

	h, err := scanBillHead(dbscope.From(ctx, r.db).QueryRowContext(ctx, q, billID, tenantID))
	if err != nil {
		return nil, err
	}
	// Display names are best-effort: a soft-deleted reference keeps its original name; a dangling one leaves it empty.
	if h.PartnerID != nil {
		const pq = `SELECT name FROM tally.partner WHERE id = $1 AND tenant_id = $2`
		if err := dbscope.From(ctx, r.db).QueryRowContext(ctx, pq, *h.PartnerID, tenantID).Scan(&h.PartnerName); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("bill repo: get partner name: %w", err)
		}
	}
	if h.WarehouseID != nil {
		const wq = `SELECT name FROM tally.warehouse WHERE id = $1 AND tenant_id = $2`
		if err := dbscope.From(ctx, r.db).QueryRowContext(ctx, wq, *h.WarehouseID, tenantID).Scan(&h.WarehouseName); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("bill repo: get warehouse name: %w", err)
		}
	}
	return h, nil
}

// ProductExists reports whether productID is an active (non-deleted) product
// owned by tenantID. See appbill.BillRepo for the cross-tenant rationale.
func (r *Repo) ProductExists(ctx context.Context, tenantID, productID uuid.UUID) (bool, error) {
	const q = `SELECT EXISTS (
		SELECT 1 FROM tally.product
		WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL)`
	var exists bool
	if err := dbscope.From(ctx, r.db).QueryRowContext(ctx, q, productID, tenantID).Scan(&exists); err != nil {
		return false, fmt.Errorf("bill repo product exists: %w", err)
	}
	return exists, nil
}

// WarehouseExists reports whether warehouseID is an active (non-deleted)
// warehouse owned by tenantID.
func (r *Repo) WarehouseExists(ctx context.Context, tenantID, warehouseID uuid.UUID) (bool, error) {
	const q = `SELECT EXISTS (
		SELECT 1 FROM tally.warehouse
		WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL)`
	var exists bool
	if err := dbscope.From(ctx, r.db).QueryRowContext(ctx, q, warehouseID, tenantID).Scan(&exists); err != nil {
		return false, fmt.Errorf("bill repo warehouse exists: %w", err)
	}
	return exists, nil
}

// PartnerExists reports whether id is an active (non-deleted) partner owned by
// tenantID.
func (r *Repo) PartnerExists(ctx context.Context, tenantID, id uuid.UUID) (bool, error) {
	const q = `SELECT EXISTS (
		SELECT 1 FROM tally.partner
		WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL)`
	var exists bool
	if err := dbscope.From(ctx, r.db).QueryRowContext(ctx, q, id, tenantID).Scan(&exists); err != nil {
		return false, fmt.Errorf("bill repo partner exists: %w", err)
	}
	return exists, nil
}

// DefaultWarehouseID returns the tenant's active default warehouse; with no default
// flagged it returns the sole active warehouse. false when neither exists.
func (r *Repo) DefaultWarehouseID(ctx context.Context, tenantID uuid.UUID) (uuid.UUID, bool, error) {
	const q = `SELECT id, COALESCE(is_default, false) FROM tally.warehouse
		WHERE tenant_id = $1 AND deleted_at IS NULL AND COALESCE(enabled, true)
		ORDER BY COALESCE(is_default, false) DESC, created_at ASC
		LIMIT 2`
	rows, err := dbscope.From(ctx, r.db).QueryContext(ctx, q, tenantID)
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("bill repo default warehouse: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var ids []uuid.UUID
	var firstDefault bool
	for rows.Next() {
		var id uuid.UUID
		var isDefault bool
		if err := rows.Scan(&id, &isDefault); err != nil {
			return uuid.Nil, false, fmt.Errorf("bill repo default warehouse scan: %w", err)
		}
		if len(ids) == 0 {
			firstDefault = isDefault
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return uuid.Nil, false, fmt.Errorf("bill repo default warehouse rows: %w", err)
	}
	if len(ids) == 0 || (!firstDefault && len(ids) > 1) {
		return uuid.Nil, false, nil
	}
	return ids[0], true, nil
}

// SetBillWarehouse writes bill_head.warehouse_id within tx.
func (r *Repo) SetBillWarehouse(ctx context.Context, tx *sql.Tx, tenantID, billID, warehouseID uuid.UUID) error {
	const q = `UPDATE tally.bill_head SET warehouse_id = $1, updated_at = $2
		WHERE id = $3 AND tenant_id = $4 AND deleted_at IS NULL`
	res, err := tx.ExecContext(ctx, q, warehouseID, time.Now().UTC(), billID, tenantID)
	if err != nil {
		return fmt.Errorf("bill repo set warehouse: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("bill repo set warehouse rows: %w", err)
	} else if n == 0 {
		return appbill.ErrBillNotFound
	}
	return nil
}

func scanBillHead(row *sql.Row) (*domain.BillHead, error) {
	var h domain.BillHead
	var billType, subType string
	var status int16
	var subtotal, shippingFee, taxAmount, totalAmount, paidAmount, exchangeRate, amountLocal string
	err := row.Scan(
		&h.ID, &h.TenantID, &h.BillNo, &billType, &subType, &status,
		&h.PartnerID, &h.WarehouseID,
		&h.CreatorID, &h.BillDate,
		&subtotal, &shippingFee, &taxAmount, &totalAmount,
		&paidAmount,
		&h.ApprovedAt, &h.ApprovedBy,
		&h.Revision,
		&h.Remark, &h.CreatedAt, &h.UpdatedAt,
		&h.Currency, &exchangeRate, &amountLocal,
		&h.OccurredAt,
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
	if h.Subtotal, err = decimalutil.Parse(subtotal, "subtotal"); err != nil {
		return nil, fmt.Errorf("bill repo: scan head: %w", err)
	}
	if h.ShippingFee, err = decimalutil.Parse(shippingFee, "shipping_fee"); err != nil {
		return nil, fmt.Errorf("bill repo: scan head: %w", err)
	}
	if h.TaxAmount, err = decimalutil.Parse(taxAmount, "tax_amount"); err != nil {
		return nil, fmt.Errorf("bill repo: scan head: %w", err)
	}
	if h.TotalAmount, err = decimalutil.Parse(totalAmount, "total_amount"); err != nil {
		return nil, fmt.Errorf("bill repo: scan head: %w", err)
	}
	if h.PaidAmount, err = decimalutil.Parse(paidAmount, "paid_amount"); err != nil {
		return nil, fmt.Errorf("bill repo: scan head: %w", err)
	}
	if h.ExchangeRateVal, err = decimalutil.Parse(exchangeRate, "exchange_rate"); err != nil {
		return nil, fmt.Errorf("bill repo: scan head: %w", err)
	}
	if h.AmountLocal, err = decimalutil.Parse(amountLocal, "amount_local"); err != nil {
		return nil, fmt.Errorf("bill repo: scan head: %w", err)
	}
	return &h, nil
}

func (r *Repo) GetBillItems(ctx context.Context, tenantID, billID uuid.UUID) ([]*domain.BillItem, error) {
	const q = `
		SELECT id, tenant_id, head_id, product_id, unit_id, unit_name, line_no, qty, unit_price, line_amount, remark
		FROM tally.bill_item
		WHERE head_id = $1 AND tenant_id = $2
		ORDER BY line_no ASC`

	rows, err := dbscope.From(ctx, r.db).QueryContext(ctx, q, billID, tenantID)
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
		if it.Qty, err = decimalutil.Parse(qty, "qty"); err != nil {
			return nil, fmt.Errorf("bill repo: scan item: %w", err)
		}
		if it.UnitPrice, err = decimalutil.Parse(unitPrice, "unit_price"); err != nil {
			return nil, fmt.Errorf("bill repo: scan item: %w", err)
		}
		if it.LineAmount, err = decimalutil.Parse(lineAmount, "line_amount"); err != nil {
			return nil, fmt.Errorf("bill repo: scan item: %w", err)
		}
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
		if rev, ok := meta["revision"]; ok {
			q += fmt.Sprintf(", revision = $%d", len(args)+1)
			args = append(args, rev.(int))
		}
	}

	// billID and tenantID are always args[2]/args[3] -> $3/$4 (appended before any
	// optional meta SET clauses, which take $5+). Using fixed positions here is
	// essential: computing them from len(args) after appending meta made the WHERE
	// reuse the meta placeholders ($5/$6), so a single $5 was bound as both
	// approved_at (timestamptz) and id (uuid) -> SQLSTATE 42804, breaking every
	// successful approval. (Caught by the non-superuser app-boot e2e.)
	q += " WHERE id = $3 AND tenant_id = $4 AND deleted_at IS NULL"

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
			updated_at   = $9,
			amount_local = $12
		WHERE id = $10 AND tenant_id = $11 AND deleted_at IS NULL`

	amountLocal := head.AmountLocal
	if amountLocal.IsZero() {
		amountLocal = head.TotalAmount
	}
	_, err := tx.ExecContext(ctx, headQ,
		head.PartnerID, head.WarehouseID, head.BillDate,
		head.Subtotal.String(), head.ShippingFee.String(), head.TaxAmount.String(), head.TotalAmount.String(),
		head.Remark, head.UpdatedAt, head.ID, head.TenantID, amountLocal.String(),
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

// UpdateItemPurchasePrice sets bill_item.purchase_price for a single line within tx.
// Sale approval calls this with the WAC/FIFO cost computed by the stock-out movement so
// margin reports compare sale price against the real cost of goods sold instead of zero.
// purchasePrice is expressed in base currency, matching the cost basis of stock movements.
func (r *Repo) UpdateItemPurchasePrice(ctx context.Context, tx *sql.Tx, tenantID, itemID uuid.UUID, purchasePrice decimal.Decimal) error {
	const q = `UPDATE tally.bill_item SET purchase_price = $1 WHERE id = $2 AND tenant_id = $3 AND deleted_at IS NULL`
	_, err := tx.ExecContext(ctx, q, purchasePrice.String(), itemID, tenantID)
	if err != nil {
		return fmt.Errorf("bill repo: update item purchase_price: %w", err)
	}
	return nil
}

// escapeLike escapes LIKE metacharacters so user input matches literally
// (used with ESCAPE '\').
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// invoicedExpr is the amount of a bill covered by live invoices: links on issued,
// non-deleted invoices only (void and red-struck invoices do not count).
const invoicedExpr = `COALESCE((SELECT SUM(ib.amount) FROM tally.invoice_bill ib
		JOIN tally.invoice iv ON iv.id = ib.invoice_id
		WHERE ib.bill_id = b.id AND iv.status = 'issued' AND iv.deleted_at IS NULL), 0)`

// buildListWhere renders the shared WHERE clause (aliases: b = bill_head, p = partner)
// and its positional args for ListBills and SummarizeBills. The returned idx is the
// next free placeholder number.
func buildListWhere(f appbill.BillListFilter) (string, []any, int) {
	args := []any{f.TenantID}
	idx := 2
	where := []string{"b.tenant_id = $1", "b.deleted_at IS NULL"}

	if f.BillType != "" {
		where = append(where, fmt.Sprintf("b.bill_type = $%d", idx))
		args = append(args, string(f.BillType))
		idx++
	}
	if f.SubType != "" {
		where = append(where, fmt.Sprintf("b.sub_type = $%d", idx))
		args = append(args, string(f.SubType))
		idx++
	}
	status := f.Status
	if f.PaymentStatus != "" || f.InvoiceStatus != "" {
		// Payment state only exists for approved bills.
		approved := domain.StatusApproved
		status = &approved
	}
	if f.OwnRecentApprovedSince != nil {
		where = append(where, fmt.Sprintf("(b.status = %d OR (b.status = %d AND b.approved_at >= $%d))", int(domain.StatusDraft), int(domain.StatusApproved), idx))
		args = append(args, *f.OwnRecentApprovedSince)
		idx++
		status = nil
	}
	if status != nil {
		where = append(where, fmt.Sprintf("b.status = $%d", idx))
		args = append(args, int16(*status))
		idx++
	}
	switch f.PaymentStatus {
	case domain.PaymentUnpaid:
		where = append(where, "b.paid_amount <= 0")
	case domain.PaymentPartial:
		where = append(where, "b.paid_amount > 0 AND b.paid_amount < "+netTotalExpr(f))
	case domain.PaymentPaid:
		where = append(where, "b.paid_amount > 0 AND b.paid_amount >= "+netTotalExpr(f))
	case "open": // net of sales returns on the sale side, same rule as the receivables summary
		where = append(where, netTotalExpr(f)+" > b.paid_amount")
	}
	switch f.InvoiceStatus {
	case domain.InvoiceNone:
		where = append(where, invoicedExpr+" <= 0")
	case domain.InvoicePartial:
		where = append(where, invoicedExpr+" > 0 AND "+invoicedExpr+" < TRUNC(b.total_amount, 2)")
	case domain.InvoiceFull:
		where = append(where, invoicedExpr+" > 0 AND "+invoicedExpr+" >= TRUNC(b.total_amount, 2)")
	}
	if f.PartnerID != nil {
		where = append(where, fmt.Sprintf("b.partner_id = $%d", idx))
		args = append(args, *f.PartnerID)
		idx++
	}
	if f.CreatorID != nil {
		where = append(where, fmt.Sprintf("b.creator_id = $%d", idx))
		args = append(args, *f.CreatorID)
		idx++
	}
	if f.DateFrom != nil {
		where = append(where, fmt.Sprintf("b.bill_date >= $%d", idx))
		args = append(args, *f.DateFrom)
		idx++
	}
	if f.DateTo != nil {
		// DateTo is an exclusive upper bound.
		where = append(where, fmt.Sprintf("b.bill_date < $%d", idx))
		args = append(args, *f.DateTo)
		idx++
	}
	if f.Keyword != "" {
		where = append(where, fmt.Sprintf(`(b.bill_no ILIKE $%d ESCAPE '\' OR p.name ILIKE $%d ESCAPE '\')`, idx, idx))
		args = append(args, "%"+escapeLike(f.Keyword)+"%")
		idx++
	}
	return strings.Join(where, " AND "), args, idx
}

const listFrom = ` FROM tally.bill_head b
		LEFT JOIN tally.partner p ON p.id = b.partner_id AND p.tenant_id = b.tenant_id`

// netSales reports whether the list is the sale side, where sales returns reduce what
// is owed (same rule as the receivables summary and the sale detail).
func netSales(f appbill.BillListFilter) bool { return f.BillType == domain.BillTypeSale }

// netTotalExpr is the bill total after sales returns (floored at zero) on the sale side,
// the plain total elsewhere. It relies on the rsum join from listCTE / listFrom.
func netTotalExpr(f appbill.BillListFilter) string {
	if netSales(f) {
		return "GREATEST(b.total_amount - COALESCE(r.amt, 0), 0)"
	}
	return "b.total_amount"
}

// unpaidExpr is what is still owed on one bill: net of returns, less paid, floored at zero
// and rounded to cents per bill. It is the ageing's rule, so the summary strip and the
// receivables panel add up to the same figure and an overpaid bill never goes negative.
func unpaidExpr(f appbill.BillListFilter) string {
	return "ROUND(GREATEST(" + netTotalExpr(f) + " - b.paid_amount, 0), 2)"
}

// listCTE is the WITH prefix of the list queries: the per-sale returns on the sale side.
func listCTE(f appbill.BillListFilter) string {
	if netSales(f) {
		return "WITH" + saleReturnsCTE("")
	}
	return ""
}

// listFromFor is listFrom plus the returns join on the sale side.
func listFromFor(f appbill.BillListFilter) string {
	if netSales(f) {
		return listFrom + ` LEFT JOIN rsum r ON r.sale_id = b.id`
	}
	return listFrom
}

// returnedExpr is the returns total for a bill (zero off the sale side).
func returnedExpr(f appbill.BillListFilter) string {
	if netSales(f) {
		return "COALESCE(r.amt, 0)"
	}
	return "0"
}

func (r *Repo) ListBills(ctx context.Context, f appbill.BillListFilter) ([]domain.BillHead, int64, error) {
	whereClause, args, idx := buildListWhere(f)

	page := f.Page
	if page <= 0 {
		page = 1
	}
	size := f.Size
	if size <= 0 {
		size = 20
	}
	offset := (page - 1) * size

	dbh := dbscope.From(ctx, r.db)
	countQ := listCTE(f) + `SELECT COUNT(*)` + listFromFor(f) + ` WHERE ` + whereClause
	var total int64
	if err := dbh.QueryRowContext(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("bill repo: list count: %w", err)
	}

	args = append(args, size, offset)
	q := listCTE(f) + `
		SELECT b.id, b.tenant_id, b.bill_no, b.bill_type, b.sub_type, b.status, b.partner_id, b.warehouse_id,
		       b.creator_id, b.bill_date, b.subtotal, b.shipping_fee, b.tax_amount, b.total_amount,
		       b.paid_amount, b.approved_at, b.approved_by, b.revision, b.remark, b.created_at, b.updated_at,
		       b.exchange_rate, COALESCE(p.name, ''), ` + invoicedExpr + `::text, ` + returnedExpr(f) + `::text` + listFromFor(f) + `
		WHERE ` + whereClause + `
		ORDER BY ` + listOrder(f) + `
		LIMIT $` + fmt.Sprintf("%d OFFSET $%d", idx, idx+1)

	rows, err := dbh.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("bill repo: list bills: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var bills []domain.BillHead
	for rows.Next() {
		var h domain.BillHead
		var billType, subType string
		var status int16
		var subtotal, shippingFee, taxAmount, totalAmount, paidAmount, exchangeRate, invoiced, returned string
		if err := rows.Scan(
			&h.ID, &h.TenantID, &h.BillNo, &billType, &subType, &status,
			&h.PartnerID, &h.WarehouseID,
			&h.CreatorID, &h.BillDate,
			&subtotal, &shippingFee, &taxAmount, &totalAmount,
			&paidAmount,
			&h.ApprovedAt, &h.ApprovedBy,
			&h.Revision,
			&h.Remark, &h.CreatedAt, &h.UpdatedAt,
			&exchangeRate, &h.PartnerName, &invoiced, &returned,
		); err != nil {
			return nil, 0, fmt.Errorf("bill repo: list scan: %w", err)
		}
		h.BillType = domain.BillType(billType)
		h.SubType = domain.BillSubType(subType)
		h.Status = domain.BillStatus(status)
		if h.Subtotal, err = decimalutil.Parse(subtotal, "subtotal"); err != nil {
			return nil, 0, fmt.Errorf("bill repo: list scan: %w", err)
		}
		if h.ShippingFee, err = decimalutil.Parse(shippingFee, "shipping_fee"); err != nil {
			return nil, 0, fmt.Errorf("bill repo: list scan: %w", err)
		}
		if h.TaxAmount, err = decimalutil.Parse(taxAmount, "tax_amount"); err != nil {
			return nil, 0, fmt.Errorf("bill repo: list scan: %w", err)
		}
		if h.TotalAmount, err = decimalutil.Parse(totalAmount, "total_amount"); err != nil {
			return nil, 0, fmt.Errorf("bill repo: list scan: %w", err)
		}
		if h.PaidAmount, err = decimalutil.Parse(paidAmount, "paid_amount"); err != nil {
			return nil, 0, fmt.Errorf("bill repo: list scan: %w", err)
		}
		if h.ExchangeRateVal, err = decimalutil.Parse(exchangeRate, "exchange_rate"); err != nil {
			return nil, 0, fmt.Errorf("bill repo: list scan: %w", err)
		}
		if h.InvoicedAmount, err = decimalutil.Parse(invoiced, "invoiced_amount"); err != nil {
			return nil, 0, fmt.Errorf("bill repo: list scan: %w", err)
		}
		if h.ReturnedAmount, err = decimalutil.Parse(returned, "returned_amount"); err != nil {
			return nil, 0, fmt.Errorf("bill repo: list scan: %w", err)
		}
		bills = append(bills, h)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("bill repo: list bills rows: %w", err)
	}
	return bills, total, nil
}

// listOrder is the ORDER BY of ListBills: newest first, or oldest first for the
// "who has owed longest" view.
func listOrder(f appbill.BillListFilter) string {
	if f.OldestFirst {
		return "b.bill_date ASC, b.created_at ASC"
	}
	return "b.bill_date DESC, b.created_at DESC"
}

// SummarizeBills aggregates the bills matching f with the same WHERE as ListBills.
// count, total, paid and unpaid all cover approved bills only. On the sale side returned
// is what sales returns take off the totals (never more than a bill's total), so
// total - returned - paid = unpaid.
func (r *Repo) SummarizeBills(ctx context.Context, f appbill.BillListFilter) (appbill.BillListSummary, error) {
	whereClause, args, _ := buildListWhere(f)
	q := listCTE(f) + `
		SELECT COUNT(*) FILTER (WHERE b.status = 2),
		       COALESCE(SUM(b.total_amount) FILTER (WHERE b.status = 2), 0)::text,
		       COALESCE(SUM(b.paid_amount) FILTER (WHERE b.status = 2), 0)::text,
		       COALESCE(SUM(b.total_amount - ` + netTotalExpr(f) + `) FILTER (WHERE b.status = 2), 0)::text,
		       COALESCE(SUM(` + unpaidExpr(f) + `) FILTER (WHERE b.status = 2), 0)::text` +
		listFromFor(f) + ` WHERE ` + whereClause

	var s appbill.BillListSummary
	var total, paid, returned, unpaid string
	if err := dbscope.From(ctx, r.db).QueryRowContext(ctx, q, args...).Scan(&s.Count, &total, &paid, &returned, &unpaid); err != nil {
		return s, fmt.Errorf("bill repo: summarize: %w", err)
	}
	var err error
	if s.TotalAmount, err = decimalutil.Parse(total, "total_amount"); err != nil {
		return s, fmt.Errorf("bill repo: summarize: %w", err)
	}
	if s.PaidAmount, err = decimalutil.Parse(paid, "paid_amount"); err != nil {
		return s, fmt.Errorf("bill repo: summarize: %w", err)
	}
	if s.ReturnedAmount, err = decimalutil.Parse(returned, "returned_amount"); err != nil {
		return s, fmt.Errorf("bill repo: summarize: %w", err)
	}
	if s.UnpaidAmount, err = decimalutil.Parse(unpaid, "unpaid_amount"); err != nil {
		return s, fmt.Errorf("bill repo: summarize: %w", err)
	}
	return s, nil
}

// FindBillByClientRef returns the bill booked under (tenant, client_ref), if any.
// Voided / deleted bills count too: the unique index ignores deleted_at.
func (r *Repo) FindBillByClientRef(ctx context.Context, tenantID uuid.UUID, clientRef string) (uuid.UUID, string, bool, error) {
	const q = `SELECT id, bill_no FROM tally.bill_head WHERE tenant_id = $1 AND client_ref = $2`
	var (
		id     uuid.UUID
		billNo string
	)
	err := dbscope.From(ctx, r.db).QueryRowContext(ctx, q, tenantID, clientRef).Scan(&id, &billNo)
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, "", false, nil
	}
	if err != nil {
		return uuid.Nil, "", false, fmt.Errorf("bill repo: find by client_ref: %w", err)
	}
	return id, billNo, true, nil
}

// RecordNegativeStock leaves an operation-log row on the offline sale that took a product
// below zero, worded as the to-do the shop should act on ("count it"). The to-do list is
// derived from live data and has no table of its own, so the operation log carries the note.
func (r *Repo) RecordNegativeStock(ctx context.Context, tx *sql.Tx, tenantID, billID, userID, productID uuid.UUID, onHandAfter decimal.Decimal) error {
	const q = `
		INSERT INTO tally.audit_log (tenant_id, user_id, action, resource, resource_id, changes)
		SELECT $1, $2, 'stock.negative', 'sale_bill', $3,
		       jsonb_build_object('reason', '离线销售导致负库存，请盘点：' || p.name || '（当前 ' || $5::text || '）',
		                          'product_id', p.id::text)
		FROM tally.product p
		WHERE p.id = $4 AND p.tenant_id = $1`
	var uid any // NULL = system
	if userID != uuid.Nil {
		uid = userID
	}
	if _, err := tx.ExecContext(ctx, q, tenantID, uid, billID, productID, onHandAfter.String()); err != nil {
		return fmt.Errorf("bill repo: record negative stock: %w", err)
	}
	return nil
}
