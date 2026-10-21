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

// ErrInvalidUnitForProduct is returned when the item's unit_id is not valid for the product.
var ErrInvalidUnitForProduct = fmt.Errorf("bill: unit_id is not valid for this product")

// resolveApprovalWarehouse returns the warehouse an approval books stock against.
// A bill without one is assigned the tenant default inside tx and written back, so
// the document records where its stock moved. ErrWarehouseRequired when none exists.
func resolveApprovalWarehouse(ctx context.Context, repo BillRepo, tx *sql.Tx, head *domain.BillHead, tenantID, billID uuid.UUID) (uuid.UUID, error) {
	if head.WarehouseID != nil {
		return *head.WarehouseID, nil
	}
	id, ok, err := repo.DefaultWarehouseID(ctx, tenantID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("resolve default warehouse: %w", err)
	}
	if !ok {
		return uuid.Nil, ErrWarehouseRequired
	}
	if err := repo.SetBillWarehouse(ctx, tx, tenantID, billID, id); err != nil {
		return uuid.Nil, fmt.Errorf("set bill warehouse: %w", err)
	}
	return id, nil
}

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
	// paymentRepo is optional; see WithPayments.
	paymentRepo PaymentRecorder
}

// ApprovePurchaseRequest is the input to ExecuteWithPayment. PaidAmount > 0 books a
// payment against the bill in the same transaction as the approval.
type ApprovePurchaseRequest struct {
	TenantID   uuid.UUID
	BillID     uuid.UUID
	ApprovedBy uuid.UUID
	PaidAmount decimal.Decimal
	PayType    string
	// AllowZeroPrice confirms lines with a zero unit price are gifts to be booked at
	// zero cost. Without it such a draft is refused with ErrZeroPriceLines.
	AllowZeroPrice bool
}

// WithPayments enables paying at approval time. Without it a request carrying a
// positive paid amount is refused rather than silently dropped.
func (uc *ApprovePurchaseUseCase) WithPayments(rec PaymentRecorder) *ApprovePurchaseUseCase {
	uc.paymentRepo = rec
	return uc
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
	return uc.ExecuteWithPayment(ctx, ApprovePurchaseRequest{TenantID: tenantID, BillID: billID, ApprovedBy: approvedBy})
}

