// Package sku contains use cases for product SKU pricing.
//
// Prices live in tally.product_sku (purchase_price / retail_price / wholesale_price).
// This package is the single write path for bulk price changes — used by the AI
// plan executor when a user confirms a propose_price_change plan.
package sku

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// DefaultSKU is the minimal pricing view of a product's default SKU.
type DefaultSKU struct {
	SKUID         uuid.UUID
	ProductID     uuid.UUID
	RetailPrice   decimal.Decimal
	PurchasePrice decimal.Decimal
}

// PriceRepo is the persistence interface for SKU price reads/writes.
type PriceRepo interface {
	// ListDefaultSKUs returns one row per product (the default SKU, falling back
	// to the earliest-created SKU). Products without any SKU are omitted.
	ListDefaultSKUs(ctx context.Context, tenantID uuid.UUID, productIDs []uuid.UUID) ([]DefaultSKU, error)
	// UpdateRetailPrice sets retail_price for one SKU within the tenant scope.
	UpdateRetailPrice(ctx context.Context, tenantID, skuID uuid.UUID, newPrice decimal.Decimal) error
}

// UpdatePriceUseCase applies a price action to the default SKU of each product.
type UpdatePriceUseCase struct {
	repo PriceRepo
}

// NewUpdatePriceUseCase constructs the use case.
func NewUpdatePriceUseCase(repo PriceRepo) *UpdatePriceUseCase {
	return &UpdatePriceUseCase{repo: repo}
}

// Execute applies action to retail_price of each product's default SKU.
// action forms: "+5%" / "-10%" (relative) or "=199.00" / "199.00" (absolute).
// Returns the number of SKU rows updated. Products with no SKU are skipped.
func (uc *UpdatePriceUseCase) Execute(ctx context.Context, tenantID uuid.UUID, productIDs []uuid.UUID, action string) (int, error) {
	if tenantID == uuid.Nil {
		return 0, fmt.Errorf("update price: tenant_id is required")
	}
	if len(productIDs) == 0 {
		return 0, nil
	}
	// Validate the action once up front so an invalid action fails before any write.
	if _, err := ApplyAction(decimal.Zero, action); err != nil {
		return 0, err
	}

	skus, err := uc.repo.ListDefaultSKUs(ctx, tenantID, productIDs)
	if err != nil {
		return 0, fmt.Errorf("update price: list default skus: %w", err)
	}

	affected := 0
	for _, s := range skus {
		newPrice, err := ApplyAction(s.RetailPrice, action)
		if err != nil {
			return affected, err
		}
		if err := uc.repo.UpdateRetailPrice(ctx, tenantID, s.SKUID, newPrice); err != nil {
			return affected, fmt.Errorf("update price: sku %s: %w", s.SKUID, err)
		}
		affected++
	}
	return affected, nil
}

// ApplyAction computes a new price from the current price and an action string.
// Supported forms:
//   - "+N%" / "-N%" : relative change (new = current × (1 ± N/100))
//   - "=N"          : absolute set
//   - "N"           : absolute set (bare number)
//
// Results are rounded to 6 decimals (matches NUMERIC(18,6)) and clamped at >= 0
// so a large negative percentage cannot produce a negative price.
func ApplyAction(current decimal.Decimal, action string) (decimal.Decimal, error) {
	a := strings.TrimSpace(action)
	if a == "" {
		return decimal.Zero, fmt.Errorf("price action is empty")
	}

	var result decimal.Decimal
	switch {
	case strings.HasSuffix(a, "%"):
		numPart := strings.TrimSpace(strings.TrimSuffix(a, "%"))
		pct, err := decimal.NewFromString(numPart)
		if err != nil {
			return decimal.Zero, fmt.Errorf("invalid percentage action %q: %w", action, err)
		}
		factor := decimal.NewFromInt(1).Add(pct.Div(decimal.NewFromInt(100)))
		result = current.Mul(factor)
	case strings.HasPrefix(a, "="):
		v, err := decimal.NewFromString(strings.TrimSpace(strings.TrimPrefix(a, "=")))
		if err != nil {
			return decimal.Zero, fmt.Errorf("invalid absolute action %q: %w", action, err)
		}
		result = v
	default:
		v, err := decimal.NewFromString(a)
		if err != nil {
			return decimal.Zero, fmt.Errorf("unrecognised price action %q (use '+5%%', '-10%%' or '=199.00')", action)
		}
		result = v
	}

	result = result.Round(6)
	if result.IsNegative() {
		result = decimal.Zero
	}
	return result, nil
}
