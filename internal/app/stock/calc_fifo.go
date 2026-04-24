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

// FIFOCalculator implements InventoryCalculator using First-In-First-Out cost accounting.
// Inbound movements create a new Lot. Outbound movements drain oldest Lots first (Go-layer iteration).
// Adjust movements update the snapshot qty without creating/draining lots.
type FIFOCalculator struct {
	repo StockRepo
}

// Name returns the FIFO strategy identifier.
func (f *FIFOCalculator) Name() string { return domain.CostStrategyFIFO }

// ValidateMovement checks outbound/adjust-negative moves will not exceed available stock.
func (f *FIFOCalculator) ValidateMovement(ctx context.Context, tx *sql.Tx, m *domain.Movement) error {
	if m.Direction == domain.DirectionIn {
		return nil
	}

	snap, err := f.repo.SelectForUpdate(ctx, tx, m.TenantID, m.ProductID, m.WarehouseID)
	if err != nil {
		return fmt.Errorf("fifo validate: %w", err)
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

// ApplyMovement executes FIFO cost logic and persists all side-effects within tx.
// Advisory lock must be held by the caller before this method is invoked.
func (f *FIFOCalculator) ApplyMovement(ctx context.Context, tx *sql.Tx, m *domain.Movement) (*domain.Snapshot, error) {
	snap, err := f.repo.SelectForUpdate(ctx, tx, m.TenantID, m.ProductID, m.WarehouseID)
	if err != nil {
		return nil, fmt.Errorf("fifo apply: load snapshot: %w", err)
	}

	oldQty := decimal.Zero
	oldCost := decimal.Zero
	if snap != nil {
		oldQty = snap.OnHandQty
		oldCost = snap.UnitCost
	}

	now := time.Now().UTC()
	if m.OccurredAt.IsZero() {
		m.OccurredAt = now
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}

	var newQty, newCost decimal.Decimal

	switch m.Direction {
	case domain.DirectionIn:
		newQty = oldQty.Add(m.QtyBase)

		// Create a new lot for this inbound receipt.
		lotID := uuid.New()
		mvID := m.ID
		lot := &domain.Lot{
			ID:               lotID,
			TenantID:         m.TenantID,
			ProductID:        m.ProductID,
			WarehouseID:      m.WarehouseID,
			LotNo:            fmt.Sprintf("LOT-%s", lotID.String()[:8]),
			Qty:              m.QtyBase,
			QtyRemaining:     m.QtyBase,
			UnitCost:         m.UnitCost,
			ReceivedAt:       m.OccurredAt,
			SourceMovementID: &mvID,
			CreatedAt:        now,
		}
		if err := f.repo.InsertLot(ctx, tx, lot); err != nil {
			return nil, fmt.Errorf("fifo apply: insert lot: %w", err)
		}

		// Snapshot unit_cost = weighted average over all stock (balance-sheet valuation).
		if oldQty.IsZero() {
			newCost = m.UnitCost
		} else {
			numerator := oldQty.Mul(oldCost).Add(m.QtyBase.Mul(m.UnitCost))
			newCost = numerator.Div(newQty).Round(6)
		}
		m.TotalCost = m.QtyBase.Mul(m.UnitCost)

	case domain.DirectionOut:
		if m.QtyBase.GreaterThan(oldQty) {
			return nil, &InsufficientStockError{Available: oldQty, Requested: m.QtyBase}
		}

		// Drain oldest lots first (FIFO).
		lots, err := f.repo.ListActiveLots(ctx, tx, m.TenantID, m.ProductID, m.WarehouseID)
		if err != nil {
			return nil, fmt.Errorf("fifo apply: list lots: %w", err)
		}

		remaining := m.QtyBase
		totalCost := decimal.Zero

		for i := range lots {
			if remaining.IsZero() {
				break
			}
			lot := &lots[i]
			consume := decimal.Min(lot.QtyRemaining, remaining)
			totalCost = totalCost.Add(consume.Mul(lot.UnitCost))
			remaining = remaining.Sub(consume)
			newLotQty := lot.QtyRemaining.Sub(consume)

			if err := f.repo.UpdateLotQty(ctx, tx, lot.ID, newLotQty); err != nil {
				return nil, fmt.Errorf("fifo apply: update lot qty: %w", err)
			}
		}

		if remaining.IsPositive() {
			return nil, fmt.Errorf(
				"fifo apply: lots exhausted before qty satisfied (remaining=%s); snapshot/lots out of sync",
				remaining,
			)
		}

		// Movement cost = weighted average of consumed lots.
		m.UnitCost = totalCost.Div(m.QtyBase).Round(6)
		m.TotalCost = totalCost

		newQty = oldQty.Sub(m.QtyBase)
		newCost = oldCost // snapshot aggregate cost unchanged on outbound

	case domain.DirectionAdjust:
		newQty = oldQty.Add(m.QtyBase)
		if newQty.IsNegative() {
			return nil, &InsufficientStockError{Available: oldQty, Requested: m.QtyBase.Neg()}
		}
		newCost = oldCost
		m.UnitCost = oldCost
		m.TotalCost = m.QtyBase.Abs().Mul(oldCost)
	}

	if err := f.repo.InsertMovement(ctx, tx, m); err != nil {
		return nil, fmt.Errorf("fifo apply: insert movement: %w", err)
	}

	newSnap := buildSnapshot(snap, m.TenantID, m.ProductID, m.WarehouseID, newQty, newCost, domain.CostStrategyFIFO)
	if err := f.repo.UpsertSnapshot(ctx, tx, newSnap); err != nil {
		return nil, fmt.Errorf("fifo apply: upsert snapshot: %w", err)
	}

	return newSnap, nil
}
