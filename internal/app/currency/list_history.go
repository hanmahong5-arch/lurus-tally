package currency

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/currency"
)

// maxHistoryDays is the hard upper limit on days for history queries.
const maxHistoryDays = 365

// ListRateHistoryUseCase returns exchange rate history for a currency pair.
type ListRateHistoryUseCase struct {
	repo CurrencyRepo
}

// NewListRateHistoryUseCase constructs the use case.
func NewListRateHistoryUseCase(repo CurrencyRepo) *ListRateHistoryUseCase {
	return &ListRateHistoryUseCase{repo: repo}
}

// Execute returns rate history for the given pair. Days is capped at 365.
func (uc *ListRateHistoryUseCase) Execute(ctx context.Context, tenantID uuid.UUID, from, to string, days int) ([]domain.ExchangeRate, error) {
	if days <= 0 {
		days = 30
	}
	if days > maxHistoryDays {
		days = maxHistoryDays
	}

	rates, err := uc.repo.ListRateHistory(ctx, tenantID, from, to, days)
	if err != nil {
		return nil, fmt.Errorf("list rate history: %w", err)
	}
	return rates, nil
}
