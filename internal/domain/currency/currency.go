// Package currency contains domain entities for multi-currency and exchange rate support.
package currency

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Source constants for exchange rate origin.
const (
	SourceManual = "manual"
	SourcePBoC   = "pboc"
	SourceAPI    = "exchangerate_api"
)

// ErrValidation is the sentinel for domain validation failures.
var ErrValidation = errors.New("currency: validation error")

// Currency maps to the tally.currency table.
type Currency struct {
	Code    string `json:"code"`
	Name    string `json:"name"`
	Symbol  string `json:"symbol"`
	Enabled bool   `json:"enabled"`
}

// Validate returns an error if the currency is missing required fields.
func (c *Currency) Validate() error {
	if c.Code == "" {
		return fmt.Errorf("%w: code is required", ErrValidation)
	}
	if c.Name == "" {
		return fmt.Errorf("%w: name is required", ErrValidation)
	}
	return nil
}

// ExchangeRate maps to the tally.exchange_rate table.
type ExchangeRate struct {
	ID           uuid.UUID       `json:"id"`
	TenantID     uuid.UUID       `json:"tenant_id"`
	FromCurrency string          `json:"from_currency"`
	ToCurrency   string          `json:"to_currency"`
	Rate         decimal.Decimal `json:"rate"`
	Source       string          `json:"source"`
	EffectiveAt  time.Time       `json:"effective_at"`
	CreatedAt    time.Time       `json:"created_at"`
}

// Validate returns an error if the exchange rate is invalid.
func (r *ExchangeRate) Validate() error {
	if r.Rate.IsNegative() || r.Rate.IsZero() {
		return fmt.Errorf("%w: rate must be positive, got %s", ErrValidation, r.Rate)
	}
	if r.FromCurrency == "" {
		return fmt.Errorf("%w: from_currency is required", ErrValidation)
	}
	if r.ToCurrency == "" {
		return fmt.Errorf("%w: to_currency is required", ErrValidation)
	}
	if r.FromCurrency == r.ToCurrency {
		return fmt.Errorf("%w: from_currency and to_currency must differ", ErrValidation)
	}
	if r.EffectiveAt.IsZero() {
		return fmt.Errorf("%w: effective_at is required", ErrValidation)
	}
	return nil
}
