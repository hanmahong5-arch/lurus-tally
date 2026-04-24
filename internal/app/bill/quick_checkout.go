package bill

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

// QuickCheckoutRequest is the input to QuickCheckoutUseCase.
// Designed for POS single-step: create draft + approve + record payment in one transaction.
type QuickCheckoutRequest struct {
	TenantID      uuid.UUID
	CreatorID     uuid.UUID
	CustomerName  string // stored as remark in bill_head
	Items         []SaleItem
	PaymentMethod string
	PaidAmount    decimal.Decimal
}

// QuickCheckoutResult is returned on success.
type QuickCheckoutResult struct {
	BillID           uuid.UUID
	BillNo           string
	TotalAmount      decimal.Decimal
	ReceivableAmount decimal.Decimal
}

// QuickCheckoutUseCase performs a single-transaction POS checkout:
// create sale draft → approve (stock out) → record payment.
type QuickCheckoutUseCase struct {
	repo      BillRepo
	approveUC *ApproveSaleUseCase
}

// NewQuickCheckoutUseCase constructs the use case.
func NewQuickCheckoutUseCase(repo BillRepo, approveUC *ApproveSaleUseCase) *QuickCheckoutUseCase {
	return &QuickCheckoutUseCase{
		repo:      repo,
		approveUC: approveUC,
	}
}

// Execute performs create + approve + payment in a single transaction.
func (uc *QuickCheckoutUseCase) Execute(ctx context.Context, req QuickCheckoutRequest) (*QuickCheckoutResult, error) {
	if req.TenantID == uuid.Nil {
		return nil, fmt.Errorf("%w: tenant_id is required", ErrValidation)
	}
	if req.CreatorID == uuid.Nil {
		return nil, fmt.Errorf("%w: creator_id is required", ErrValidation)
	}
	if len(req.Items) == 0 {
		return nil, fmt.Errorf("%w: at least one item is required", ErrValidation)
	}

	var result QuickCheckoutResult

	if err := uc.repo.WithTx(ctx, func(tx *sql.Tx) error {
		billID := uuid.New()
		billNo, err := uc.repo.NextBillNo(ctx, tx, req.TenantID, "SL")
		if err != nil {
			return fmt.Errorf("quick checkout: generate bill_no: %w", err)
		}

		// Build items and totals (shared logic with CreateSaleUseCase).
		billItems, totalAmount, err := assembleSaleItems(req.TenantID, billID, req.Items)
		if err != nil {
			return fmt.Errorf("quick checkout: validate items: %w", err)
		}

		now := time.Now().UTC()

		// Resolve warehouse from first item when not set at bill level.
		var warehouseID *uuid.UUID
		if len(req.Items) > 0 && req.Items[0].WarehouseID != uuid.Nil {
			w := req.Items[0].WarehouseID
			warehouseID = &w
		}

		head := &domain.BillHead{
			ID:          billID,
			TenantID:    req.TenantID,
			BillNo:      billNo,
			BillType:    domain.BillTypeSale,
			SubType:     domain.BillSubTypeSale,
			Status:      domain.StatusDraft,
			WarehouseID: warehouseID,
			CreatorID:   req.CreatorID,
			BillDate:    now,
			Subtotal:    totalAmount,
			TotalAmount: totalAmount,
			Remark:      req.CustomerName,
			CreatedAt:   now,
			UpdatedAt:   now,
		}

		if err := uc.repo.CreateBill(ctx, tx, head, billItems); err != nil {
			return fmt.Errorf("quick checkout: create bill: %w", err)
		}

		// Approve (stock out movements + status update + optional payment).
		if err := uc.approveUC.ExecuteInTx(ctx, tx, ApproveSaleRequest{
			TenantID:   req.TenantID,
			BillID:     billID,
			CreatorID:  req.CreatorID,
			PaidAmount: req.PaidAmount,
			PayType:    req.PaymentMethod,
		}); err != nil {
			return fmt.Errorf("quick checkout: approve: %w", err)
		}

		receivable := totalAmount.Sub(req.PaidAmount)
		if receivable.IsNegative() {
			receivable = decimal.Zero
		}

		result = QuickCheckoutResult{
			BillID:           billID,
			BillNo:           billNo,
			TotalAmount:      totalAmount,
			ReceivableAmount: receivable,
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return &result, nil
}

// assembleSaleItems converts SaleItem slice to domain.BillItem slice and returns total amount.
func assembleSaleItems(tenantID, billID uuid.UUID, items []SaleItem) ([]*domain.BillItem, decimal.Decimal, error) {
	out := make([]*domain.BillItem, 0, len(items))
	var total decimal.Decimal
	for _, it := range items {
		if it.ProductID == uuid.Nil {
			return nil, decimal.Zero, fmt.Errorf("%w: item product_id is required", ErrValidation)
		}
		if it.Qty.IsZero() || it.Qty.IsNegative() {
			return nil, decimal.Zero, fmt.Errorf("%w: item qty must be positive", ErrValidation)
		}
		if it.UnitPrice.IsNegative() {
			return nil, decimal.Zero, fmt.Errorf("%w: item unit_price must be non-negative", ErrValidation)
		}
		lineAmt := it.Qty.Mul(it.UnitPrice).Round(4)
		total = total.Add(lineAmt)
		out = append(out, &domain.BillItem{
			ID:         uuid.New(),
			TenantID:   tenantID,
			HeadID:     billID,
			ProductID:  it.ProductID,
			UnitID:     it.UnitID,
			UnitName:   it.UnitName,
			LineNo:     it.LineNo,
			Qty:        it.Qty,
			UnitPrice:  it.UnitPrice,
			LineAmount: lineAmt,
		})
	}
	return out, total.Round(4), nil
}
