package payment

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

func (r *Repo) InsertNew(ctx context.Context, tenant uuid.UUID, channel, account string, lines []domainpayment.ParsedLine) (inserted, skipped int, err error) {
	tx, err := dbscope.BeginTx(ctx, r.db, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("money line: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	const q = `
		INSERT INTO tally.money_line
			(id, tenant_id, channel, account_ref, external_id, booked_at, amount, counterparty, memo, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'open')
		ON CONFLICT (tenant_id, channel, account_ref, external_id) DO NOTHING`
	for _, ln := range lines {
		res, err := tx.ExecContext(ctx, q, uuid.New(), tenant, channel, account, ln.ExternalID, ln.BookedAt, ln.Amount.StringFixed(2), ln.Counterparty, ln.Memo)
		if err != nil {
			return 0, 0, fmt.Errorf("money line: insert: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, 0, fmt.Errorf("money line: insert: %w", err)
		}
		if n == 0 {
			skipped++
		} else {
			inserted++
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("money line: commit: %w", err)
	}
	return inserted, skipped, nil
}

func (r *Repo) ListOpen(ctx context.Context, tenant uuid.UUID) ([]domainpayment.MoneyLine, error) {
	const q = `
		SELECT id, channel, account_ref, external_id, booked_at, amount::text, counterparty, memo, status, bill_id, payment_id
		FROM tally.money_line
		WHERE tenant_id = $1 AND status = 'open'
		ORDER BY booked_at DESC, id DESC
		LIMIT 200`
	rows, err := dbscope.From(ctx, r.db).QueryContext(ctx, q, tenant)
	if err != nil {
		return nil, fmt.Errorf("money line: list: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domainpayment.MoneyLine
	for rows.Next() {
		ln, err := scanMoneyLine(rows)
		if err != nil {
			return nil, fmt.Errorf("money line: list: %w", err)
		}
		out = append(out, ln)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("money line: list: %w", err)
	}
	return out, nil
}

func (r *Repo) Get(ctx context.Context, tenant, id uuid.UUID) (domainpayment.MoneyLine, error) {
	const q = `
		SELECT id, channel, account_ref, external_id, booked_at, amount::text, counterparty, memo, status, bill_id, payment_id
		FROM tally.money_line
		WHERE tenant_id = $1 AND id = $2`
	row := dbscope.From(ctx, r.db).QueryRowContext(ctx, q, tenant, id)
	ln, err := scanMoneyLine(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domainpayment.MoneyLine{}, domainpayment.ErrMoneyLine
	}
	if err != nil {
		return domainpayment.MoneyLine{}, fmt.Errorf("money line: get: %w", err)
	}
	return ln, nil
}

func (r *Repo) MarkMatched(ctx context.Context, tx *sql.Tx, tenant, id, bill, payment uuid.UUID) error {
	const q = `
		UPDATE tally.money_line
		SET status = 'matched', bill_id = $1, payment_id = $2
		WHERE id = $3 AND tenant_id = $4 AND status = 'open'`
	res, err := tx.ExecContext(ctx, q, bill, payment, id, tenant)
	if err != nil {
		return fmt.Errorf("money line: match: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("money line: match: %w", err)
	}
	if n == 0 {
		return domainpayment.ErrMoneyTaken
	}
	return nil
}

func (r *Repo) Ignore(ctx context.Context, tenant, id uuid.UUID) error {
	const q = `
		UPDATE tally.money_line SET status = 'ignored'
		WHERE id = $1 AND tenant_id = $2 AND status = 'open'`
	res, err := dbscope.From(ctx, r.db).ExecContext(ctx, q, id, tenant)
	if err != nil {
		return fmt.Errorf("money line: ignore: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("money line: ignore: %w", err)
	}
	if n == 1 {
		return nil
	}
	ln, err := r.Get(ctx, tenant, id)
	if err != nil {
		return err
	}
	if ln.Status == domainpayment.MoneyIgnored {
		return nil
	}
	return domainpayment.ErrMoneyClosed
}

type moneyScanner interface {
	Scan(dest ...any) error
}

func scanMoneyLine(sc moneyScanner) (domainpayment.MoneyLine, error) {
	var ln domainpayment.MoneyLine
	var amount string
	var billID, paymentID uuid.NullUUID
	err := sc.Scan(&ln.ID, &ln.Channel, &ln.AccountRef, &ln.ExternalID, &ln.BookedAt, &amount, &ln.Counterparty, &ln.Memo, &ln.Status, &billID, &paymentID)
	if err != nil {
		return domainpayment.MoneyLine{}, err
	}
	ln.Amount, err = decimalutil.Parse(amount, "amount")
	if err != nil {
		return domainpayment.MoneyLine{}, fmt.Errorf("money line: amount: %w", err)
	}
	if billID.Valid {
		ln.BillID = &billID.UUID
	}
	if paymentID.Valid {
		ln.PaymentID = &paymentID.UUID
	}
	return ln, nil
}
