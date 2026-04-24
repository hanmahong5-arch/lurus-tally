package currency

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/currency"
)

// RateResult is the output of GetRateUseCase.Execute.
type RateResult struct {
	Rate    decimal.Decimal `json:"rate"`
	Source  string          `json:"source"`
	Warning string          `json:"warning,omitempty"`
}

// GetRateUseCase looks up the most recent exchange rate on or before a given date.
// If no rate is found, it returns a default rate of 1 with a warning.
type GetRateUseCase struct {
	repo CurrencyRepo
}

// NewGetRateUseCase constructs the use case.
func NewGetRateUseCase(repo CurrencyRepo) *GetRateUseCase {
	return &GetRateUseCase{repo: repo}
}

// Execute returns the best available rate for the given currency pair and date.
// Falls back to rate=1, source="default", warning="no_rate_found" when no data exists.
func (uc *GetRateUseCase) Execute(ctx context.Context, tenantID uuid.UUID, from, to string, date time.Time) (*RateResult, error) {
	if from == to {
		return &RateResult{Rate: decimal.NewFromInt(1), Source: domain.SourceManual}, nil
	}

	r, err := uc.repo.GetRateOn(ctx, tenantID, from, to, date)
	if err != nil {
		return nil, fmt.Errorf("get rate: %w", err)
	}
	if r == nil {
		return &RateResult{
			Rate:    decimal.NewFromInt(1),
			Source:  "default",
			Warning: "no_rate_found",
		}, nil
	}
	return &RateResult{
		Rate:   r.Rate,
		Source: r.Source,
	}, nil
}
