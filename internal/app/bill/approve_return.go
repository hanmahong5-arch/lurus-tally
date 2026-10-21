package bill

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

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
	// Strict makes an over-return an error instead of trimming the lines to what the sale
	// has left. The shop-floor return sets it: the bill's quantities and amounts were
	// quoted to the clerk, so they must be booked in full or not at all.
	Strict bool
}

// ApproveReturnBillUseCase approves a draft return-stock bill (bill_type=入库, sub_type=销售退货)
// and records stock-in movements. Direction is always "in" — goods are returning to the warehouse.
// All writes happen in a single database transaction; the use case is idempotent (second call is a no-op).
type ApproveReturnBillUseCase struct {
	repo     BillRepo
	stockUC  StockMovementExecutor
	unitRepo ProductUnitRepo // optional; nil = every line is already in the base unit
}

// NewApproveReturnBillUseCase constructs the use case.
func NewApproveReturnBillUseCase(repo BillRepo, stockUC StockMovementExecutor) *ApproveReturnBillUseCase {
	return &ApproveReturnBillUseCase{repo: repo, stockUC: stockUC}
}

// WithUnitRepo enables auxiliary-unit conversion on return lines, using the same
// factor lookup as sale approval. Returns the receiver for chaining.
func (uc *ApproveReturnBillUseCase) WithUnitRepo(unitRepo ProductUnitRepo) *ApproveReturnBillUseCase {
	uc.unitRepo = unitRepo
	return uc
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

	nothingLeft := false
	err := uc.repo.WithTx(ctx, func(tx *sql.Tx) error {
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

		warehouseID, err := resolveApprovalWarehouse(ctx, uc.repo, tx, head, req.TenantID, req.BillID)
		if err != nil {
			return fmt.Errorf("approve return: %w", err)
		}

		// The original sale, when the bill points at one, supplies the cost the goods
		// left at. Without it the current average cost applies.
		costSrc, _ := uc.repo.(returnCostSource)
		originBill := uuid.Nil
		if costSrc != nil {
			if originBill, err = resolveOriginBill(ctx, costSrc, head); err != nil {
				return fmt.Errorf("approve return: %w", err)
			}
		}

		// A cancellation or platform refund may only give back what the sale still has
		// left. Concurrent deliveries of the same event, or a refund racing a
		// cancellation, each pass the create-time check; serialising on the sale and
		// re-reading what is returned here closes that window.
		var rem map[uuid.UUID]decimal.Decimal
		if originBill != uuid.Nil {
			if err := uc.repo.AcquireBillAdvisoryLock(ctx, tx, req.TenantID, originBill); err != nil {
				return fmt.Errorf("approve return: %w: %v", ErrBillApprovalConflict, err)
			}
			var isSale bool
			if rem, isSale, err = remainingBase(ctx, uc.repo, uc.unitRepo, req.TenantID, originBill); err != nil {
				return fmt.Errorf("approve return: %w", err)
			} else if !isSale {
				rem = nil
			}
		}
		moved := 0
		var strict *strictReturnCheck
		if req.Strict && originBill != uuid.Nil {
			if strict, err = uc.newStrictCheck(ctx, req.TenantID, req.BillID, originBill); err != nil {
				return fmt.Errorf("approve return: %w", err)
			}
		}

		// Record one stock-in movement per line item. The refund price is what the
		// customer got back, not what the goods cost, so it never feeds the cost average.
		for _, item := range items {
			convFactor := "1"
			if item.UnitID != nil && uc.unitRepo != nil {
				f, err := uc.unitRepo.GetConversionFactor(ctx, item.ProductID, *item.UnitID)
				if err != nil {
					return fmt.Errorf("approve return: item line_no=%d: %w", item.LineNo, err)
				}
				convFactor = f.String()
			}
			qty := item.Qty
			if strict != nil && rem != nil {
				f, ferr := decimal.NewFromString(convFactor)
				if ferr != nil || !f.IsPositive() {
					f = decimal.NewFromInt(1)
				}
				if err := strict.take(rem, item, f); err != nil {
					return err
				}
			} else if rem != nil {
				f, ferr := decimal.NewFromString(convFactor)
				if ferr != nil || !f.IsPositive() {
					f = decimal.NewFromInt(1)
				}
				got := takeBase(rem, item.ProductID, item.Qty.Mul(f))
				if !got.IsPositive() {
					continue
				}
				if got.LessThan(item.Qty.Mul(f)) {
					qty = got.Div(f)
				}
			}
			unitCost, err := uc.returnUnitCost(ctx, costSrc, originBill, req.TenantID, item.ProductID, warehouseID)
			if err != nil {
				return fmt.Errorf("approve return: cost for item line_no=%d: %w", item.LineNo, err)
			}
			_, err = uc.stockUC.ExecuteInTx(ctx, tx, appstock.RecordMovementRequest{
				TenantID:    req.TenantID,
				ProductID:   item.ProductID,
				WarehouseID: warehouseID,
				Direction:   domainstock.DirectionIn, // goods enter the warehouse on return
				Qty:         qty,
				UnitID: func() uuid.UUID {
					if item.UnitID != nil {
						return *item.UnitID
					}
					return uuid.Nil
				}(),
				ConvFactor:    convFactor,
				UnitCost:      unitCost,
				CostStrategy:  "wac",
				ReferenceType: domainstock.RefReturn,
				ReferenceID:   &req.BillID,
			})
			if err != nil {
				return fmt.Errorf("approve return: record movement for item line_no=%d: %w", item.LineNo, err)
			}
			moved++
		}

		if rem != nil && moved == 0 {
			// Another delivery already gave everything back: retire this draft.
			nothingLeft = true
			return uc.repo.UpdateBillStatus(ctx, tx, req.TenantID, req.BillID, domain.StatusCancelled, nil)
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
	if err != nil {
		return err
	}
	if nothingLeft {
		return ErrNothingToReverse
	}
	return nil
}

// returnUnitCost is the per-base-unit cost a returned line re-enters stock at: the cost
// the original sale booked, else the warehouse's current average, else zero (unknown).
func (uc *ApproveReturnBillUseCase) returnUnitCost(ctx context.Context, src returnCostSource, originBill, tenantID, productID, warehouseID uuid.UUID) (decimal.Decimal, error) {
	if src == nil {
		return decimal.Zero, nil
	}
	if originBill != uuid.Nil {
		if c, ok, err := src.SaleOutUnitCost(ctx, tenantID, originBill, productID); err != nil {
			return decimal.Zero, err
		} else if ok {
			return c, nil
		}
	}
	if c, ok, err := src.SnapshotUnitCost(ctx, tenantID, productID, warehouseID); err != nil {
		return decimal.Zero, err
	} else if ok {
		return c, nil
	}
	return decimal.Zero, nil
}
