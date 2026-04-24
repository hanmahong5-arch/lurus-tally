package currency_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/currency"
)

func TestCurrency_Validate_RequiresCode(t *testing.T) {
	c := &domain.Currency{Name: "Test", Symbol: "T", Enabled: true}
	err := c.Validate()
	if err == nil {
		t.Fatal("expected error for empty code, got nil")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("expected ErrValidation, got %v", err)
	}
}

func TestCurrency_Validate_ValidCurrency(t *testing.T) {
	c := &domain.Currency{Code: "USD", Name: "US Dollar", Symbol: "$", Enabled: true}
	if err := c.Validate(); err != nil {
		t.Errorf("expected no error for valid currency, got %v", err)
	}
}

func TestExchangeRate_Validate_RatePositive(t *testing.T) {
	r := &domain.ExchangeRate{
		ID:           uuid.New(),
		TenantID:     uuid.New(),
		FromCurrency: "USD",
		ToCurrency:   "CNY",
		Rate:         decimal.Zero, // invalid
		Source:       domain.SourceManual,
		EffectiveAt:  time.Now(),
	}
	err := r.Validate()
	if err == nil {
		t.Fatal("expected error for zero rate, got nil")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("expected ErrValidation, got %v", err)
	}
}

func TestExchangeRate_Validate_NegativeRate(t *testing.T) {
	r := &domain.ExchangeRate{
		ID:           uuid.New(),
		TenantID:     uuid.New(),
		FromCurrency: "USD",
		ToCurrency:   "CNY",
		Rate:         decimal.NewFromFloat(-1),
		Source:       domain.SourceManual,
		EffectiveAt:  time.Now(),
	}
	err := r.Validate()
	if err == nil {
		t.Fatal("expected error for negative rate, got nil")
	}
}

func TestExchangeRate_Validate_SameCurrencyRejected(t *testing.T) {
	r := &domain.ExchangeRate{
		FromCurrency: "USD",
		ToCurrency:   "USD",
		Rate:         decimal.NewFromFloat(1),
		EffectiveAt:  time.Now(),
	}
	err := r.Validate()
	if err == nil {
		t.Fatal("expected error for same from/to currency, got nil")
	}
}

func TestExchangeRate_Validate_ValidRate(t *testing.T) {
	r := &domain.ExchangeRate{
		ID:           uuid.New(),
		TenantID:     uuid.New(),
		FromCurrency: "USD",
		ToCurrency:   "CNY",
		Rate:         decimal.NewFromFloat(7.25),
		Source:       domain.SourceManual,
		EffectiveAt:  time.Now(),
	}
	if err := r.Validate(); err != nil {
		t.Errorf("expected no error for valid rate, got %v", err)
	}
}
