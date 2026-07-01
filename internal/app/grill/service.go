// Package grill is the application/use-case layer of the BBQ-stall fulfillment
// module. It orchestrates the pure domain (internal/domain/grill) over two ports
// — the temporary-entity Store and the Inventory port (接点② into tally's cost
// engine) — to realise the Phase 1 "账不乱" flow: 下单即扣 (route R1, idempotent),
// 退单反扣, and 结账只汇总 (no re-deduction). It depends on interfaces only, so it
// is unit-tested with fakes and carries no DB/transport coupling (the PG repo and
// the RecordMovementUseCase adapter implement these ports later, behind a
// migration reserved via the ledger).
package grill

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	domaingrill "github.com/hanmahong5-arch/lurus-tally/internal/domain/grill"
)

// ErrNotCalibrated is returned when an item is confirmed (下单) before the
// calibration gate — deduction must use the calibrated truth, not an estimate.
var ErrNotCalibrated = errors.New("grill: order item must be calibrated before it is confirmed")

// DeductRequest is one 接点② outbound deduction. Ref is the order item id and
// doubles as the idempotency key: tally writes at most one movement per Ref
// (INV-3).
type DeductRequest struct {
	Ref   uuid.UUID
	SKUID uuid.UUID
	Qty   int
}

// ReverseRequest reverses a prior deduction for a 退单. It references the
// original movement so the adapter writes an independent compensating movement
// (never deletes the original).
type ReverseRequest struct {
	Ref        uuid.UUID
	MovementID uuid.UUID
	SKUID      uuid.UUID
	Qty        int
}

// Inventory is 接点② to tally. Deduct reuses tally's FIFO/WAC cost engine
// (RecordMovementUseCase) and MUST be idempotent on Ref. Reverse writes the
// compensating movement for a cancellation.
type Inventory interface {
	Deduct(ctx context.Context, tenantID uuid.UUID, req DeductRequest) (movementID uuid.UUID, err error)
	Reverse(ctx context.Context, tenantID uuid.UUID, req ReverseRequest) error
}

// Store persists the temporary, RLS-isolated grill entities. Phase 1 needs only
// these reads/writes; the PG adapter implements it with tenant pinning.
type Store interface {
	GetItem(ctx context.Context, tenantID, itemID uuid.UUID) (domaingrill.OrderItem, error)
	SaveItem(ctx context.Context, tenantID uuid.UUID, item domaingrill.OrderItem) error
	ListItems(ctx context.Context, tenantID, sessionID uuid.UUID) ([]domaingrill.OrderItem, error)
	ListCharges(ctx context.Context, tenantID, sessionID uuid.UUID) ([]domaingrill.SharedCharge, error)
	CountPeople(ctx context.Context, tenantID, sessionID uuid.UUID) (int, error)
}

// Service wires the use cases over the two ports.
type Service struct {
	store Store
	inv   Inventory
}

// NewService constructs the grill application service.
func NewService(store Store, inv Inventory) *Service {
	return &Service{store: store, inv: inv}
}

// ConfirmItem is 下单即扣 (route R1): it deducts inventory for a calibrated item
// exactly once. It is idempotent — a re-confirm of an already-deducted (or
// cancelled) item is a no-op — which keeps INV-3 (≤1 movement per item) true
// under retries. Deduction uses the calibrated accounting qty (INV-5).
func (s *Service) ConfirmItem(ctx context.Context, tenantID, itemID uuid.UUID) error {
	item, err := s.store.GetItem(ctx, tenantID, itemID)
	if err != nil {
		return fmt.Errorf("grill: load item %s: %w", itemID, err)
	}
	if item.CalibratedQty == nil {
		return ErrNotCalibrated
	}
	if !item.NeedsDeduction() {
		// Already deducted, or cancelled — nothing to do (idempotent).
		return nil
	}
	movementID, err := s.inv.Deduct(ctx, tenantID, DeductRequest{
		Ref:   item.MovementReference(),
		SKUID: item.SKUID,
		Qty:   item.AccountingQty(),
	})
	if err != nil {
		return fmt.Errorf("grill: deduct item %s: %w", itemID, err)
	}
	if err := item.MarkDeducted(movementID); err != nil {
		return fmt.Errorf("grill: mark item %s deducted: %w", itemID, err)
	}
	if err := s.store.SaveItem(ctx, tenantID, item); err != nil {
		return fmt.Errorf("grill: save item %s: %w", itemID, err)
	}
	return nil
}

// CancelItem is 退单: it writes a compensating reverse movement (only if the item
// was actually deducted) and marks the line cancelled. Idempotent — cancelling an
// already-cancelled item is a no-op. The original record is retained.
func (s *Service) CancelItem(ctx context.Context, tenantID, itemID uuid.UUID) error {
	item, err := s.store.GetItem(ctx, tenantID, itemID)
	if err != nil {
		return fmt.Errorf("grill: load item %s: %w", itemID, err)
	}
	if item.Status == domaingrill.ItemCancelled {
		return nil
	}
	if item.MovementID != nil {
		if err := s.inv.Reverse(ctx, tenantID, ReverseRequest{
			Ref:        item.MovementReference(),
			MovementID: *item.MovementID,
			SKUID:      item.SKUID,
			Qty:        item.AccountingQty(),
		}); err != nil {
			return fmt.Errorf("grill: reverse item %s: %w", itemID, err)
		}
	}
	if err := item.Cancel(); err != nil {
		return fmt.Errorf("grill: cancel item %s: %w", itemID, err)
	}
	if err := s.store.SaveItem(ctx, tenantID, item); err != nil {
		return fmt.Errorf("grill: save item %s: %w", itemID, err)
	}
	return nil
}

// SettleSession is 接点③: it computes the amount due (INV-1) and never touches
// inventory (INV-4 — settlement only summarises; the stock was already moved at
// 下单). The sale-bill write-back is a later increment behind its own port.
func (s *Service) SettleSession(ctx context.Context, tenantID, sessionID uuid.UUID) (decimal.Decimal, error) {
	items, err := s.store.ListItems(ctx, tenantID, sessionID)
	if err != nil {
		return decimal.Zero, fmt.Errorf("grill: list items for session %s: %w", sessionID, err)
	}
	charges, err := s.store.ListCharges(ctx, tenantID, sessionID)
	if err != nil {
		return decimal.Zero, fmt.Errorf("grill: list charges for session %s: %w", sessionID, err)
	}
	people, err := s.store.CountPeople(ctx, tenantID, sessionID)
	if err != nil {
		return decimal.Zero, fmt.Errorf("grill: count people for session %s: %w", sessionID, err)
	}
	return domaingrill.SessionTotal(items, charges, people), nil
}
