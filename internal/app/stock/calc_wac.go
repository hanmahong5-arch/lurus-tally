package stock

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
)

// WACCalculator implements InventoryCalculator using the Weighted Average Cost method.
// Formula (inbound):  new_avg = (old_qty * old_cost + in_qty * in_cost) / (old_qty + in_qty)
// Outbound does not change unit_cost — goods leave at the current average.
// Adjust (±qty) does not change unit_cost either.
type WACCalculator struct {
	repo StockRepo
}

// Name returns the WAC strategy identifier.
func (w *WACCalculator) Name() string { return domain.CostStrategyWAC }

// ValidateMovement checks that an outbound or negative-adjust movement will not push on_hand_qty below zero.
func (w *WACCalculator) ValidateMovement(ctx context.Context, tx *sql.Tx, m *domain.Movement) error {
	if m.Direction == domain.DirectionIn {
		return nil
	}

	snap, err := w.repo.SelectForUpdate(ctx, tx, m.TenantID, m.ProductID, m.WarehouseID)
	if err != nil {
		return fmt.Errorf("wac validate: %w", err)
	}

	available := decimal.Zero
	if snap != nil {
		available = snap.OnHandQty
	}

	switch m.Direction {
	case domain.DirectionOut:
		if m.QtyBase.GreaterThan(available) {
			return &InsufficientStockError{Available: available, Requested: m.QtyBase}
		}
	case domain.DirectionAdjust:
		if m.QtyBase.IsNegative() && m.QtyBase.Neg().GreaterThan(available) {
			return &InsufficientStockError{Available: available, Requested: m.QtyBase.Neg()}
		}
	}
	return nil
}

// ApplyMovement executes the WAC cost update and persists all side-effects.
// It must be called inside a transaction with the advisory lock already held.
func (w *WACCalculator) ApplyMovement(ctx context.Context, tx *sql.Tx, m *domain.Movement) (*domain.Snapshot, error) {
	snap, err := w.repo.SelectForUpdate(ctx, tx, m.TenantID, m.ProductID, m.WarehouseID)
	if err != nil {
		return nil, fmt.Errorf("wac apply: load snapshot: %w", err)
	}

	oldQty := decimal.Zero
	oldCost := decimal.Zero
	if snap != nil {
		oldQty = snap.OnHandQty
		oldCost = snap.UnitCost
	}

	var newQty, newCost decimal.Decimal
	inboundCost := m.UnitCost // preserve the caller-supplied inbound unit cost

	switch m.Direction {
	case domain.DirectionIn:
		newQty = oldQty.Add(m.QtyBase)
		if newQty.IsZero() {
			newCost = decimal.Zero
		} else if oldQty.IsZero() {
			newCost = inboundCost
		} else {
			numerator := oldQty.Mul(oldCost).Add(m.QtyBase.Mul(inboundCost))
			newCost = numerator.Div(newQty).Round(6)
		}
		m.TotalCost = m.QtyBase.Mul(inboundCost)

	case domain.DirectionOut:
		if m.QtyBase.GreaterThan(oldQty) {
			return nil, &InsufficientStockError{Available: oldQty, Requested: m.QtyBase}
		}
		newQty = oldQty.Sub(m.QtyBase)
		newCost = oldCost
		m.UnitCost = oldCost // out records the prevailing avg cost
		m.TotalCost = m.QtyBase.Mul(oldCost)

	case domain.DirectionAdjust:
		newQty = oldQty.Add(m.QtyBase)
		if newQty.IsNegative() {
			return nil, &InsufficientStockError{Available: oldQty, Requested: m.QtyBase.Neg()}
		}
		newCost = oldCost
		m.UnitCost = oldCost
		m.TotalCost = m.QtyBase.Abs().Mul(oldCost)
	}

	now := time.Now().UTC()
	if m.OccurredAt.IsZero() {
		m.OccurredAt = now
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}

	if err := w.repo.InsertMovement(ctx, tx, m); err != nil {
		return nil, fmt.Errorf("wac apply: insert movement: %w", err)
	}

	newSnap := buildSnapshot(snap, m.TenantID, m.ProductID, m.WarehouseID, newQty, newCost, domain.CostStrategyWAC)
	if err := w.repo.UpsertSnapshot(ctx, tx, newSnap); err != nil {
		return nil, fmt.Errorf("wac apply: upsert snapshot: %w", err)
	}

	return newSnap, nil
}

// buildSnapshot constructs a Snapshot, reusing the existing ID when available.
func buildSnapshot(
	existing *domain.Snapshot,
	tenantID, productID, warehouseID uuid.UUID,
	qty, cost decimal.Decimal,
	strategy string,
) *domain.Snapshot {
	id := uuid.New()
	if existing != nil {
		id = existing.ID
	}
	return &domain.Snapshot{
		ID:           id,
		TenantID:     tenantID,
		ProductID:    productID,
		WarehouseID:  warehouseID,
		OnHandQty:    qty,
		AvailableQty: qty, // simplified: available = on_hand for MVP
		UnitCost:     cost,
		CostStrategy: strategy,
		UpdatedAt:    time.Now().UTC(),
	}
}

// InsufficientStockError is returned when an out/adjust movement would make on_hand_qty negative.
type InsufficientStockError struct {
	Available decimal.Decimal
	Requested decimal.Decimal
}

func (e *InsufficientStockError) Error() string {
	return fmt.Sprintf(
		"stock: insufficient stock: available=%s, requested=%s",
		e.Available.String(), e.Requested.String(),
	)
}

// IsInsufficientStock reports whether err is an InsufficientStockError.
func IsInsufficientStock(err error) bool {
	_, ok := err.(*InsufficientStockError)
	return ok
}
