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

// UpdateSaleDraftRequest is the input to UpdateSaleDraftUseCase.
type UpdateSaleDraftRequest struct {
	TenantID    uuid.UUID
	BillID      uuid.UUID
	PartnerID   *uuid.UUID
	WarehouseID *uuid.UUID
	BillDate    time.Time
	ShippingFee decimal.Decimal
	TaxAmount   decimal.Decimal
	Remark      string
	Items       []SaleItem
}

// UpdateSaleDraftUseCase replaces the lines and recalculates the totals of a sale draft.
// Only drafts may be edited: an approved sale has moved stock and money and is corrected
// by a return instead.
type UpdateSaleDraftUseCase struct {
	repo BillRepo
}

// NewUpdateSaleDraftUseCase constructs the use case.
func NewUpdateSaleDraftUseCase(repo BillRepo) *UpdateSaleDraftUseCase {
	return &UpdateSaleDraftUseCase{repo: repo}
}

// Execute validates and applies the update; the bill keeps its number and creator.
func (uc *UpdateSaleDraftUseCase) Execute(ctx context.Context, req UpdateSaleDraftRequest) (*domain.BillHead, error) {
	if req.TenantID == uuid.Nil {
		return nil, fmt.Errorf("%w: tenant_id is required", ErrValidation)
	}
	if req.BillID == uuid.Nil {
		return nil, fmt.Errorf("%w: bill_id is required", ErrValidation)
	}
	if len(req.Items) == 0 {
		return nil, fmt.Errorf("%w: at least one item is required", ErrValidation)
	}
	if req.ShippingFee.IsNegative() {
		return nil, fmt.Errorf("%w: shipping_fee is %s", ErrNegativeFee, req.ShippingFee)
	}
	if req.TaxAmount.IsNegative() {
		return nil, fmt.Errorf("%w: tax_amount is %s", ErrNegativeFee, req.TaxAmount)
	}

	items := make([]*domain.BillItem, 0, len(req.Items))
	var subtotal decimal.Decimal
	productIDs := make([]uuid.UUID, 0, len(req.Items))
	for _, it := range req.Items {
		if it.ProductID == uuid.Nil {
			return nil, fmt.Errorf("%w: item product_id is required", ErrValidation)
		}
		if !it.Qty.IsPositive() {
			return nil, fmt.Errorf("%w: item qty must be positive", ErrValidation)
		}
		if it.UnitPrice.IsNegative() {
			return nil, fmt.Errorf("%w: item unit_price must be non-negative", ErrValidation)
		}
		lineAmt := it.Qty.Mul(it.UnitPrice).Round(2)
		subtotal = subtotal.Add(lineAmt)
		productIDs = append(productIDs, it.ProductID)
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

	// Same fallback as create: a per-line warehouse stands in for a missing bill-level one.
	warehouseID := req.WarehouseID
	if warehouseID == nil && req.Items[0].WarehouseID != uuid.Nil {
		w := req.Items[0].WarehouseID
		warehouseID = &w
	}
	if err := validateRefs(ctx, uc.repo, req.TenantID, productIDs, warehouseID); err != nil {
		return nil, err
	}
	if err := validatePartner(ctx, uc.repo, req.TenantID, req.PartnerID); err != nil {
		return nil, err
	}

	var result *domain.BillHead
	if err := uc.repo.WithTx(ctx, func(tx *sql.Tx) error {
		head, err := uc.repo.GetBillForUpdate(ctx, tx, req.TenantID, req.BillID)
		if err != nil {
			return fmt.Errorf("update sale draft: %w", err)
		}
		if head.BillType != domain.BillTypeSale || head.SubType != domain.BillSubTypeSale {
			return fmt.Errorf("update sale draft: %w", ErrBillNotFound)
		}
		if head.Status != domain.StatusDraft {
			return fmt.Errorf("update sale draft: %w: only draft bills can be updated", ErrInvalidBillStatus)
		}

		billDate := req.BillDate
		if billDate.IsZero() {
			billDate = head.BillDate
		}
		total := subtotal.Add(req.ShippingFee).Add(req.TaxAmount).Round(2)
		head.PartnerID = req.PartnerID
		head.WarehouseID = warehouseID
		head.BillDate = billDate
		head.Subtotal = subtotal
		head.ShippingFee = req.ShippingFee
		head.TaxAmount = req.TaxAmount
		head.TotalAmount = total
		head.AmountLocal = total
		head.Remark = req.Remark
		head.UpdatedAt = time.Now().UTC()

		if err := uc.repo.UpdateBill(ctx, tx, head, items); err != nil {
			return fmt.Errorf("update sale draft: persist: %w", err)
		}
		result = head
		return nil
	}); err != nil {
		return nil, err
	}
	return result, nil
}
