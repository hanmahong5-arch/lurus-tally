// Package payment implements PaymentRepo backed by PostgreSQL.
// All mutating operations require a *sql.Tx and respect RLS.
package payment

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	apppayment "github.com/hanmahong5-arch/lurus-tally/internal/app/payment"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/payment"
)

// Repo implements apppayment.PaymentRepo.
type Repo struct {
	db *sql.DB
}

// New creates a Repo.
func New(db *sql.DB) *Repo {
	return &Repo{db: db}
}

// Ensure Repo satisfies the interface at compile time.
var _ apppayment.PaymentRepo = (*Repo)(nil)

// ----- Transaction boundary -----

func (r *Repo) WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("payment repo: begin tx: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()
	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("payment repo: rollback (%v): %w", err, rbErr)
		}
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("payment repo: commit tx: %w", err)
	}
	return nil
}

// ----- payment_head CRUD -----

// Record inserts a payment_head row within the provided transaction.
// pay_date defaults to now() when p.PayDate is zero.
func (r *Repo) Record(ctx context.Context, tx *sql.Tx, p *domain.Payment) error {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	payDate := p.PayDate
	if payDate.IsZero() {
		payDate = time.Now().UTC()
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now().UTC()
	}

	const q = `
		INSERT INTO tally.payment_head
			(id, tenant_id, pay_type, partner_id, operator_id, creator_id,
			 bill_no, pay_date, amount, discount_amount, total_amount,
			 related_bill_id, remark, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`

	_, err := tx.ExecContext(ctx, q,
		p.ID, p.TenantID, string(p.PayType),
		p.PartnerID, p.OperatorID, p.CreatorID,
		nilStr(p.BillNo), payDate,
		p.Amount.String(), "0", p.Amount.String(), // discount=0, total=amount
		p.BillID, // related_bill_id
		nilStr(p.Remark),
		p.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("payment repo: record: %w", err)
	}
	return nil
}

// ListByBill returns all non-deleted payment_head rows for the given bill, newest first.
func (r *Repo) ListByBill(ctx context.Context, tenantID, billID uuid.UUID) ([]*domain.Payment, error) {
	const q = `
		SELECT id, tenant_id, related_bill_id, pay_type, amount, partner_id, operator_id,
		       creator_id, bill_no, pay_date, remark, created_at
		FROM tally.payment_head
		WHERE related_bill_id = $1 AND tenant_id = $2 AND deleted_at IS NULL
		ORDER BY pay_date ASC`

	rows, err := r.db.QueryContext(ctx, q, billID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("payment repo: list by bill: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var payments []*domain.Payment
	for rows.Next() {
		p, err := scanPayment(rows)
		if err != nil {
			return nil, fmt.Errorf("payment repo: scan: %w", err)
		}
		payments = append(payments, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("payment repo: list rows: %w", err)
	}
	return payments, nil
}

// SumByBill returns the sum of all payments for the bill, locking the rows for update.
func (r *Repo) SumByBill(ctx context.Context, tx *sql.Tx, tenantID, billID uuid.UUID) (decimal.Decimal, error) {
	const q = `
		SELECT COALESCE(SUM(amount), 0)
		FROM tally.payment_head
		WHERE related_bill_id = $1 AND tenant_id = $2 AND deleted_at IS NULL
		FOR UPDATE`

	var total string
	if err := tx.QueryRowContext(ctx, q, billID, tenantID).Scan(&total); err != nil {
		return decimal.Zero, fmt.Errorf("payment repo: sum by bill: %w", err)
	}
	d, err := decimal.NewFromString(total)
	if err != nil {
		return decimal.Zero, fmt.Errorf("payment repo: parse sum: %w", err)
	}
	return d, nil
}

// ----- helpers -----

func scanPayment(rows *sql.Rows) (*domain.Payment, error) {
	var p domain.Payment
	var payType string
	var amount string
	var billNo sql.NullString
	var remark sql.NullString

	err := rows.Scan(
		&p.ID, &p.TenantID, &p.BillID, &payType, &amount,
		&p.PartnerID, &p.OperatorID, &p.CreatorID,
		&billNo, &p.PayDate, &remark, &p.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	p.PayType = domain.PayType(payType)
	p.Amount, _ = decimal.NewFromString(amount)
	p.TotalAmount = p.Amount
	if billNo.Valid {
		p.BillNo = billNo.String
	}
	if remark.Valid {
		p.Remark = remark.String
	}
	return &p, nil
}

// nilStr returns nil if s is empty, otherwise a pointer to the string.
// Used to avoid inserting empty strings into optional VARCHAR columns.
func nilStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
