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

// UpdatePurchaseDraftRequest is the input to UpdatePurchaseDraftUseCase.
type UpdatePurchaseDraftRequest struct {
	TenantID    uuid.UUID
	BillID      uuid.UUID
	PartnerID   *uuid.UUID
	WarehouseID *uuid.UUID
	BillDate    time.Time
	ShippingFee decimal.Decimal
	TaxAmount   decimal.Decimal
	Remark      string
	Items       []CreatePurchaseItemInput
}

// UpdatePurchaseDraftUseCase replaces items and recalculates totals on a draft bill.
// Only draft bills may be updated; an approved or cancelled bill returns ErrInvalidBillStatus.
type UpdatePurchaseDraftUseCase struct {
	repo BillRepo
}

// NewUpdatePurchaseDraftUseCase constructs the use case.
func NewUpdatePurchaseDraftUseCase(repo BillRepo) *UpdatePurchaseDraftUseCase {
	return &UpdatePurchaseDraftUseCase{repo: repo}
}

// Execute validates and applies the update.
func (uc *UpdatePurchaseDraftUseCase) Execute(ctx context.Context, req UpdatePurchaseDraftRequest) (*domain.BillHead, error) {
	if req.TenantID == uuid.Nil {
		return nil, fmt.Errorf("%w: tenant_id is required", ErrValidation)
	}
	if req.BillID == uuid.Nil {
		return nil, fmt.Errorf("%w: bill_id is required", ErrValidation)
	}
	if len(req.Items) == 0 {
		return nil, fmt.Errorf("%w: at least one item is required", ErrValidation)
	}

	var result *domain.BillHead
	if err := uc.repo.WithTx(ctx, func(tx *sql.Tx) error {
		head, err := uc.repo.GetBillForUpdate(ctx, tx, req.TenantID, req.BillID)
		if err != nil {
			return fmt.Errorf("update purchase draft: %w", err)
		}
		if head.Status != domain.StatusDraft {
			return fmt.Errorf("update purchase draft: %w: only draft bills can be updated", ErrInvalidBillStatus)
		}

		// Rebuild items and recalculate totals.
		items := make([]*domain.BillItem, 0, len(req.Items))
		var subtotal decimal.Decimal
		for _, it := range req.Items {
			if it.ProductID == uuid.Nil {
				return fmt.Errorf("%w: item product_id is required", ErrValidation)
			}
			if it.Qty.IsZero() || it.Qty.IsNegative() {
				return fmt.Errorf("%w: item qty must be positive", ErrValidation)
			}
			lineAmt := it.Qty.Mul(it.UnitPrice).Round(4)
			subtotal = subtotal.Add(lineAmt)
			items = append(items, &domain.BillItem{
				ID:         uuid.New(),
				TenantID:   req.TenantID,
				HeadID:     req.BillID,
				ProductID:  it.ProductID,
				UnitID:     it.UnitID,
				UnitName:   it.UnitName,
				LineNo:     it.LineNo,
				Qty:        it.Qty,
				UnitPrice:  it.UnitPrice,
				LineAmount: lineAmt,
			})
		}

		shippingFee := req.ShippingFee
		taxAmount := req.TaxAmount
		if shippingFee.IsNegative() {
			shippingFee = decimal.Zero
		}
		if taxAmount.IsNegative() {
			taxAmount = decimal.Zero
		}
		totalAmount := subtotal.Add(shippingFee).Add(taxAmount).Round(4)

		billDate := req.BillDate
		if billDate.IsZero() {
			billDate = head.BillDate
		}

		head.PartnerID = req.PartnerID
		head.WarehouseID = req.WarehouseID
		head.BillDate = billDate
		head.Subtotal = subtotal
		head.ShippingFee = shippingFee
		head.TaxAmount = taxAmount
		head.TotalAmount = totalAmount
		head.Remark = req.Remark
		head.UpdatedAt = time.Now().UTC()

		if err := uc.repo.UpdateBill(ctx, tx, head, items); err != nil {
			return fmt.Errorf("update purchase draft: persist: %w", err)
		}
		result = head
		return nil
	}); err != nil {
		return nil, err
	}

	return result, nil
}
