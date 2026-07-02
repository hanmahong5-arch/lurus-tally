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

// CreateReturnRequest is the input for creating a return-stock draft bill
// (bill_type=入库, sub_type=销售退货).
type CreateReturnRequest struct {
	TenantID    uuid.UUID
	CreatorID   uuid.UUID
	WarehouseID *uuid.UUID
	BillDate    time.Time
	Remark      string // carries audit link to original order/bill
	Items       []ReturnItem
}

// ReturnItem describes one line item in a return-stock draft.
type ReturnItem struct {
	ProductID uuid.UUID
	UnitID    *uuid.UUID
	UnitName  string
	LineNo    int
	Qty       decimal.Decimal
	UnitPrice decimal.Decimal
}

// CreateReturnOutput is returned on success.
type CreateReturnOutput struct {
	BillID uuid.UUID
	BillNo string
}

// CreateReturnBillUseCase creates a new return-stock bill in draft status.
// bill_type=入库, sub_type=销售退货 — stock will be added back when approved.
type CreateReturnBillUseCase struct {
	repo BillRepo
}

// NewCreateReturnBillUseCase constructs the use case.
func NewCreateReturnBillUseCase(repo BillRepo) *CreateReturnBillUseCase {
	return &CreateReturnBillUseCase{repo: repo}
}

// Execute validates and persists a new return-stock draft.
func (uc *CreateReturnBillUseCase) Execute(ctx context.Context, req CreateReturnRequest) (*CreateReturnOutput, error) {
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

		items = append(items, &domain.BillItem{
			ID:         uuid.New(),
			TenantID:   req.TenantID,
			ProductID:  it.ProductID,
			UnitID:     it.UnitID,
			UnitName:   it.UnitName,
			LineNo:     it.LineNo,
			Qty:        it.Qty,
			UnitPrice:  it.UnitPrice,
			LineAmount: lineAmt,
		})
	}

	// Reject any product/warehouse reference outside this tenant before opening
	// the write transaction. validateRefs closes a cross-tenant hole that the
	// bill_item / bill_head FKs miss because FK checks bypass RLS.
	productIDs := make([]uuid.UUID, 0, len(req.Items))
	for _, it := range req.Items {
		productIDs = append(productIDs, it.ProductID)
	}
	if err := validateRefs(ctx, uc.repo, req.TenantID, productIDs, req.WarehouseID); err != nil {
		return nil, err
	}

	billID := uuid.New()
	now := time.Now().UTC()
	var out CreateReturnOutput

	if err := uc.repo.WithTx(ctx, func(tx *sql.Tx) error {
		billNo, err := uc.repo.NextBillNo(ctx, tx, req.TenantID, "RT")
		if err != nil {
			return fmt.Errorf("create return draft: generate bill_no: %w", err)
		}

		head := &domain.BillHead{
			ID:          billID,
			TenantID:    req.TenantID,
			BillNo:      billNo,
			BillType:    domain.BillTypePurchase, // 入库
			SubType:     domain.BillSubTypeSaleReturn,
			Status:      domain.StatusDraft,
			WarehouseID: req.WarehouseID,
			CreatorID:   req.CreatorID,
			BillDate:    req.BillDate,
			Subtotal:    subtotal,
			TotalAmount: subtotal,
			Remark:      req.Remark,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		for _, item := range items {
			item.HeadID = billID
		}
		if err := uc.repo.CreateBill(ctx, tx, head, items); err != nil {
			return fmt.Errorf("create return draft: persist bill: %w", err)
		}
		out.BillID = billID
		out.BillNo = billNo
		return nil
	}); err != nil {
		return nil, err
	}

	return &out, nil
}
