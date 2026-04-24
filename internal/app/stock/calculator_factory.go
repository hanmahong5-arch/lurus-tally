package stock

import "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"

// NewCalculator returns the InventoryCalculator that matches the profile's inventory method.
// Unknown / empty method falls back to WAC (safe default for retail profiles).
func NewCalculator(profile Profile, repo StockRepo) InventoryCalculator {
	method := ""
	if profile != nil {
		method = profile.InventoryMethod()
	}
	switch method {
	case stock.CostStrategyFIFO:
		return &FIFOCalculator{repo: repo}
	default:
		return &WACCalculator{repo: repo}
	}
}
