package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// searchProducts executes full-text product search across name, code, mnemonic, and brand.
func (r *Registry) searchProducts(ctx context.Context, tenantID uuid.UUID, argsJSON string) (string, error) {
	var args searchProductsArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("search_products: invalid args: %w", err)
	}
	rows, err := r.productRepo.SearchProducts(ctx, tenantID, args.Query)
	if err != nil {
		return "", fmt.Errorf("search_products: %w", err)
	}
	out := make([]map[string]interface{}, 0, len(rows))
	for _, p := range rows {
		out = append(out, map[string]interface{}{
			"id": p.ID, "name": p.Name, "code": p.Code,
			"brand": p.Brand, "mnemonic": p.Mnemonic,
		})
	}
	return jsonMarshal(map[string]interface{}{"products": out, "count": len(out)})
}

// getStockSummary aggregates warehouse SKU count, inventory value, low-stock count, and dead-stock count.
func (r *Registry) getStockSummary(ctx context.Context, tenantID uuid.UUID) (string, error) {
	rows, err := r.stockRepo.ListStockSnapshots(ctx, tenantID)
	if err != nil {
		return "", fmt.Errorf("get_stock_summary: %w", err)
	}
	totalValue := decimal.Zero
	lowStock := 0
	deadStock := 0
	now := time.Now()
	for _, s := range rows {
		totalValue = totalValue.Add(s.Qty.Mul(s.UnitCost))
		rop := computeROP(s)
		if s.Qty.LessThan(rop) {
			lowStock++
		}
		if now.Sub(s.LastMovedAt) > 90*24*time.Hour {
			deadStock++
		}
	}
	return jsonMarshal(map[string]interface{}{
		"total_skus":       len(rows),
		"total_value_cny":  totalValue.StringFixed(2),
		"low_stock_count":  lowStock,
		"dead_stock_count": deadStock,
	})
}

// listLowStock lists SKUs below their re-order point or supply-day threshold.
func (r *Registry) listLowStock(ctx context.Context, tenantID uuid.UUID, argsJSON string) (string, error) {
	var args listLowStockArgs
	_ = json.Unmarshal([]byte(argsJSON), &args)
	threshDays := 7
	if args.ThresholdDays != nil {
		threshDays = *args.ThresholdDays
	}

	rows, err := r.stockRepo.ListStockSnapshots(ctx, tenantID)
	if err != nil {
		return "", fmt.Errorf("list_low_stock: %w", err)
	}

	type item struct {
		Name         string `json:"name"`
		Code         string `json:"code"`
		Qty          string `json:"qty"`
		ROP          string `json:"rop"`
		DaysOfSupply string `json:"days_of_supply"`
	}
	var items []item
	for _, s := range rows {
		rop := computeROP(s)
		if s.AvgDailySales.IsPositive() {
			d := s.Qty.Div(s.AvgDailySales)
			if d.LessThanOrEqual(decimal.NewFromInt(int64(threshDays))) {
				items = append(items, item{
					Name:         s.ProductName,
					Code:         s.ProductCode,
					Qty:          s.Qty.StringFixed(2),
					ROP:          rop.StringFixed(2),
					DaysOfSupply: d.StringFixed(1),
				})
			}
		} else if s.Qty.LessThan(rop) {
			items = append(items, item{
				Name:         s.ProductName,
				Code:         s.ProductCode,
				Qty:          s.Qty.StringFixed(2),
				ROP:          rop.StringFixed(2),
				DaysOfSupply: "N/A",
			})
		}
	}
	return jsonMarshal(map[string]interface{}{
		"low_stock_items": items,
		"count":           len(items),
		"threshold_days":  threshDays,
	})
}

// listDeadStock lists SKUs without stock movement over the threshold window.
func (r *Registry) listDeadStock(ctx context.Context, tenantID uuid.UUID, argsJSON string) (string, error) {
	var args listDeadStockArgs
	_ = json.Unmarshal([]byte(argsJSON), &args)
	days := 90
	if args.Days != nil {
		days = *args.Days
	}

	rows, err := r.stockRepo.ListStockSnapshots(ctx, tenantID)
	if err != nil {
		return "", fmt.Errorf("list_dead_stock: %w", err)
	}
	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	type item struct {
		Name      string `json:"name"`
		Code      string `json:"code"`
		Qty       string `json:"qty"`
		Value     string `json:"value_cny"`
		DaysSince int    `json:"days_since_last_movement"`
	}
	var items []item
	for _, s := range rows {
		if s.LastMovedAt.Before(cutoff) {
			items = append(items, item{
				Name:      s.ProductName,
				Code:      s.ProductCode,
				Qty:       s.Qty.StringFixed(2),
				Value:     s.Qty.Mul(s.UnitCost).StringFixed(2),
				DaysSince: int(time.Since(s.LastMovedAt).Hours() / 24),
			})
		}
	}
	return jsonMarshal(map[string]interface{}{
		"dead_stock_items": items,
		"count":            len(items),
		"threshold_days":   days,
	})
}

// computeROP calculates the re-order point from lead time, average daily sales, and safety stock.
func computeROP(s StockRow) decimal.Decimal {
	if s.AvgDailySales.IsZero() || s.LeadTimeDays == 0 {
		return decimal.Zero
	}
	lt := decimal.NewFromInt(int64(s.LeadTimeDays))
	avgDaily := s.AvgDailySales
	z := decimal.NewFromFloat(1.65)
	sigma := avgDaily.Mul(decimal.NewFromFloat(0.3))
	sqrtLT := decimal.NewFromFloat(math.Sqrt(float64(s.LeadTimeDays)))
	safetyStock := z.Mul(sigma).Mul(sqrtLT)
	return lt.Mul(avgDaily).Add(safetyStock)
}