// ExecuteWithPayment approves the purchase bill and, when PaidAmount > 0, records the
// payment in the same transaction so the payable drops as soon as the goods are booked.
func (uc *ApprovePurchaseUseCase) ExecuteWithPayment(ctx context.Context, req ApprovePurchaseRequest) error {
	tenantID, billID, approvedBy := req.TenantID, req.BillID, req.ApprovedBy
	if tenantID == uuid.Nil {
		return fmt.Errorf("approve purchase: tenant_id is required")
	}
	if billID == uuid.Nil {
		return fmt.Errorf("approve purchase: bill_id is required")
	}

	if req.PaidAmount.IsNegative() {
		return fmt.Errorf("approve purchase: %w: paid_amount must be non-negative", ErrValidation)
	}
	if _, err := resolvePayType(req.PayType); err != nil {
		return err
	}
	if err := checkSettlementMethod(req.PayType, req.PaidAmount); err != nil {
		return err
	}
	if req.PaidAmount.GreaterThan(decimal.Zero) && uc.paymentRepo == nil {
		return fmt.Errorf("approve purchase: payments are not enabled")
	}

	// Pre-lock idempotency check: if the bill is already approved, return nil immediately
	// (unless a payment rides along, which would be silently dropped).
	// This avoids taking the advisory lock on every duplicate-click retry, which is the
	// common case. The lock-and-recheck inside still guards the concurrent race.
	preFetch, err := uc.repo.GetBill(ctx, tenantID, billID)
	if err == nil && preFetch.Status == domain.StatusApproved {
		return alreadyPaidApprovalResult(req.PaidAmount)
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

		// Idempotent: if concurrently approved between pre-lock check and lock acquisition, return nil.
		if head.Status == domain.StatusApproved {
			return alreadyPaidApprovalResult(req.PaidAmount)
		}

		if req.PaidAmount.GreaterThan(head.TotalAmount) {
			return fmt.Errorf("approve purchase: %w: paid %s, total %s", ErrPaidExceedsTotal, req.PaidAmount, head.TotalAmount)
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

		if !req.AllowZeroPrice {
			var zero []ZeroPriceLine
			for _, item := range items {
				if item.UnitPrice.IsZero() {
					zero = append(zero, ZeroPriceLine{LineNo: item.LineNo, ProductID: item.ProductID})
				}
			}
			if len(zero) > 0 {
				return fmt.Errorf("approve purchase: %w", &ZeroPriceLinesError{Lines: zero})
			}
		}

		// Resolve warehouse from bill head, falling back to the tenant default.
		warehouseID, err := resolveApprovalWarehouse(ctx, uc.repo, tx, head, tenantID, billID)
		if err != nil {
			return fmt.Errorf("approve purchase: %w", err)
		}

		// Resolve the FX rate that converts the bill's invoice currency to base (CNY).
		// create_purchase defaults ExchangeRateVal to 1 for CNY bills, but a bill loaded
		// from the DB may carry an unset (zero) rate; in both cases treat the unit price as
		// already base by falling back to a rate of 1. A zero/negative rate is never a valid
		// multiplier, so the guard also protects against corrupt data.
		baseRate := head.ExchangeRateVal
		if baseRate.IsZero() || baseRate.IsNegative() {
			baseRate = decimal.NewFromInt(1)
		}

		// Record one stock movement per line item.
		for _, item := range items {
			// Resolve conversion factor: if no unit_id, assume 1 (already in base unit).
			convFactor, factor := "1", decimal.NewFromInt(1)
			if item.UnitID != nil {
				f, err := uc.unitRepo.GetConversionFactor(ctx, item.ProductID, *item.UnitID)
				if err != nil {
					return fmt.Errorf("approve purchase: item line_no=%d: %w", item.LineNo, err)
				}
				convFactor, factor = f.String(), f
			}

			// Convert the per-unit cost to base currency (CNY) at this boundary so the
			// recorded movement, WAC/FIFO cost and downstream inventory valuation are all
			// expressed in the same currency as bill_head.total_amount (already converted in
			// create_purchase). For a base-currency bill baseRate == 1, so this is a no-op.
			// The price is per purchase unit while the movement quantity is in base units, so
			// the cost is brought down to a base unit as well.
			baseUnitCost := item.UnitPrice.Mul(baseRate).Div(factor)

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
				UnitCost:      baseUnitCost,
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
			loghelper.Error(ctx, "bill_approved", err, map[string]any{
				"bill_type": "purchase",
				"bill_id":   billID.String(),
				"result":    "failed",
			})
			return fmt.Errorf("approve purchase: update status: %w", err)
		}

		middleware.IncBillApproved("purchase", tenantID.String())
		loghelper.Info(ctx, "bill_approved", map[string]any{
			"bill_type": "purchase",
			"bill_id":   billID.String(),
			"result":    "ok",
		})

		if req.PaidAmount.GreaterThan(decimal.Zero) {
			if err := recordInitialPayment(ctx, tx, uc.repo, uc.paymentRepo, head, tenantID, billID, approvedBy, req.PaidAmount, req.PayType, now, ""); err != nil {
				return fmt.Errorf("approve purchase: %w", err)
			}
		}
		return nil
	})
}

// alreadyPaidApprovalResult is the outcome of approving an already-approved purchase:
// a plain repeat is idempotent, a repeat carrying a payment is told it was not booked.
func alreadyPaidApprovalResult(paid decimal.Decimal) error {
	if paid.GreaterThan(decimal.Zero) {
		return ErrBillAlreadyApproved
	}
	return nil
}
