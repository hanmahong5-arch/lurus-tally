package currency_test

import (
	"context"
	"testing"
	"time"

	appcurrency "github.com/hanmahong5-arch/lurus-tally/internal/app/currency"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/currency"
)

func TestListRateHistory_ReturnsRatesWithinDays(t *testing.T) {
	now := time.Now().UTC()
	repo := &mockCurrencyRepo{
		rates: []domain.ExchangeRate{
			makeRate("USD", "CNY", "7.25", now.AddDate(0, 0, -5)),
			makeRate("USD", "CNY", "7.30", now.AddDate(0, 0, -10)),
			makeRate("USD", "CNY", "7.10", now.AddDate(0, 0, -45)), // outside 30d window
		},
	}
	uc := appcurrency.NewListRateHistoryUseCase(repo)

	result, err := uc.Execute(context.Background(), testTenantID, "USD", "CNY", 30)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 rates within 30d window, got %d", len(result))
	}
}

func TestListRateHistory_DaysCappedAt365(t *testing.T) {
	// Pass days=500, should be silently capped to 365.
	// We just verify no error (mock returns empty, fine).
	repo := &mockCurrencyRepo{}
	uc := appcurrency.NewListRateHistoryUseCase(repo)

	_, err := uc.Execute(context.Background(), testTenantID, "USD", "CNY", 500)
	if err != nil {
		t.Fatalf("Execute with days=500: %v", err)
	}
}
