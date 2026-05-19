package stock

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
)

// RecordMovementRequest is the canonical input type for recording a stock movement.
// Epic 6 (purchase receipt) and Epic 7 (sales shipment) inject RecordMovementUseCase
// and call Execute(ctx, RecordMovementRequest{...}) — never via the HTTP endpoint.
//
// Qty + UnitID are the user-facing inputs; the use case converts them to QtyBase via
// unitconv before passing to the calculator. If UnitID == uuid.Nil or ConvFactor == "",
// QtyBase must be supplied directly (already in base unit).
type RecordMovementRequest struct {
	// TenantID must always be set.
	TenantID uuid.UUID

	ProductID   uuid.UUID
	WarehouseID uuid.UUID

	// Direction is "in", "out", or "adjust".
	Direction domain.Direction

	// Qty is the quantity in the unit specified by UnitID.
	// The use case converts this to base-unit quantity via ConvFactor.
	Qty decimal.Decimal

	// UnitID identifies the unit the caller used. May be uuid.Nil if QtyBase is given directly.
	UnitID uuid.UUID

	// ConvFactor is the conversion factor to the product's base unit.
	// Provide "" or "1" when Qty is already in base unit.
	ConvFactor string

	// UnitCost is the per-base-unit cost. Required for inbound movements.
	UnitCost decimal.Decimal

	// CostStrategy is "wac" or "fifo". The caller should read this from the product/profile.
	// Empty string falls back to "wac".
	CostStrategy string

	// ReferenceType categorises the business event. See domain.ReferenceType constants.
	ReferenceType domain.ReferenceType

	// ReferenceID links to the source business document (e.g. purchase bill UUID).
	ReferenceID *uuid.UUID

	// OccurredAt defaults to now() when zero.
	OccurredAt time.Time

	// CreatedBy is the acting user UUID (optional).
	CreatedBy *uuid.UUID

	Note string
}

// OutboxEnqueuer is the write-side contract for enqueuing an event inside an existing
// DB transaction. Implemented by internal/adapter/repo/event_outbox.Store.
// A nil implementation is accepted — outbox is skipped but the movement still commits.
type OutboxEnqueuer interface {
	Enqueue(ctx context.Context, tx *sql.Tx, tenantID uuid.UUID, subject string, payload json.RawMessage) error
}

// RecordMovementUseCase orchestrates a single stock movement transaction.
// It is designed for injection by Epic 6/7 use cases; they never call the HTTP handler directly.
type RecordMovementUseCase struct {
	repo       StockRepo
	calculator InventoryCalculator
	outbox     OutboxEnqueuer // may be nil (dev / test)
	log        *slog.Logger
}

// NewRecordMovementUseCase constructs the use case.
// outbox may be nil; when nil, events are not queued (acceptable in dev/test).
func NewRecordMovementUseCase(
	repo StockRepo,
	calculator InventoryCalculator,
	outbox OutboxEnqueuer,
	log *slog.Logger,
) *RecordMovementUseCase {
	if log == nil {
		log = slog.Default()
	}
	return &RecordMovementUseCase{
		repo:       repo,
		calculator: calculator,
		outbox:     outbox,
		log:        log,
	}
}

