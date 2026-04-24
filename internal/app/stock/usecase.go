package stock

import (
	"context"
	"database/sql"
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

// NATSPublisher publishes stock-changed events asynchronously.
// A nil implementation is accepted (NATS not yet configured in MVP lifecycle).
type NATSPublisher interface {
	Publish(ctx context.Context, subject string, payload []byte) error
}

// RecordMovementUseCase orchestrates a single stock movement transaction.
// It is designed for injection by Epic 6/7 use cases; they never call the HTTP handler directly.
type RecordMovementUseCase struct {
	repo       StockRepo
	calculator InventoryCalculator
	nats       NATSPublisher // may be nil
	log        *slog.Logger
}

// NewRecordMovementUseCase constructs the use case.
// nats may be nil; missing NATS connection is logged but does not fail movements.
func NewRecordMovementUseCase(
	repo StockRepo,
	calculator InventoryCalculator,
	nats NATSPublisher,
	log *slog.Logger,
) *RecordMovementUseCase {
	if log == nil {
		log = slog.Default()
	}
	return &RecordMovementUseCase{
		repo:       repo,
		calculator: calculator,
		nats:       nats,
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
//  6. Commit.
//  7. Publish psi.stock.changed to NATS (best-effort, non-blocking).
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
		return nil
	})
	if txErr != nil {
		return nil, txErr
	}

	// Publish psi.stock.changed — best effort; failure is logged, not returned.
	uc.publishEvent(ctx, req, m, snap)

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
		return decimal.Zero, fmt.Errorf("conversion factor must be > 0, got %s", factor)
	}
	return qty.Mul(f), nil
}

// publishEvent sends psi.stock.changed to NATS after a successful commit.
// Non-fatal: failure only emits a warning log.
func (uc *RecordMovementUseCase) publishEvent(ctx context.Context, req RecordMovementRequest, m *domain.Movement, snap *domain.Snapshot) {
	if uc.nats == nil {
		uc.log.Warn("NATS not configured, skipping psi.stock.changed event",
			slog.String("product_id", req.ProductID.String()),
			slog.String("direction", string(req.Direction)),
		)
		return
	}

	payload := fmt.Sprintf(
		`{"tenant_id":%q,"product_id":%q,"warehouse_id":%q,"direction":%q,"qty_base_delta":%s,"new_on_hand_qty":%s,"occurred_at":%q}`,
		req.TenantID, req.ProductID, req.WarehouseID,
		req.Direction,
		m.QtyBase.String(),
		snap.OnHandQty.String(),
		m.OccurredAt.Format(time.RFC3339),
	)

	if err := uc.nats.Publish(ctx, "psi.stock.changed", []byte(payload)); err != nil {
		uc.log.Warn("failed to publish psi.stock.changed",
			slog.String("product_id", req.ProductID.String()),
			slog.Any("error", err),
		)
	}
}
