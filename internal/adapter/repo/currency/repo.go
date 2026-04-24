// Package currency implements CurrencyRepo backed by PostgreSQL.
// currency table is global (no RLS). exchange_rate table has RLS via app.tenant_id.
// WHERE tenant_id = $n defensive filter is applied in all exchange_rate queries.
package currency

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appcurrency "github.com/hanmahong5-arch/lurus-tally/internal/app/currency"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/currency"
)

// Repo implements appcurrency.CurrencyRepo.
type Repo struct {
	db *sql.DB
}

// New creates a Repo.
func New(db *sql.DB) *Repo {
	return &Repo{db: db}
}

// Ensure Repo satisfies the interface at compile time.
var _ appcurrency.CurrencyRepo = (*Repo)(nil)

// ListCurrencies returns all enabled currencies ordered by code.
func (r *Repo) ListCurrencies(ctx context.Context) ([]domain.Currency, error) {
	const q = `
		SELECT code, name, symbol, enabled
		FROM tally.currency
		WHERE enabled = true
		ORDER BY code`

	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("currency repo: list currencies: %w", err)
	}
	defer rows.Close()

	var result []domain.Currency
	for rows.Next() {
		var c domain.Currency
		if err := rows.Scan(&c.Code, &c.Name, &c.Symbol, &c.Enabled); err != nil {
			return nil, fmt.Errorf("currency repo: scan currency: %w", err)
		}
		result = append(result, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("currency repo: list currencies rows: %w", err)
	}
	return result, nil
}

// GetRateOn returns the most recent exchange rate where effective_at <= date.
// Returns nil, nil when no matching row is found.
func (r *Repo) GetRateOn(ctx context.Context, tenantID uuid.UUID, from, to string, date time.Time) (*domain.ExchangeRate, error) {
	const q = `
		SELECT id, tenant_id, from_currency, to_currency, rate, source, effective_at, created_at
		FROM tally.exchange_rate
		WHERE tenant_id = $1
		  AND from_currency = $2
		  AND to_currency = $3
		  AND effective_at <= $4
		ORDER BY effective_at DESC
		LIMIT 1`

	row := r.db.QueryRowContext(ctx, q, tenantID, from, to, date)
	er, err := scanExchangeRate(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("currency repo: get rate on %s: %w", date.Format("2006-01-02"), err)
	}
	return er, nil
}

// SaveRate upserts an exchange rate (ON CONFLICT by tenant/from/to/effective_at updates rate+source).
func (r *Repo) SaveRate(ctx context.Context, er *domain.ExchangeRate) error {
	const q = `
		INSERT INTO tally.exchange_rate
			(id, tenant_id, from_currency, to_currency, rate, source, effective_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (tenant_id, from_currency, to_currency, effective_at)
		DO UPDATE SET rate = EXCLUDED.rate, source = EXCLUDED.source`

	_, err := r.db.ExecContext(ctx, q,
		er.ID, er.TenantID, er.FromCurrency, er.ToCurrency,
		er.Rate.String(), er.Source, er.EffectiveAt, er.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("currency repo: save rate: %w", err)
	}
	return nil
}

// ListRateHistory returns rates ordered by effective_at ASC covering the last `days` days.
func (r *Repo) ListRateHistory(ctx context.Context, tenantID uuid.UUID, from, to string, days int) ([]domain.ExchangeRate, error) {
	const q = `
		SELECT id, tenant_id, from_currency, to_currency, rate, source, effective_at, created_at
		FROM tally.exchange_rate
		WHERE tenant_id = $1
		  AND from_currency = $2
		  AND to_currency = $3
		  AND effective_at >= now() - ($4::int * interval '1 day')
		ORDER BY effective_at ASC`

	rows, err := r.db.QueryContext(ctx, q, tenantID, from, to, days)
	if err != nil {
		return nil, fmt.Errorf("currency repo: list rate history: %w", err)
	}
	defer rows.Close()

	var result []domain.ExchangeRate
	for rows.Next() {
		er, err := scanExchangeRateRow(rows)
		if err != nil {
			return nil, fmt.Errorf("currency repo: scan rate history: %w", err)
		}
		result = append(result, *er)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("currency repo: list rate history rows: %w", err)
	}
	return result, nil
}

// ----- helpers -----

type rowScanner interface {
	Scan(dest ...any) error
}

func scanExchangeRate(row rowScanner) (*domain.ExchangeRate, error) {
	var er domain.ExchangeRate
	var rateStr string
	err := row.Scan(
		&er.ID, &er.TenantID, &er.FromCurrency, &er.ToCurrency,
		&rateStr, &er.Source, &er.EffectiveAt, &er.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	rate, err := decimal.NewFromString(rateStr)
	if err != nil {
		return nil, fmt.Errorf("parse rate %q: %w", rateStr, err)
	}
	er.Rate = rate
	return &er, nil
}

func scanExchangeRateRow(rows *sql.Rows) (*domain.ExchangeRate, error) {
	var er domain.ExchangeRate
	var rateStr string
	err := rows.Scan(
		&er.ID, &er.TenantID, &er.FromCurrency, &er.ToCurrency,
		&rateStr, &er.Source, &er.EffectiveAt, &er.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	rate, err := decimal.NewFromString(rateStr)
	if err != nil {
		return nil, fmt.Errorf("parse rate %q: %w", rateStr, err)
	}
	er.Rate = rate
	return &er, nil
}
