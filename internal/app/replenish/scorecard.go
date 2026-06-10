// Package replenish implements the weekly replenishment decision surface.
package replenish

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// scorecardWindowDays is the rolling window the scorecard reports over.
// 28 days = 4 full weeks, aligning with the weekly replenishment cadence so
// the denominator always covers the same number of suggestion cycles.
const scorecardWindowDays = 28

// ScorecardRaw is what the repo returns — window counts without derived rates.
type ScorecardRaw struct {
	SuggestionsCount int // ledger rows in window (one per product-day)
	AdoptedCount     int // rows in window with adopted_at set
	StockoutMisses   int // products suggested, never adopted, now at zero stock
}

// ScorecardRepo is the data access interface required by GetScorecardUseCase.
type ScorecardRepo interface {
	Scorecard(ctx context.Context, tenantID uuid.UUID, windowDays int) (ScorecardRaw, error)
}

// Scorecard is the use-case output: window counts plus the adoption rate.
type Scorecard struct {
	WindowDays       int
	SuggestionsCount int
	AdoptedCount     int
	AdoptionRate     float64 // AdoptedCount / SuggestionsCount; 0 when 0/0
	StockoutMisses   int
}

// GetScorecardUseCase reports the suggestion track record (F3): how many
// suggestions the system made, how many the user adopted, and how many
// ignored suggestions ended in a stockout.
type GetScorecardUseCase struct {
	repo ScorecardRepo
}

// NewGetScorecardUseCase constructs the use case with its required repo.
func NewGetScorecardUseCase(repo ScorecardRepo) *GetScorecardUseCase {
	return &GetScorecardUseCase{repo: repo}
}

// Execute returns the 28-day scorecard for the tenant.
func (uc *GetScorecardUseCase) Execute(ctx context.Context, tenantID uuid.UUID) (*Scorecard, error) {
	if tenantID == uuid.Nil {
		return nil, fmt.Errorf("replenish scorecard: tenant_id required")
	}
	raw, err := uc.repo.Scorecard(ctx, tenantID, scorecardWindowDays)
	if err != nil {
		return nil, fmt.Errorf("replenish scorecard: %w", err)
	}
	// 0/0 reports rate 0, not NaN — an empty ledger is "no track record yet".
	rate := 0.0
	if raw.SuggestionsCount > 0 {
		rate = float64(raw.AdoptedCount) / float64(raw.SuggestionsCount)
	}
	return &Scorecard{
		WindowDays:       scorecardWindowDays,
		SuggestionsCount: raw.SuggestionsCount,
		AdoptedCount:     raw.AdoptedCount,
		AdoptionRate:     rate,
		StockoutMisses:   raw.StockoutMisses,
	}, nil
}
