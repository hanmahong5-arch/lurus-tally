package ai

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	domainai "github.com/hanmahong5-arch/lurus-tally/internal/domain/ai"
)

// UndoTTLSeconds is the window during which a confirmed plan may be reverted.
// After this window the revert endpoint returns ErrRevertWindowClosed.
// Default 30 s — matches the FE undo-stack EXPIRY_MS constant.
const UndoTTLSeconds = 30

// ErrRevertWindowClosed is returned when more than UndoTTLSeconds have elapsed
// since the plan was confirmed.
var ErrRevertWindowClosed = errors.New("revert: undo window has closed (>30 s since execution)")

// ErrAlreadyReverted is returned when a plan has already been reverted (idempotency guard).
var ErrAlreadyReverted = errors.New("revert: plan already reverted")

// ErrPlanNotRevertible is returned when the plan type does not support revert.
var ErrPlanNotRevertible = errors.New("revert: plan type does not support undo")

// PriceBeforeEntry is one SKU's price state captured before execution.
type PriceBeforeEntry struct {
	SKUID    uuid.UUID       `json:"sku_id"`
	OldPrice decimal.Decimal `json:"old_price"`
}

// PriceSnapshotStore persists and retrieves the before-prices for a price-change
// plan so the revert path can restore the original values.
// Backed by Redis with TTL = UndoTTLSeconds.
type PriceSnapshotStore interface {
	// SaveSnapshot stores the before-price entries for planID.
	// TTL is applied by the implementation (UndoTTLSeconds).
	SaveSnapshot(ctx context.Context, tenantID, planID uuid.UUID, entries []PriceBeforeEntry) error
	// GetSnapshot retrieves and deletes the snapshot for planID (consume-once).
	// Returns nil, nil when not found or already consumed.
	GetSnapshot(ctx context.Context, tenantID, planID uuid.UUID) ([]PriceBeforeEntry, error)
}

// StockReverterPort reverses a bulk_stock_adjust plan by reading the original
// movements (keyed by planID as reference_id) and writing compensating movements.
type StockReverterPort interface {
	// RevertStockAdjust writes one reverse movement per original movement that
	// references planID. Returns the number of movements written.
	RevertStockAdjust(ctx context.Context, tenantID, actorID, planID uuid.UUID) (int, error)
}

// PriceReverterPort restores SKU prices from a before-state snapshot.
type PriceReverterPort interface {
	// RestorePrices sets each SKU back to its captured old_price.
	// Returns the number of SKUs updated.
	RestorePrices(ctx context.Context, tenantID uuid.UUID, entries []PriceBeforeEntry) (int, error)
}

// PriceCapturerPort captures the current prices of the matched product IDs
// before execution so they can be restored on revert.
type PriceCapturerPort interface {
	// CaptureBeforePrices returns the current retail prices for the given products.
	CaptureBeforePrices(ctx context.Context, tenantID uuid.UUID, productIDs []uuid.UUID) ([]PriceBeforeEntry, error)
}

// Reverter performs undo for bulk_stock_adjust and price_change plans.
// It is composed with the Orchestrator at the handler level; the plan store is
// the same PlanStore used by the orchestrator.
type Reverter struct {
	planStore     PlanStore
	stockReverter StockReverterPort
	priceReverter PriceReverterPort
	snapshotStore PriceSnapshotStore
	undoTTL       time.Duration // configurable; defaults to UndoTTLSeconds
}

// NewReverter constructs a Reverter.
func NewReverter(
	planStore PlanStore,
	stockReverter StockReverterPort,
	priceReverter PriceReverterPort,
	snapshotStore PriceSnapshotStore,
) *Reverter {
	return &Reverter{
		planStore:     planStore,
		stockReverter: stockReverter,
		priceReverter: priceReverter,
		snapshotStore: snapshotStore,
		undoTTL:       UndoTTLSeconds * time.Second,
	}
}

// WithUndoTTL overrides the default 30 s window for testing.
func (r *Reverter) WithUndoTTL(d time.Duration) *Reverter {
	r.undoTTL = d
	return r
}

