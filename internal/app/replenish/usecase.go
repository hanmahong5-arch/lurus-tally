// Package replenish implements the weekly replenishment decision surface.
// It answers "what should we order this week, and how much?" by combining
// current available stock with recent sales velocity.
package replenish

import (
	"context"
	"sort"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// SuggestionRow is a single product replenishment suggestion.
type SuggestionRow struct {
	ProductID     uuid.UUID
	ProductName   string
	ProductCode   string
	AvailableQty  decimal.Decimal
	SafetyQty     decimal.Decimal // low_safe_qty from stock_initial; zero if not configured
	AvgDailySales decimal.Decimal
	SuggestedQty  decimal.Decimal // max(0, weeklyDemand * weeks - available)
	EstAmountCNY  decimal.Decimal // SuggestedQty × unit_cost
	SupplierID    *uuid.UUID      // nil when no supplier is linked
	SupplierName  string          // empty when nil
	// UrgencyScore is days-of-supply at current velocity; lower = more urgent.
	// Zero velocity products are scored MaxFloat64 (not urgent in a sales sense).
	UrgencyScore decimal.Decimal
}

// SuggestionRepo is the data access interface required by ListSuggestionsUseCase.
// Only the use case and its SQL adapter need to depend on this type.
type SuggestionRepo interface {
	// ListSuggestions returns all active products for the tenant together with
	// stock and velocity data needed to compute replenishment quantities.
	// The repo is responsible for the SQL join; the use case owns the formula.
	ListSuggestions(ctx context.Context, tenantID uuid.UUID) ([]RawRow, error)
}

// RawRow is what the repo returns — raw DB fields without formula application.
type RawRow struct {
	ProductID     uuid.UUID
	ProductName   string
	ProductCode   string
	AvailableQty  decimal.Decimal
	SafetyQty     decimal.Decimal // zero when not configured
	UnitCost      decimal.Decimal
	AvgDailySales decimal.Decimal
	SupplierID    *uuid.UUID
	SupplierName  string
}

// ListSuggestionsUseCase computes weekly replenishment suggestions.
type ListSuggestionsUseCase struct {
	repo SuggestionRepo
}

// NewListSuggestionsUseCase constructs the use case with its required repo.
func NewListSuggestionsUseCase(repo SuggestionRepo) *ListSuggestionsUseCase {
	return &ListSuggestionsUseCase{repo: repo}
}

const defaultWeeks = 2

// Execute returns replenishment suggestions sorted by urgency ascending
// (products closest to stockout first).
//
// Formula (v1 — intentionally simple, no ML):
//
//	weeklyDemand   = avgDailySales × 7 × weeks
//	suggestedQty   = max(0, weeklyDemand - availableQty)
//	estAmount      = suggestedQty × unitCost
//	urgencyScore   = availableQty / avgDailySales  (days-of-supply; 0 velocity → Inf)
func (uc *ListSuggestionsUseCase) Execute(ctx context.Context, tenantID uuid.UUID, weeks int) ([]SuggestionRow, error) {
	if weeks <= 0 {
		weeks = defaultWeeks
	}

	raws, err := uc.repo.ListSuggestions(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	weeksD := decimal.NewFromInt(int64(weeks))
	seven := decimal.NewFromInt(7)
	// largeScore represents "not urgent" for zero-velocity products.
	largeScore := decimal.NewFromInt(999999)

	out := make([]SuggestionRow, 0, len(raws))
	for _, r := range raws {
		weeklyDemand := r.AvgDailySales.Mul(seven).Mul(weeksD)
		suggested := weeklyDemand.Sub(r.AvailableQty)
		if suggested.IsNegative() {
			suggested = decimal.Zero
		}
		estAmount := suggested.Mul(r.UnitCost)

		var urgency decimal.Decimal
		if r.AvgDailySales.IsZero() {
			urgency = largeScore
		} else {
			urgency = r.AvailableQty.Div(r.AvgDailySales)
		}

		out = append(out, SuggestionRow{
			ProductID:     r.ProductID,
			ProductName:   r.ProductName,
			ProductCode:   r.ProductCode,
			AvailableQty:  r.AvailableQty,
			SafetyQty:     r.SafetyQty,
			AvgDailySales: r.AvgDailySales,
			SuggestedQty:  suggested,
			EstAmountCNY:  estAmount,
			SupplierID:    r.SupplierID,
			SupplierName:  r.SupplierName,
			UrgencyScore:  urgency,
		})
	}

	// Sort by urgency ascending — closest to stockout first.
	sort.Slice(out, func(i, j int) bool {
		return out[i].UrgencyScore.LessThan(out[j].UrgencyScore)
	})

	return out, nil
}
