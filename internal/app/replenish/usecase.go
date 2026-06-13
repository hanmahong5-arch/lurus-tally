// Package replenish implements the weekly replenishment decision surface.
// It answers "what should we order this week, and how much?" by combining
// current available stock, recent sales velocity, product lead time, and
// in-transit quantities to compute a demand-forecast Re-Order Point (ROP)
// with safety stock.
package replenish

import (
	"context"
	"fmt"
	"math"
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
	SuggestedQty  decimal.Decimal // max(0, target + safetyStock − available − inTransit)
	EstAmountCNY  decimal.Decimal // SuggestedQty × unit_cost
	SupplierID    *uuid.UUID      // nil when no supplier is linked
	SupplierName  string          // empty when nil
	// UrgencyScore is days-of-supply at current velocity; lower = more urgent.
	// Zero velocity products are scored MaxFloat64 (not urgent in a sales sense).
	UrgencyScore decimal.Decimal

	// Forecast-driven fields (v2 formula).
	LeadTimeDays int             // per-product lead time (days); default 7
	InTransit    decimal.Decimal // open purchase qty already ordered but not yet received
	ROP          decimal.Decimal // Re-Order Point = avgDaily × leadTime + safetyStock
	SafetyStock  decimal.Decimal // z × σ × √leadTime  (z=1.65, σ≈avgDaily×0.3)
	// Reason is a human-readable explanation of why this qty was suggested.
	// Example: "日均5×提前期7天 + 安全库存10 − 可用30 − 在途5"
	Reason string
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
	LeadTimeDays  int             // per-product lead time from tally.product
	InTransit     decimal.Decimal // open purchase qty (status 0 or 1)
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

// z is the service-level factor for safety stock (z=1.65 → ~95% service level).
const z = 1.65

// sigmaFactor approximates σ (demand standard deviation) as a fraction of avg daily sales.
// Using 0.3 (30%) as a conservative approximation — same as internal/app/ai computeROP.
const sigmaFactor = 0.3

// Execute returns replenishment suggestions sorted by urgency ascending
// (products closest to stockout first).
//
// Formula (v2 — demand-forecast ROP with safety stock):
//
//	safetyStock  = z × (avgDailySales × σFactor) × √leadTimeDays  (z=1.65, σFactor=0.3)
//	ROP          = avgDailySales × leadTimeDays + safetyStock
//	target       = avgDailySales × 7 × weeks  (coverage window)
//	suggestedQty = max(0, target + safetyStock − availableQty − inTransit)
//	urgencyScore = availableQty / avgDailySales  (days-of-supply; 0 velocity → 999999)
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
	zD := decimal.NewFromFloat(z)
	sigmaFactorD := decimal.NewFromFloat(sigmaFactor)
	// largeScore represents "not urgent" for zero-velocity products.
	largeScore := decimal.NewFromInt(999999)

	out := make([]SuggestionRow, 0, len(raws))
	for _, r := range raws {
		leadTime := r.LeadTimeDays
		if leadTime <= 0 {
			leadTime = 7
		}

		// Safety stock: z × σ × √leadTime.
		// σ is approximated as avgDailySales × sigmaFactor.
		var safetyStock decimal.Decimal
		if !r.AvgDailySales.IsZero() {
			sigma := r.AvgDailySales.Mul(sigmaFactorD)
			sqrtLT := decimal.NewFromFloat(math.Sqrt(float64(leadTime)))
			safetyStock = zD.Mul(sigma).Mul(sqrtLT)
		}

		// ROP = avgDailySales × leadTime + safetyStock.
		ltD := decimal.NewFromInt(int64(leadTime))
		rop := r.AvgDailySales.Mul(ltD).Add(safetyStock)

		// Target coverage: avgDailySales × 7 × weeks.
		target := r.AvgDailySales.Mul(seven).Mul(weeksD)

		// Suggested = target + safetyStock − available − inTransit, floored at 0, whole units.
		suggested := target.Add(safetyStock).Sub(r.AvailableQty).Sub(r.InTransit)
		if suggested.IsNegative() {
			suggested = decimal.Zero
		}
		// Round up to the nearest whole unit.
		suggested = suggested.Ceil()

		estAmount := suggested.Mul(r.UnitCost)

		// Urgency: days-of-supply.
		var urgency decimal.Decimal
		if r.AvgDailySales.IsZero() {
			urgency = largeScore
		} else {
			urgency = r.AvailableQty.Div(r.AvgDailySales)
		}

		// Human-readable reason string (why this qty).
		reason := fmt.Sprintf(
			"日均%s×提前期%d天 + 安全库存%s − 可用%s − 在途%s",
			r.AvgDailySales.StringFixed(1),
			leadTime,
			safetyStock.StringFixed(1),
			r.AvailableQty.StringFixed(0),
			r.InTransit.StringFixed(0),
		)

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
			LeadTimeDays:  leadTime,
			InTransit:     r.InTransit,
			ROP:           rop,
			SafetyStock:   safetyStock,
			Reason:        reason,
		})
	}

	// Sort by urgency ascending — closest to stockout first.
	sort.Slice(out, func(i, j int) bool {
		return out[i].UrgencyScore.LessThan(out[j].UrgencyScore)
	})

	return out, nil
}
