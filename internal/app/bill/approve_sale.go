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
	domainpayment "github.com/hanmahong5-arch/lurus-tally/internal/domain/payment"
	domainstock "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
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
			// *stock.InsufficientStockError bubbles up for HTTP 422.
			return fmt.Errorf("approve sale: record movement for item line_no=%d: %w", item.LineNo, err)
		}
	}

	// Update bill status to approved.
	now := time.Now().UTC()
	if err := uc.repo.UpdateBillStatus(ctx, tx, req.TenantID, req.BillID, domain.StatusApproved, map[string]any{
		"approved_at": now,
		"approved_by": req.CreatorID,
	}); err != nil {
		return fmt.Errorf("approve sale: update status: %w", err)
	}

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
