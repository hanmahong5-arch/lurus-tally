package currency

import (
	"context"
	"fmt"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/currency"
)

// ListCurrenciesUseCase returns all enabled currencies.
type ListCurrenciesUseCase struct {
	repo CurrencyRepo
}

// NewListCurrenciesUseCase constructs the use case.
func NewListCurrenciesUseCase(repo CurrencyRepo) *ListCurrenciesUseCase {
	return &ListCurrenciesUseCase{repo: repo}
}

// Execute returns all enabled currencies.
func (uc *ListCurrenciesUseCase) Execute(ctx context.Context) ([]domain.Currency, error) {
	currencies, err := uc.repo.ListCurrencies(ctx)
	if err != nil {
		return nil, fmt.Errorf("list currencies: %w", err)
	}
	return currencies, nil
}
