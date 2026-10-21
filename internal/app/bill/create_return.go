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
	PartnerID   *uuid.UUID // the customer giving the goods back; nil = none recorded
	// LinkBillID points the return at the sale it gives back (bill_head.link_bill_id).
	// Needs a repo that can write the link; one that cannot rejects the request.
	LinkBillID *uuid.UUID
	BillDate   time.Time
	Remark     string // carries audit link to original order/bill
	Items      []ReturnItem
}

// ReturnItem describes one line item in a return-stock draft.
type ReturnItem struct {
	ProductID uuid.UUID
	UnitID    *uuid.UUID
	UnitName  string
	LineNo    int
	Qty       decimal.Decimal
	UnitPrice decimal.Decimal
	// LinkItemID points the line at the sale line it gives back (bill_item.link_item_id).
	LinkItemID *uuid.UUID
}

// CreateReturnOutput is returned on success.
type CreateReturnOutput struct {
	BillID uuid.UUID
	BillNo string
}

// billLinker is an optional capability of BillRepo: it writes bill_head.link_bill_id so a
// return points back at the sale it gives back. The production repo implements it.
type billLinker interface {
	SetLinkBill(ctx context.Context, tx *sql.Tx, tenantID, billID, linkedBillID uuid.UUID) error
}

// itemLinker is an optional capability of BillRepo: it writes bill_item.link_item_id so a
// return line points back at the sale line it gives back. links maps return line id to
// sale line id; every sale line must belong to saleBillID.
type itemLinker interface {
	SetLinkItems(ctx context.Context, tx *sql.Tx, tenantID, saleBillID uuid.UUID, links map[uuid.UUID]uuid.UUID) error
}

// CreateReturnBillUseCase creates a new return-stock bill in draft status.
// bill_type=入库, sub_type=销售退货 — stock will be added back when approved.
type CreateReturnBillUseCase struct {
	repo     BillRepo
	unitRepo ProductUnitRepo // optional; nil = every line is already in the base unit
}

// NewCreateReturnBillUseCase constructs the use case.
func NewCreateReturnBillUseCase(repo BillRepo) *CreateReturnBillUseCase {
	return &CreateReturnBillUseCase{repo: repo}
}

// WithUnitRepo lets returned quantities be compared across units (a box against
// pieces) when working out what a sale still has left to give back.
func (uc *CreateReturnBillUseCase) WithUnitRepo(unitRepo ProductUnitRepo) *CreateReturnBillUseCase {
	uc.unitRepo = unitRepo
	return uc
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
	itemLinks := map[uuid.UUID]uuid.UUID{}
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
		lineAmt := it.Qty.Mul(it.UnitPrice).Round(2)
		subtotal = subtotal.Add(lineAmt)

		itemID := uuid.New()
		if it.LinkItemID != nil {
			itemLinks[itemID] = *it.LinkItemID
		}
		items = append(items, &domain.BillItem{
			ID:         itemID,
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
			PartnerID:   req.PartnerID,
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
		if req.LinkBillID != nil {
			linker, ok := uc.repo.(billLinker)
			if !ok {
				return fmt.Errorf("create return draft: repo cannot link a return to its sale")
			}
			if err := linker.SetLinkBill(ctx, tx, req.TenantID, billID, *req.LinkBillID); err != nil {
				return fmt.Errorf("create return draft: link sale: %w", err)
			}
		}
		if len(itemLinks) > 0 && req.LinkBillID != nil {
			linker, ok := uc.repo.(itemLinker)
			if !ok {
				return fmt.Errorf("create return draft: repo cannot link return lines to sale lines")
			}
			if err := linker.SetLinkItems(ctx, tx, req.TenantID, *req.LinkBillID, itemLinks); err != nil {
				return fmt.Errorf("create return draft: link sale lines: %w", err)
			}
		}
		out.BillID = billID
		out.BillNo = billNo
		return nil
	}); err != nil {
		return nil, err
	}

	return &out, nil
}
