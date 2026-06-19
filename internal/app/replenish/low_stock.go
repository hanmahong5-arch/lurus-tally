package replenish

import (
	"context"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// LowStockRow is one product at or below its auto-computed reorder point.
// Granularity is per-product (a reorder decision is a per-product purchasing
// question; the engine sums available across warehouses). Decimal values are
// rendered as JSON strings to preserve precision (mirrors StockSnapshot on the
// wire).
type LowStockRow struct {
	ProductID     uuid.UUID `json:"product_id"`
	ProductCode   string    `json:"product_code"`
	ProductName   string    `json:"product_name"`
	AvailableQty  string    `json:"available_qty"`
	ReorderPoint  string    `json:"reorder_point"` // learned ROP, or explicit low_safe_qty if set
	AvgDailySales string    `json:"avg_daily_sales"`
	DaysOfSupply  string    `json:"days_of_supply"` // available / avgDaily (urgency)
}

// ListLowStockUseCase returns products whose available stock has fallen at or
// below their reorder point. The reorder point is the SAME learned demand +
// lead-time intelligence ListSuggestionsUseCase computes (zero-config) via the
// shared Forecast; a per-product low_safe_qty, when set, acts as an explicit
// override. There is no threshold-config API — the low-stock alert and the
// reorder suggestion are one signal off one reorder-point definition.
//
// It lives in this package (not app/stock) because it reuses Forecast and
// SuggestionRepo; app/stock cannot import app/replenish (replenish already
// depends on app/stock via the approve/draft paths — that would cycle).
type ListLowStockUseCase struct {
	repo SuggestionRepo
}

// NewListLowStockUseCase constructs the use case from the replenish repo so the
// alert reuses one reorder-point definition (no duplicate SQL or forecast math).
func NewListLowStockUseCase(repo SuggestionRepo) *ListLowStockUseCase {
	return &ListLowStockUseCase{repo: repo}
}

// ReorderPoint is the reorder point against which available stock is compared.
// An explicit per-product low_safe_qty (SafetyQty) wins when set; otherwise the
// learned ROP is used. Zero means "no demand signal" (new SKU / dead stock) —
// the caller skips those rather than alerting on noise.
//
// Exported so the Monday weekly digest reuses the SAME reorder-point definition
// the low-stock alert uses (one signal, off one definition): the digest's
// replenish count then equals the dashboard low-stock count by construction.
func ReorderPoint(f SuggestionRow) decimal.Decimal {
	if f.SafetyQty.IsPositive() {
		return f.SafetyQty
	}
	return f.ROP
}

// Execute returns up to `limit` products at or below their reorder point,
// sorted by days-of-supply ascending (closest to stockout first); limit<=0
// picks a sensible default (200).
func (uc *ListLowStockUseCase) Execute(ctx context.Context, tenantID uuid.UUID, limit int) ([]LowStockRow, error) {
	if limit <= 0 {
		limit = 200
	}

	raws, err := uc.repo.ListSuggestions(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list low stock: %w", err)
	}

	matched := make([]SuggestionRow, 0, len(raws))
	for _, raw := range raws {
		f := Forecast(raw, DefaultWeeks)
		threshold := ReorderPoint(f)
		// Skip when there is no reorder point (no demand signal) or stock is
		// still above it.
		if threshold.IsZero() || f.AvailableQty.GreaterThanOrEqual(threshold) {
			continue
		}
		matched = append(matched, f)
	}

	// Most urgent first — days-of-supply ascending (UrgencyScore IS days-of-supply).
	sort.Slice(matched, func(i, j int) bool {
		return matched[i].UrgencyScore.LessThan(matched[j].UrgencyScore)
	})
	if len(matched) > limit {
		matched = matched[:limit]
	}

	out := make([]LowStockRow, 0, len(matched))
	for _, f := range matched {
		out = append(out, LowStockRow{
			ProductID:     f.ProductID,
			ProductCode:   f.ProductCode,
			ProductName:   f.ProductName,
			AvailableQty:  f.AvailableQty.String(),
			ReorderPoint:  ReorderPoint(f).String(),
			AvgDailySales: f.AvgDailySales.String(),
			DaysOfSupply:  f.UrgencyScore.String(),
		})
	}
	return out, nil
}
