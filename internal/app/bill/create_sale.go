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

// SaleItem describes one line item in a sale draft.
type SaleItem struct {
	ProductID   uuid.UUID
	WarehouseID uuid.UUID
	UnitID      *uuid.UUID
	UnitName    string
	LineNo      int
	Qty         decimal.Decimal
	UnitPrice   decimal.Decimal
	ConvFactor  string
}

// CreateSaleRequest is the input to CreateSaleUseCase.
type CreateSaleRequest struct {
	TenantID    uuid.UUID
	CreatorID   uuid.UUID
	PartnerID   *uuid.UUID
	WarehouseID *uuid.UUID
	BillDate    time.Time
	ShippingFee decimal.Decimal
	TaxAmount   decimal.Decimal
	Remark      string
	Items       []SaleItem
}

// CreateSaleOutput is returned on success.
type CreateSaleOutput struct {
	BillID uuid.UUID
	BillNo string
}

// CreateSaleUseCase creates a new sale bill in draft status.
type CreateSaleUseCase struct {
	repo BillRepo
}

// NewCreateSaleUseCase constructs the use case.
func NewCreateSaleUseCase(repo BillRepo) *CreateSaleUseCase {
	return &CreateSaleUseCase{repo: repo}
}

// Execute validates and persists a new sale draft.
func (uc *CreateSaleUseCase) Execute(ctx context.Context, req CreateSaleRequest) (*CreateSaleOutput, error) {
	if req.TenantID == uuid.Nil {
		return nil, fmt.Errorf("%w: tenant_id is required", ErrValidation)
	}
	if req.CreatorID == uuid.Nil {
		return nil, fmt.Errorf("%w: creator_id is required", ErrValidation)
	}
	if len(req.Items) == 0 {
		return nil, fmt.Errorf("%w: at least one item is required", ErrValidation)
	}
	if req.BillDate.IsZero() {
		req.BillDate = time.Now().UTC()
	}

	items := make([]*domain.BillItem, 0, len(req.Items))
	var subtotal decimal.Decimal
	for _, it := range req.Items {
		if it.ProductID == uuid.Nil {
			return nil, fmt.Errorf("%w: item product_id is required", ErrValidation)
		}
		if it.Qty.IsZero() || it.Qty.IsNegative() {
			return nil, fmt.Errorf("%w: item qty must be positive", ErrValidation)
		}
		if it.UnitPrice.IsNegative() {
			return nil, fmt.Errorf("%w: item unit_price must be non-negative", ErrValidation)
		}
		lineAmt := it.Qty.Mul(it.UnitPrice).Round(4)
		subtotal = subtotal.Add(lineAmt)

		bi := &domain.BillItem{
			ID:         uuid.New(),
			TenantID:   req.TenantID,
			ProductID:  it.ProductID,
			UnitID:     it.UnitID,
			UnitName:   it.UnitName,
			LineNo:     it.LineNo,
			Qty:        it.Qty,
			UnitPrice:  it.UnitPrice,
			LineAmount: lineAmt,
		}
		items = append(items, bi)
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

	billID := uuid.New()
	now := time.Now().UTC()

	var out CreateSaleOutput

	if err := uc.repo.WithTx(ctx, func(tx *sql.Tx) error {
		billNo, err := uc.repo.NextBillNo(ctx, tx, req.TenantID, "SL")
		if err != nil {
			return fmt.Errorf("create sale draft: generate bill_no: %w", err)
		}

		// Resolve warehouse: prefer per-item warehouse if bill-level is absent.
		warehouseID := req.WarehouseID
		if warehouseID == nil && len(req.Items) > 0 && req.Items[0].WarehouseID != uuid.Nil {
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
			PartnerID:   req.PartnerID,
			WarehouseID: warehouseID,
			CreatorID:   req.CreatorID,
			BillDate:    req.BillDate,
			Subtotal:    subtotal,
			ShippingFee: shippingFee,
			TaxAmount:   taxAmount,
			TotalAmount: totalAmount,
			Remark:      req.Remark,
			CreatedAt:   now,
			UpdatedAt:   now,
		}

		for _, item := range items {
			item.HeadID = billID
		}

		if err := uc.repo.CreateBill(ctx, tx, head, items); err != nil {
			return fmt.Errorf("create sale draft: persist bill: %w", err)
		}
		out.BillID = billID
		out.BillNo = billNo
		return nil
	}); err != nil {
		return nil, err
	}

	return &out, nil
}
