package currency_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	appcurrency "github.com/hanmahong5-arch/lurus-tally/internal/app/currency"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/currency"
)

// --- shared mock repo ---

type mockCurrencyRepo struct {
	currencies []domain.Currency
	rates      []domain.ExchangeRate
	saved      *domain.ExchangeRate
	saveErr    error
}

func (m *mockCurrencyRepo) ListCurrencies(_ context.Context) ([]domain.Currency, error) {
	return m.currencies, nil
}

func (m *mockCurrencyRepo) GetRateOn(_ context.Context, _ uuid.UUID, from, to string, date time.Time) (*domain.ExchangeRate, error) {
	var best *domain.ExchangeRate
	for i := range m.rates {
		r := &m.rates[i]
		if r.FromCurrency == from && r.ToCurrency == to && !r.EffectiveAt.After(date) {
			if best == nil || r.EffectiveAt.After(best.EffectiveAt) {
				best = r
			}
		}
	}
	return best, nil
}

func (m *mockCurrencyRepo) SaveRate(_ context.Context, r *domain.ExchangeRate) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.saved = r
	return nil
}

func (m *mockCurrencyRepo) ListRateHistory(_ context.Context, _ uuid.UUID, from, to string, days int) ([]domain.ExchangeRate, error) {
	cutoff := time.Now().AddDate(0, 0, -days)
	var result []domain.ExchangeRate
	for _, r := range m.rates {
		if r.FromCurrency == from && r.ToCurrency == to && !r.EffectiveAt.Before(cutoff) {
			result = append(result, r)
		}
	}
	return result, nil
}

var testTenantID = uuid.New()

func sixCurrencies() []domain.Currency {
	return []domain.Currency{
		{Code: "CNY", Name: "人民币", Symbol: "¥", Enabled: true},
		{Code: "USD", Name: "美元", Symbol: "$", Enabled: true},
		{Code: "EUR", Name: "欧元", Symbol: "€", Enabled: true},
		{Code: "GBP", Name: "英镑", Symbol: "£", Enabled: true},
		{Code: "JPY", Name: "日元", Symbol: "¥", Enabled: true},
		{Code: "HKD", Name: "港币", Symbol: "HK$", Enabled: true},
	}
}

func TestListCurrencies_ReturnsAll(t *testing.T) {
	repo := &mockCurrencyRepo{currencies: sixCurrencies()}
	uc := appcurrency.NewListCurrenciesUseCase(repo)

	result, err := uc.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result) != 6 {
		t.Errorf("expected 6 currencies, got %d", len(result))
	}
}