// Execute validates and applies a stock movement, returning the updated snapshot.
// Flow:
//  1. Convert Qty to base unit via ConvFactor.
//  2. Open PG transaction.
//  3. Acquire advisory lock for (tenantID, productID, warehouseID).
//  4. ValidateMovement — returns *InsufficientStockError on oversell.
//  5. ApplyMovement — persists movement + snapshot + lots.
//  6. Enqueue outbox row in the same transaction (atomic with the movement write).
//  7. Commit — outbox row and movement are committed together.
//  8. Background worker drains outbox → NATS; NATS outage cannot lose events.
func (uc *RecordMovementUseCase) Execute(ctx context.Context, req RecordMovementRequest) (*domain.Snapshot, error) {
	if req.TenantID == uuid.Nil {
		return nil, fmt.Errorf("record movement: tenant_id is required")
	}
	if req.ProductID == uuid.Nil {
		return nil, fmt.Errorf("record movement: product_id is required")
	}
	if req.WarehouseID == uuid.Nil {
		return nil, fmt.Errorf("record movement: warehouse_id is required")
	}
	if err := req.Direction.Validate(); err != nil {
		return nil, fmt.Errorf("record movement: %w", err)
	}

	// Convert to base unit quantity.
	qtyBase, err := convertToBase(req.Qty, req.ConvFactor)
	if err != nil {
		return nil, fmt.Errorf("record movement: unit conversion: %w", err)
	}

	occurredAt := req.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}

	m := &domain.Movement{
		ID:            uuid.New(),
		TenantID:      req.TenantID,
		ProductID:     req.ProductID,
		WarehouseID:   req.WarehouseID,
		Direction:     req.Direction,
		QtyBase:       qtyBase,
		UnitCost:      req.UnitCost,
		ReferenceType: req.ReferenceType,
		ReferenceID:   req.ReferenceID,
		OccurredAt:    occurredAt,
		CreatedBy:     req.CreatedBy,
		Note:          req.Note,
	}

	var snap *domain.Snapshot

	txErr := uc.repo.WithTx(ctx, func(tx *sql.Tx) error {
		// Serialise concurrent writes to the same SKU/warehouse.
		if err := uc.repo.AcquireAdvisoryLock(ctx, tx, req.TenantID, req.ProductID, req.WarehouseID); err != nil {
			return fmt.Errorf("acquire advisory lock: %w", err)
		}

		if err := uc.calculator.ValidateMovement(ctx, tx, m); err != nil {
			return err // *InsufficientStockError bubbles up as-is for HTTP 422
		}

		s, err := uc.calculator.ApplyMovement(ctx, tx, m)
		if err != nil {
			return fmt.Errorf("apply movement: %w", err)
		}
		snap = s

		// Enqueue outbox row atomically with the movement write.
		// If outbox is not configured (dev/test), skip silently.
		if uc.outbox != nil {
			if err := uc.enqueueOutbox(ctx, tx, req, m, snap); err != nil {
				return fmt.Errorf("enqueue outbox: %w", err)
			}
		}
		return nil
	})
	if txErr != nil {
		return nil, txErr
	}

	return snap, nil
}

// ExecuteInTx is identical to Execute but uses an externally-managed transaction.
// Use this when the caller (e.g. ApprovePurchaseUseCase) needs to atomically commit
// multiple movements together with a bill status update in one transaction.
//
// Caller is responsible for tx.Begin / tx.Commit / tx.Rollback.
// The advisory lock is still acquired inside the provided tx so it is automatically
// released when that transaction ends.
func (uc *RecordMovementUseCase) ExecuteInTx(ctx context.Context, tx *sql.Tx, req RecordMovementRequest) (*domain.Snapshot, error) {
	if req.TenantID == uuid.Nil {
		return nil, fmt.Errorf("record movement (tx): tenant_id is required")
	}
	if req.ProductID == uuid.Nil {
		return nil, fmt.Errorf("record movement (tx): product_id is required")
	}
	if req.WarehouseID == uuid.Nil {
		return nil, fmt.Errorf("record movement (tx): warehouse_id is required")
	}
	if err := req.Direction.Validate(); err != nil {
		return nil, fmt.Errorf("record movement (tx): %w", err)
	}

	qtyBase, err := convertToBase(req.Qty, req.ConvFactor)
	if err != nil {
		return nil, fmt.Errorf("record movement (tx): unit conversion: %w", err)
	}

	occurredAt := req.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}

	m := &domain.Movement{
		ID:            uuid.New(),
		TenantID:      req.TenantID,
		ProductID:     req.ProductID,
		WarehouseID:   req.WarehouseID,
		Direction:     req.Direction,
		QtyBase:       qtyBase,
		UnitCost:      req.UnitCost,
		ReferenceType: req.ReferenceType,
		ReferenceID:   req.ReferenceID,
		OccurredAt:    occurredAt,
		CreatedBy:     req.CreatedBy,
		Note:          req.Note,
	}

	if err := uc.repo.AcquireAdvisoryLock(ctx, tx, req.TenantID, req.ProductID, req.WarehouseID); err != nil {
		return nil, fmt.Errorf("record movement (tx): acquire advisory lock: %w", err)
	}

	if err := uc.calculator.ValidateMovement(ctx, tx, m); err != nil {
		return nil, err
	}

	snap, err := uc.calculator.ApplyMovement(ctx, tx, m)
	if err != nil {
		return nil, fmt.Errorf("record movement (tx): apply movement: %w", err)
	}

	// Enqueue outbox row in the caller's transaction so it commits atomically
	// with the movement write and any other mutations in the same bill approval tx.
	if uc.outbox != nil {
		if err := uc.enqueueOutbox(ctx, tx, req, m, snap); err != nil {
			return nil, fmt.Errorf("record movement (tx): enqueue outbox: %w", err)
		}
	}

	return snap, nil
}

