package bill

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
	domainpayment "github.com/hanmahong5-arch/lurus-tally/internal/domain/payment"
	domainstock "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/loghelper"
)

// PaymentRecorder is the minimal interface needed to record a payment from the bill layer.
// Defined here to avoid an import cycle between app/bill and app/payment.
type PaymentRecorder interface {
	Record(ctx context.Context, tx *sql.Tx, p *domainpayment.Payment) error
}

// ApproveSaleRequest is the input to ApproveSaleUseCase.
type ApproveSaleRequest struct {
	TenantID   uuid.UUID
	BillID     uuid.UUID
	CreatorID  uuid.UUID
	PaidAmount decimal.Decimal
	PayType    string
}

// ApproveSaleUseCase approves a draft sale bill and records stock-out movements.
// All writes happen in a single database transaction: if any item lacks stock,
// the entire operation rolls back and the bill remains in draft status.
type ApproveSaleUseCase struct {
	repo        BillRepo
	stockUC     StockMovementExecutor
	unitRepo    ProductUnitRepo
	paymentRepo PaymentRecorder
}

// NewApproveSaleUseCase constructs the use case.
func NewApproveSaleUseCase(
	repo BillRepo,
	stockUC StockMovementExecutor,
	unitRepo ProductUnitRepo,
	paymentRepo PaymentRecorder,
) *ApproveSaleUseCase {
	return &ApproveSaleUseCase{
		repo:        repo,
		stockUC:     stockUC,
		unitRepo:    unitRepo,
		paymentRepo: paymentRepo,
	}
}

// Execute approves the given sale bill within a single transaction.
func (uc *ApproveSaleUseCase) Execute(ctx context.Context, req ApproveSaleRequest) error {
	if req.TenantID == uuid.Nil {
		return fmt.Errorf("approve sale: tenant_id is required")
	}
	if req.BillID == uuid.Nil {
		return fmt.Errorf("approve sale: bill_id is required")
	}

	// Pre-lock idempotency check: if already approved, return nil immediately.
	// The lock-and-recheck inside executeInTx still guards concurrent races.
	if preFetch, err := uc.repo.GetBill(ctx, req.TenantID, req.BillID); err == nil && preFetch.Status == domain.StatusApproved {
		return nil
	}

	return uc.repo.WithTx(ctx, func(tx *sql.Tx) error {
		return uc.executeInTx(ctx, tx, req)
	})
}

// ExecuteInTx approves the sale bill using the provided external transaction.
// Used by QuickCheckoutUseCase to compose create + approve + payment atomically.
func (uc *ApproveSaleUseCase) ExecuteInTx(ctx context.Context, tx *sql.Tx, req ApproveSaleRequest) error {
	return uc.executeInTx(ctx, tx, req)
}

