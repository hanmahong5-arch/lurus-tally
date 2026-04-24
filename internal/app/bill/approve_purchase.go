package bill

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
	domainstock "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
)

// ErrInvalidUnitForProduct is returned when the item's unit_id is not valid for the product.
var ErrInvalidUnitForProduct = fmt.Errorf("bill: unit_id is not valid for this product")

// StockMovementExecutor is the minimal interface of RecordMovementUseCase needed by ApprovePurchase.
// It exposes only ExecuteInTx so the approve use case can participate in an outer transaction.
type StockMovementExecutor interface {
	ExecuteInTx(ctx context.Context, tx *sql.Tx, req appstock.RecordMovementRequest) (*domainstock.Snapshot, error)
}

// ProductUnitRepo provides unit conversion factor lookups.
// Implemented by adapter/repo/unit (surgical extension in Task 8).
type ProductUnitRepo interface {
	GetConversionFactor(ctx context.Context, productID, unitID uuid.UUID) (decimal.Decimal, error)
}

// ApprovePurchaseUseCase approves a draft purchase bill and records stock-in movements.
// All writes happen in a single database transaction: if any item's conversion fails,
// the entire operation rolls back and the bill remains in draft status.
type ApprovePurchaseUseCase struct {
	repo     BillRepo
	stockUC  StockMovementExecutor
	unitRepo ProductUnitRepo
}

// NewApprovePurchaseUseCase constructs the use case.
func NewApprovePurchaseUseCase(repo BillRepo, stockUC StockMovementExecutor, unitRepo ProductUnitRepo) *ApprovePurchaseUseCase {
	return &ApprovePurchaseUseCase{
		repo:     repo,
		stockUC:  stockUC,
		unitRepo: unitRepo,
	}
}

// Execute approves the given purchase bill within a single transaction.
func (uc *ApprovePurchaseUseCase) Execute(ctx context.Context, tenantID, billID, approvedBy uuid.UUID) error {
	if tenantID == uuid.Nil {
		return fmt.Errorf("approve purchase: tenant_id is required")
	}
	if billID == uuid.Nil {
		return fmt.Errorf("approve purchase: bill_id is required")
	}

	return uc.repo.WithTx(ctx, func(tx *sql.Tx) error {
		// Acquire advisory lock to prevent concurrent double-approval of the same bill.
		if err := uc.repo.AcquireBillAdvisoryLock(ctx, tx, tenantID, billID); err != nil {
			return fmt.Errorf("approve purchase: %w: %v", ErrBillApprovalConflict, err)
		}

		// Load with row lock.
		head, err := uc.repo.GetBillForUpdate(ctx, tx, tenantID, billID)
		if err != nil {
			return fmt.Errorf("approve purchase: load bill: %w", err)
		}

		// Validate state machine transition.
		if !head.Status.CanTransitionTo(domain.StatusApproved) {
			return fmt.Errorf("approve purchase: %w: current status is %d", ErrInvalidBillStatus, head.Status)
		}

		// Load items.
		items, err := uc.repo.GetBillItems(ctx, tenantID, billID)
		if err != nil {
			return fmt.Errorf("approve purchase: load items: %w", err)
		}

		// Resolve warehouse from bill head (fall back to uuid.Nil if not set).
		warehouseID := uuid.Nil
		if head.WarehouseID != nil {
			warehouseID = *head.WarehouseID
		}

		// Record one stock movement per line item.
		for _, item := range items {
			// Resolve conversion factor: if no unit_id, assume 1 (already in base unit).
			convFactor := "1"
			if item.UnitID != nil {
				f, err := uc.unitRepo.GetConversionFactor(ctx, item.ProductID, *item.UnitID)
				if err != nil {
					return fmt.Errorf("approve purchase: item line_no=%d: %w", item.LineNo, err)
				}
				convFactor = f.String()
			}

			_, err := uc.stockUC.ExecuteInTx(ctx, tx, appstock.RecordMovementRequest{
				TenantID:    tenantID,
				ProductID:   item.ProductID,
				WarehouseID: warehouseID,
				Direction:   domainstock.DirectionIn,
				Qty:         item.Qty,
				UnitID: func() uuid.UUID {
					if item.UnitID != nil {
						return *item.UnitID
					}
					return uuid.Nil
				}(),
				ConvFactor:    convFactor,
				UnitCost:      item.UnitPrice,
				CostStrategy:  "wac",
				ReferenceType: domainstock.RefPurchase,
				ReferenceID:   &billID,
			})
			if err != nil {
				return fmt.Errorf("approve purchase: record movement for item line_no=%d: %w", item.LineNo, err)
			}
		}

		// Update bill status to approved.
		now := time.Now().UTC()
		if err := uc.repo.UpdateBillStatus(ctx, tx, tenantID, billID, domain.StatusApproved, map[string]any{
			"approved_at": now,
			"approved_by": approvedBy,
		}); err != nil {
			return fmt.Errorf("approve purchase: update status: %w", err)
		}

		return nil
	})
}
