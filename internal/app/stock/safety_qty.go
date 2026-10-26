package stock

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// ErrNoWarehouse is returned when the tenant has no warehouse to host a new
// stock_initial row (needed only when setting an override with no prior row).
var ErrNoWarehouse = errors.New("stock: no warehouse")

// ErrInvalidSafetyQty is a non-negative parse/range failure for low_safe_qty.
var ErrInvalidSafetyQty = errors.New("stock: invalid safety qty")

// SafetyQtyStore persists the per-product low_safe_qty override on stock_initial.
type SafetyQtyStore interface {
	// SetProductSafetyQty writes low_safe_qty on every stock_initial row for the
	// product. qty=nil clears the override (NULL). When no row exists and qty is
	// set, inserts one row at defaultWarehouseID with qty=0.
	SetProductSafetyQty(ctx context.Context, tenantID, productID uuid.UUID, qty *decimal.Decimal, defaultWarehouseID uuid.UUID) error
}

// DefaultWarehouseLookup resolves where to park a brand-new stock_initial row.
type DefaultWarehouseLookup interface {
	DefaultWarehouseID(ctx context.Context, tenantID uuid.UUID) (uuid.UUID, error)
}

// SetSafetyQtyUseCase sets or clears the explicit low-stock override that wins
// over the learned reorder point (see replenish.ReorderPoint).
type SetSafetyQtyUseCase struct {
	store SafetyQtyStore
	wh    DefaultWarehouseLookup
}

// NewSetSafetyQtyUseCase wires the store and warehouse lookup.
func NewSetSafetyQtyUseCase(store SafetyQtyStore, wh DefaultWarehouseLookup) *SetSafetyQtyUseCase {
	return &SetSafetyQtyUseCase{store: store, wh: wh}
}

// Execute applies low_safe_qty for productID. A non-positive qty (including 0)
// clears the override so the learned ROP takes over again.
func (uc *SetSafetyQtyUseCase) Execute(ctx context.Context, tenantID, productID uuid.UUID, qty decimal.Decimal) error {
	if productID == uuid.Nil {
		return fmt.Errorf("%w: product_id required", ErrInvalidSafetyQty)
	}
	if qty.IsNegative() {
		return fmt.Errorf("%w: must be >= 0", ErrInvalidSafetyQty)
	}

	var ptr *decimal.Decimal
	if qty.IsPositive() {
		ptr = &qty
	}

	whID := uuid.Nil
	if ptr != nil {
		id, err := uc.wh.DefaultWarehouseID(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrNoWarehouse, err)
		}
		whID = id
	}

	if err := uc.store.SetProductSafetyQty(ctx, tenantID, productID, ptr, whID); err != nil {
		return fmt.Errorf("set safety qty: %w", err)
	}
	return nil
}