func (uc *ApproveSaleUseCase) executeInTx(ctx context.Context, tx *sql.Tx, req ApproveSaleRequest) error {
	// Acquire advisory lock to prevent concurrent double-approval.
	if err := uc.repo.AcquireBillAdvisoryLock(ctx, tx, req.TenantID, req.BillID); err != nil {
		return fmt.Errorf("approve sale: %w: %v", ErrBillApprovalConflict, err)
	}

	// Load with row lock.
	head, err := uc.repo.GetBillForUpdate(ctx, tx, req.TenantID, req.BillID)
	if err != nil {
		return fmt.Errorf("approve sale: load bill: %w", err)
	}

	// Idempotent: if concurrently approved between pre-lock check and lock acquisition, return nil.
	if head.Status == domain.StatusApproved {
		return nil
	}

	if !head.Status.CanTransitionTo(domain.StatusApproved) {
		return fmt.Errorf("approve sale: %w: current status is %d", ErrInvalidBillStatus, head.Status)
	}

	items, err := uc.repo.GetBillItems(ctx, req.TenantID, req.BillID)
	if err != nil {
		return fmt.Errorf("approve sale: load items: %w", err)
	}

	warehouseID := uuid.Nil
	if head.WarehouseID != nil {
		warehouseID = *head.WarehouseID
	}

	// Record one stock-out movement per line item.
	// Unit cost for outbound = zero; WAC calculator reads current snapshot.unit_cost.
	//
	// D2: collect all insufficient-stock errors across every line before failing.
	// When ExecuteInTx returns *appstock.InsufficientStockError, the movement was
	// NOT applied (ValidateMovement bailed before ApplyMovement), so the snapshot
	// on subsequent items is still accurate. Non-stock errors short-circuit
	// immediately because they indicate a programming or infra failure.
	var stockShortages []appstock.InsufficientStockError

	for _, item := range items {
		convFactor := "1"
		if item.UnitID != nil {
			f, err := uc.unitRepo.GetConversionFactor(ctx, item.ProductID, *item.UnitID)
			if err != nil {
				return fmt.Errorf("approve sale: item line_no=%d: %w", item.LineNo, err)
			}
			convFactor = f.String()
		}

		itemWH := warehouseID // bill-level fallback already resolved above

		_, err := uc.stockUC.ExecuteInTx(ctx, tx, appstock.RecordMovementRequest{
			TenantID:    req.TenantID,
			ProductID:   item.ProductID,
			WarehouseID: itemWH,
			Direction:   domainstock.DirectionOut,
			Qty:         item.Qty,
			UnitID: func() uuid.UUID {
				if item.UnitID != nil {
					return *item.UnitID
				}
				return uuid.Nil
			}(),
			ConvFactor:    convFactor,
			UnitCost:      decimal.Zero, // WAC calculator reads current snapshot cost
			CostStrategy:  "wac",
			ReferenceType: domainstock.RefSale,
			ReferenceID:   &req.BillID,
		})
		if err != nil {
			var ise *appstock.InsufficientStockError
			if errors.As(err, &ise) {
				// Accumulate and continue to the next line so the caller gets
				// the full set of short SKUs in one response.
				stockShortages = append(stockShortages, *ise)
				continue
			}
			// Non-stock error (infra/programming fault) — short-circuit.
			return fmt.Errorf("approve sale: record movement for item line_no=%d: %w", item.LineNo, err)
		}
	}

	if len(stockShortages) > 0 {
		return &appstock.BatchInsufficientStockError{Shortages: stockShortages}
	}

	// Update bill status to approved.
	now := time.Now().UTC()
	if err := uc.repo.UpdateBillStatus(ctx, tx, req.TenantID, req.BillID, domain.StatusApproved, map[string]any{
		"approved_at": now,
		"approved_by": req.CreatorID,
	}); err != nil {
		loghelper.Error(ctx, "bill_approved", err, map[string]any{
			"bill_type": "sale",
			"bill_id":   req.BillID.String(),
			"result":    "failed",
		})
		return fmt.Errorf("approve sale: update status: %w", err)
	}

	middleware.IncBillApproved("sale", req.TenantID.String())
	loghelper.Info(ctx, "bill_approved", map[string]any{
		"bill_type": "sale",
		"bill_id":   req.BillID.String(),
		"result":    "ok",
	})

	// Write initial payment if paid_amount > 0.
	if req.PaidAmount.GreaterThan(decimal.Zero) {
		payType := domainpayment.PayType(req.PayType)
		if err := payType.Validate(); err != nil {
			payType = domainpayment.PayTypeCash // default fallback for invalid/empty
		}
		p := &domainpayment.Payment{
			ID:        uuid.New(),
			TenantID:  req.TenantID,
			BillID:    req.BillID,
			PayType:   payType,
			Amount:    req.PaidAmount,
			CreatorID: req.CreatorID,
			PayDate:   now,
		}
		if head.PartnerID != nil {
			p.PartnerID = head.PartnerID
		}
		if err := uc.paymentRepo.Record(ctx, tx, p); err != nil {
			return fmt.Errorf("approve sale: record payment: %w", err)
		}
		// Update paid_amount on bill_head.
		if err := uc.repo.UpdatePaidAmount(ctx, tx, req.TenantID, req.BillID, req.PaidAmount); err != nil {
			return fmt.Errorf("approve sale: update paid_amount: %w", err)
		}
	}

	return nil
}