// convertToBase converts qty using the provided factor string.
// Empty or "1" factors return qty unchanged (already in base unit).
func convertToBase(qty decimal.Decimal, factor string) (decimal.Decimal, error) {
	if factor == "" || factor == "1" {
		return qty, nil
	}
	f, err := decimal.NewFromString(factor)
	if err != nil {
		return decimal.Zero, fmt.Errorf("invalid conversion factor %q: %w", factor, err)
	}
	if f.IsZero() || f.IsNegative() {
		return decimal.Zero, fmt.Errorf("%w: got %s", ErrInvalidUnitFactor, factor)
	}
	return qty.Mul(f), nil
}

// enqueueOutbox serialises a psi.stock.movement_recorded event envelope and
// inserts it into event_outbox within the provided transaction. The background
// worker drains the outbox into NATS, so NATS unavailability no longer loses events.
func (uc *RecordMovementUseCase) enqueueOutbox(ctx context.Context, tx *sql.Tx, req RecordMovementRequest, m *domain.Movement, snap *domain.Snapshot) error {
	// Build the typed event body. Using a local struct here keeps the nats
	// package import out of the stock use case; the worker re-publishes the
	// raw JSON bytes verbatim so no schema translation is needed.
	type stockMovementPayload struct {
		ProductID     string `json:"product_id"`
		WarehouseID   string `json:"warehouse_id"`
		Direction     string `json:"direction"`
		QtyDelta      string `json:"qty_delta"`
		OnHandAfter   string `json:"on_hand_after"`
		UnitCost      string `json:"unit_cost"`
		ReferenceType string `json:"reference_type"`
	}
	inner, err := json.Marshal(stockMovementPayload{
		ProductID:     m.ProductID.String(),
		WarehouseID:   m.WarehouseID.String(),
		Direction:     string(m.Direction),
		QtyDelta:      m.QtyBase.String(),
		OnHandAfter:   snap.OnHandQty.String(),
		UnitCost:      m.UnitCost.String(),
		ReferenceType: string(m.ReferenceType),
	})
	if err != nil {
		return fmt.Errorf("marshal movement payload: %w", err)
	}

	// Wrap in canonical Event envelope (mirrors nats.buildEvent layout).
	type eventEnvelope struct {
		EventID    string          `json:"event_id"`
		EventType  string          `json:"event_type"`
		TenantID   string          `json:"tenant_id"`
		OccurredAt string          `json:"occurred_at"`
		Source     string          `json:"source"`
		Payload    json.RawMessage `json:"payload"`
	}
	envelope, err := json.Marshal(eventEnvelope{
		EventID:    uuid.New().String(),
		EventType:  "stock.movement_recorded",
		TenantID:   req.TenantID.String(),
		OccurredAt: m.OccurredAt.Format(time.RFC3339),
		Source:     "tally",
		Payload:    inner,
	})
	if err != nil {
		return fmt.Errorf("marshal event envelope: %w", err)
	}

	const subject = "PSI_EVENTS.stock.movement_recorded"
	return uc.outbox.Enqueue(ctx, tx, req.TenantID, subject, json.RawMessage(envelope))
}
