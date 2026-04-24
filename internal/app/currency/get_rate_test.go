package currency_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appcurrency "github.com/hanmahong5-arch/lurus-tally/internal/app/currency"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/currency"
)

func makeRate(from, to string, rateStr string, effectiveAt time.Time) domain.ExchangeRate {
	return domain.ExchangeRate{
		ID:           uuid.New(),
		TenantID:     testTenantID,
		FromCurrency: from,
		ToCurrency:   to,
		Rate:         decimal.RequireFromString(rateStr),
		Source:       domain.SourceManual,
		EffectiveAt:  effectiveAt,
	}
}

func TestGetRate_ExactDate_ReturnsManualRate(t *testing.T) {
	today := time.Now().UTC().Truncate(24 * time.Hour)
	repo := &mockCurrencyRepo{
		rates: []domain.ExchangeRate{
			makeRate("USD", "CNY", "7.25", today),
		},
	}
	uc := appcurrency.NewGetRateUseCase(repo)

	result, err := uc.Execute(context.Background(), testTenantID, "USD", "CNY", today)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Warning != "" {
		t.Errorf("unexpected warning: %s", result.Warning)
	}
	if !result.Rate.Equal(decimal.RequireFromString("7.25")) {
		t.Errorf("rate = %s, want 7.25", result.Rate)
	}
	if result.Source != domain.SourceManual {
		t.Errorf("source = %s, want manual", result.Source)
	}
}

func TestGetRate_NoExactDate_ReturnsMostRecentPriorRate(t *testing.T) {
	yesterday := time.Now().UTC().AddDate(0, 0, -1).Truncate(24 * time.Hour)
	repo := &mockCurrencyRepo{
		rates: []domain.ExchangeRate{
			makeRate("USD", "CNY", "7.25", yesterday),
		},
	}
	uc := appcurrency.NewGetRateUseCase(repo)

	today := time.Now().UTC()
	result, err := uc.Execute(context.Background(), testTenantID, "USD", "CNY", today)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Warning != "" {
		t.Errorf("unexpected warning: %s", result.Warning)
	}
	if !result.Rate.Equal(decimal.RequireFromString("7.25")) {
		t.Errorf("rate = %s, want 7.25 (fallback from yesterday)", result.Rate)
	}
}

func TestGetRate_NoData_ReturnsFallbackRate(t *testing.T) {
	repo := &mockCurrencyRepo{rates: nil}
	uc := appcurrency.NewGetRateUseCase(repo)

	result, err := uc.Execute(context.Background(), testTenantID, "USD", "CNY", time.Now())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Warning != "no_rate_found" {
		t.Errorf("warning = %q, want no_rate_found", result.Warning)
	}
	if !result.Rate.Equal(decimal.NewFromInt(1)) {
		t.Errorf("rate = %s, want 1 (default fallback)", result.Rate)
	}
	if result.Source != "default" {
		t.Errorf("source = %s, want default", result.Source)
	}
}