// RevertResult summarises the outcome of a revert operation.
type RevertResult struct {
	PlanID        uuid.UUID `json:"plan_id"`
	RevertedType  string    `json:"reverted_type"`
	AffectedCount int       `json:"affected_count"`
}

// RevertPlan undoes a confirmed bulk_stock_adjust or price_change plan.
//
// Guards:
//   - plan must exist and belong to tenantID
//   - plan.Type must be bulk_stock_adjust or price_change
//   - plan.Status must be confirmed (not already reverted or pending)
//   - time since plan.CreatedAt must be < UndoTTL
//
// On success the plan status is flipped to "cancelled" so the FE cannot revert twice.
// The status flip is the idempotency lock — a concurrent second revert will find
// the status is not "confirmed" and return ErrAlreadyReverted.
func (r *Reverter) RevertPlan(ctx context.Context, tenantID, actorID, planID uuid.UUID) (*RevertResult, error) {
	plan, err := r.planStore.GetPlan(ctx, tenantID, planID)
	if err != nil {
		return nil, fmt.Errorf("revert plan: get: %w", err)
	}
	if plan == nil {
		return nil, ErrPlanNotFound
	}

	switch plan.Type {
	case domainai.PlanTypeBulkStockAdjust, domainai.PlanTypePriceChange:
		// supported
	default:
		return nil, ErrPlanNotRevertible
	}

	if plan.Status != domainai.PlanStatusConfirmed {
		// Covers both "already reverted → cancelled" and "pending/expired → not yet executed".
		if plan.Status == domainai.PlanStatusCancelled {
			return nil, ErrAlreadyReverted
		}
		return nil, fmt.Errorf("revert plan: plan is %s, expected confirmed", plan.Status)
	}

	// Undo window check — we use CreatedAt as the proxy for "when execution happened"
	// (execution is nearly instantaneous after creation). The plan CreatedAt is set
	// by the orchestrator at proposal time and is immutable.
	if time.Since(plan.CreatedAt) > r.undoTTL {
		return nil, ErrRevertWindowClosed
	}

	// Flip status first — acts as a distributed idempotency lock.
	plan.Status = domainai.PlanStatusCancelled
	if err := r.planStore.UpdatePlan(ctx, plan); err != nil {
		return nil, fmt.Errorf("revert plan: lock status flip: %w", err)
	}

	var affected int
	switch plan.Type {
	case domainai.PlanTypeBulkStockAdjust:
		affected, err = r.stockReverter.RevertStockAdjust(ctx, tenantID, actorID, planID)
		if err != nil {
			// Best-effort rollback of the status flip so the user can retry.
			plan.Status = domainai.PlanStatusConfirmed
			_ = r.planStore.UpdatePlan(ctx, plan)
			return nil, fmt.Errorf("revert plan: reverse stock movements: %w", err)
		}
	case domainai.PlanTypePriceChange:
		entries, snapErr := r.snapshotStore.GetSnapshot(ctx, tenantID, planID)
		if snapErr != nil {
			plan.Status = domainai.PlanStatusConfirmed
			_ = r.planStore.UpdatePlan(ctx, plan)
			return nil, fmt.Errorf("revert plan: get price snapshot: %w", snapErr)
		}
		if len(entries) == 0 {
			// Snapshot already consumed or TTL elapsed — cannot revert.
			plan.Status = domainai.PlanStatusConfirmed
			_ = r.planStore.UpdatePlan(ctx, plan)
			return nil, ErrRevertWindowClosed
		}
		affected, err = r.priceReverter.RestorePrices(ctx, tenantID, entries)
		if err != nil {
			plan.Status = domainai.PlanStatusConfirmed
			_ = r.planStore.UpdatePlan(ctx, plan)
			return nil, fmt.Errorf("revert plan: restore prices: %w", err)
		}
	}

	return &RevertResult{
		PlanID:        planID,
		RevertedType:  string(plan.Type),
		AffectedCount: affected,
	}, nil
}
