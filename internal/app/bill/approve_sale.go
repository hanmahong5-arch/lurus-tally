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

type sqlTx = sql.Tx

var errNoRows = sql.ErrNoRows

// PaymentRecorder is the minimal interface needed to record a payment from the bill layer.
// Defined here to avoid an import cycle between app/bill and app/payment.
type PaymentRecorder interface {
	Record(ctx context.Context, tx *sql.Tx, p *domainpayment.Payment) error
}

// itemPurchasePriceUpdater persists the cost of goods sold onto a sale line so margin
// reports compare sale price against the real WAC/FIFO cost instead of zero.
// It is an optional capability detected on BillRepo at runtime: the production adapter
// implements it, while test doubles that do not are simply skipped. This keeps the cost
// write inside the approval transaction without widening the shared BillRepo interface.
type itemPurchasePriceUpdater interface {
	UpdateItemPurchasePrice(ctx context.Context, tx *sql.Tx, tenantID, itemID uuid.UUID, purchasePrice decimal.Decimal) error
}

// ApproveSaleRequest is the input to ApproveSaleUseCase.
type ApproveSaleRequest struct {
	TenantID   uuid.UUID
	BillID     uuid.UUID
	CreatorID  uuid.UUID
	PaidAmount decimal.Decimal
	PayType    string
	// PaymentRemark rides on the payment booked with the approval (e.g. cash tendered and change).
	PaymentRemark string
	// OccurredAt, when set, is the real time of the sale (offline replay): it dates the stock
	// movements and the payment instead of the approval moment.
	OccurredAt time.Time
	// AllowNegative lets stock go below zero for this approval (offline-till replay only).
	AllowNegative bool
	// OnNegativeStock, when set, is called for every line whose stock ended up below zero.
	OnNegativeStock func(productID uuid.UUID, onHandAfter decimal.Decimal)
}

// ApproveSaleUseCase approves a draft sale bill and records stock-out movements.
// All writes happen in a single database transaction: if any item lacks stock,
// the entire operation rolls back and the bill remains in draft status.
type ApproveSaleUseCase struct {
	repo        BillRepo
	stockUC     StockMovementExecutor
	unitRepo    ProductUnitRepo
	paymentRepo PaymentRecorder
	batches     ReceiptLedger
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

	if req.PaidAmount.IsNegative() {
		return fmt.Errorf("approve sale: %w: paid_amount must be non-negative", ErrValidation)
	}
	if _, err := resolvePayType(req.PayType); err != nil {
		return err
	}

	// Pre-lock idempotency check: if already approved, return nil immediately —
	// unless a payment rides along, which would be silently dropped.
	// The lock-and-recheck inside executeInTx still guards concurrent races.
	if preFetch, err := uc.repo.GetBill(ctx, req.TenantID, req.BillID); err == nil && preFetch.Status == domain.StatusApproved {
		return alreadyApprovedResult(req)
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

// alreadyApprovedResult is the outcome of approving a bill that is already approved:
// a plain repeat is idempotent, but a repeat carrying a payment is rejected because
// that payment is not recorded here.
func alreadyApprovedResult(req ApproveSaleRequest) error {
	if req.PaidAmount.GreaterThan(decimal.Zero) {
		return ErrBillAlreadyApproved
	}
	return nil
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
		return alreadyApprovedResult(req)
	}

	// Payment checks run before any stock movement so a rejected request leaves
	// no trace. Change (paid > total) is the caller's concern and is never booked.
	if req.PaidAmount.IsNegative() {
		return fmt.Errorf("approve sale: %w: paid_amount must be non-negative", ErrValidation)
	}
	if req.PaidAmount.GreaterThan(head.TotalAmount) {
		return fmt.Errorf("approve sale: %w: paid %s, total %s", ErrPaidExceedsTotal, req.PaidAmount, head.TotalAmount)
	}

	if !head.Status.CanTransitionTo(domain.StatusApproved) {
		return fmt.Errorf("approve sale: %w: current status is %d", ErrInvalidBillStatus, head.Status)
	}

	items, err := uc.repo.GetBillItems(ctx, req.TenantID, req.BillID)
	if err != nil {
		return fmt.Errorf("approve sale: load items: %w", err)
	}

	warehouseID, err := resolveApprovalWarehouse(ctx, uc.repo, tx, head, req.TenantID, req.BillID)
	if err != nil {
		return fmt.Errorf("approve sale: %w", err)
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
	var draws []BatchDrawLine
	var lineLinks map[uuid.UUID]uuid.UUID
	if uc.batches != nil {
		if linker, ok := uc.repo.(interface {
			ReturnLinksTx(context.Context, *sql.Tx, uuid.UUID, uuid.UUID) (map[uuid.UUID]uuid.UUID, error)
		}); ok {
			lineLinks, err = linker.ReturnLinksTx(ctx, tx, req.TenantID, req.BillID)
			if err != nil {
				return fmt.Errorf("approve sale: load receipt links: %w", err)
			}
		}
	}

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

		snap, err := uc.stockUC.ExecuteInTx(ctx, tx, appstock.RecordMovementRequest{
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
			OccurredAt:    req.OccurredAt,
			AllowNegative: req.AllowNegative,
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

		if req.AllowNegative && req.OnNegativeStock != nil && snap != nil && snap.OnHandQty.IsNegative() {
			req.OnNegativeStock(item.ProductID, snap.OnHandQty)
		}

		// Persist the cost of goods sold onto the line so margin reports compare the
		// sale price against the real cost instead of zero. snap.UnitCost is the
		// post-movement snapshot cost (base currency); for WAC this equals the cost the
		// goods left at. Skipped when the repo does not implement the optional capability.
		if updater, ok := uc.repo.(itemPurchasePriceUpdater); ok && snap != nil {
			if err := updater.UpdateItemPurchasePrice(ctx, tx, req.TenantID, item.ID, snap.UnitCost); err != nil {
				return fmt.Errorf("approve sale: persist purchase_price for item line_no=%d: %w", item.LineNo, err)
			}
		}
		if uc.batches != nil && snap != nil {
			factor := decimal.NewFromInt(1)
			if f, ferr := decimal.NewFromString(convFactor); ferr == nil && f.IsPositive() {
				factor = f
			}
			line := BatchDrawLine{
				SaleItemID:   item.ID,
				ProductID:    item.ProductID,
				QtyBase:      item.Qty.Mul(factor),
				Factor:       factor,
				SourceItemID: lineLinks[item.ID],
				OnHandAfter:  snap.OnHandQty,
			}
			draws = append(draws, line)
		}
	}

	if len(stockShortages) > 0 {
		return &appstock.BatchInsufficientStockError{Shortages: stockShortages}
	}
	if uc.batches != nil {
		if err := uc.batches.Draw(ctx, tx, req.TenantID, warehouseID, draws, req.AllowNegative); err != nil {
			return fmt.Errorf("approve sale: draw receipt batch: %w", err)
		}
	}

	// Update bill status to approved.
	now := time.Now().UTC()
	payDate := now
	if !req.OccurredAt.IsZero() {
		payDate = req.OccurredAt.UTC()
	}
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
		if err := recordInitialPayment(ctx, tx, uc.repo, uc.paymentRepo, head, req.TenantID, req.BillID, req.CreatorID, req.PaidAmount, req.PayType, payDate, req.PaymentRemark); err != nil {
			return fmt.Errorf("approve sale: %w", err)
		}
	}

	return nil
}
