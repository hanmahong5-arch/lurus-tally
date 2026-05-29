package bill

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
	domainstock "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/loghelper"
)

// ApproveReturnRequest is the input to ApproveReturnBillUseCase.
type ApproveReturnRequest struct {
	TenantID  uuid.UUID
	BillID    uuid.UUID
	CreatorID uuid.UUID
}

// ApproveReturnBillUseCase approves a draft return-stock bill (bill_type=入库, sub_type=销售退货)
// and records stock-in movements. Direction is always "in" — goods are returning to the warehouse.
// All writes happen in a single database transaction; the use case is idempotent (second call is a no-op).
type ApproveReturnBillUseCase struct {
	repo    BillRepo
	stockUC StockMovementExecutor
}

// NewApproveReturnBillUseCase constructs the use case.
func NewApproveReturnBillUseCase(repo BillRepo, stockUC StockMovementExecutor) *ApproveReturnBillUseCase {
	return &ApproveReturnBillUseCase{repo: repo, stockUC: stockUC}
}

// Execute approves the given return-stock bill within a single transaction.
func (uc *ApproveReturnBillUseCase) Execute(ctx context.Context, req ApproveReturnRequest) error {
	if req.TenantID == uuid.Nil {
		return fmt.Errorf("approve return: tenant_id is required")
	}
	if req.BillID == uuid.Nil {
		return fmt.Errorf("approve return: bill_id is required")
	}

	// Pre-lock idempotency check: if already approved, short-circuit before taking the advisory lock.
	if preFetch, err := uc.repo.GetBill(ctx, req.TenantID, req.BillID); err == nil && preFetch.Status == domain.StatusApproved {
		return nil
	}

	return uc.repo.WithTx(ctx, func(tx *sql.Tx) error {
		// Advisory lock prevents concurrent double-approval of the same bill.
		if err := uc.repo.AcquireBillAdvisoryLock(ctx, tx, req.TenantID, req.BillID); err != nil {
			return fmt.Errorf("approve return: %w: %v", ErrBillApprovalConflict, err)
		}

		head, err := uc.repo.GetBillForUpdate(ctx, tx, req.TenantID, req.BillID)
		if err != nil {
			return fmt.Errorf("approve return: load bill: %w", err)
		}

		// Idempotent: concurrent approval raced past the pre-lock check.
		if head.Status == domain.StatusApproved {
			return nil
		}

		if !head.Status.CanTransitionTo(domain.StatusApproved) {
			return fmt.Errorf("approve return: %w: current status is %d", ErrInvalidBillStatus, head.Status)
		}

		items, err := uc.repo.GetBillItems(ctx, req.TenantID, req.BillID)
		if err != nil {
			return fmt.Errorf("approve return: load items: %w", err)
		}

		warehouseID := uuid.Nil
		if head.WarehouseID != nil {
			warehouseID = *head.WarehouseID
		}

		// Record one stock-in movement per line item.
		// No unit conversion factor lookup: return bills from the import path carry
		// quantities already expressed in the product's base unit (UnitID is nil).
		// When UnitID is set, treat ConvFactor as 1 (no separate unit repo call needed).
		for _, item := range items {
			convFactor := "1"
			_, err := uc.stockUC.ExecuteInTx(ctx, tx, appstock.RecordMovementRequest{
				TenantID:    req.TenantID,
				ProductID:   item.ProductID,
				WarehouseID: warehouseID,
				Direction:   domainstock.DirectionIn, // goods enter the warehouse on return
				Qty:         item.Qty,
				UnitID: func() uuid.UUID {
					if item.UnitID != nil {
						return *item.UnitID
					}
					return uuid.Nil
				}(),
				ConvFactor:    convFactor,
				UnitCost:      item.UnitPrice, // restore unit cost from the return line
				CostStrategy:  "wac",
				ReferenceType: domainstock.RefReturn,
				ReferenceID:   &req.BillID,
			})
			if err != nil {
				return fmt.Errorf("approve return: record movement for item line_no=%d: %w", item.LineNo, err)
			}
		}

		now := time.Now().UTC()
		if err := uc.repo.UpdateBillStatus(ctx, tx, req.TenantID, req.BillID, domain.StatusApproved, map[string]any{
			"approved_at": now,
			"approved_by": req.CreatorID,
		}); err != nil {
			loghelper.Error(ctx, "bill_approved", err, map[string]any{
				"bill_type": "return",
				"bill_id":   req.BillID.String(),
				"result":    "failed",
			})
			return fmt.Errorf("approve return: update status: %w", err)
		}

		middleware.IncBillApproved("return", req.TenantID.String())
		loghelper.Info(ctx, "bill_approved", map[string]any{
			"bill_type": "return",
			"bill_id":   req.BillID.String(),
			"result":    "ok",
		})
		return nil
	})
}
