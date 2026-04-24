// Package currency contains use cases for multi-currency and exchange rate management.
// CurrencyRepo is the persistence interface; the PG implementation lives in adapter/repo/currency.
package currency

import (
	"context"
	"time"

	"github.com/google/uuid"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/currency"
)

// CurrencyRepo is the persistence interface for currency and exchange_rate operations.
type CurrencyRepo interface {
	// ListCurrencies returns all enabled currencies ordered by code.
	ListCurrencies(ctx context.Context) ([]domain.Currency, error)

	// GetRateOn returns the most recent exchange rate where effective_at <= date.
	// Returns nil, nil when no rate is found.
	GetRateOn(ctx context.Context, tenantID uuid.UUID, from, to string, date time.Time) (*domain.ExchangeRate, error)

	// SaveRate upserts an exchange rate (ON CONFLICT by tenant/from/to/effective_at).
	SaveRate(ctx context.Context, r *domain.ExchangeRate) error

	// ListRateHistory returns rates for the given pair, ordered by effective_at ASC,
	// covering the last `days` days.
	ListRateHistory(ctx context.Context, tenantID uuid.UUID, from, to string, days int) ([]domain.ExchangeRate, error)
}
