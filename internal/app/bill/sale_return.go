package bill

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/loghelper"
)

// sale_return.go is the shop-floor return: a clerk picks lines of an approved sale and a
// quantity, and the goods go back into stock in one step. It builds on the return-bill
// use cases (draft -> approve) and links the return to the sale it gives back.

var (
	// ErrSaleNotReturnable means the bill is not an approved sale (a draft, a voided bill
	// or a purchase): no stock left the warehouse, so nothing can come back.
	ErrSaleNotReturnable = errors.New("bill: sale is not returnable")

	// ErrReturnItemNotFound means a requested line does not belong to the sale.
	ErrReturnItemNotFound = errors.New("bill: return item not on the sale")

	// ErrWarehouseNotFound means the warehouse a return should go back to is not one of the
	// tenant's.
	ErrWarehouseNotFound = errors.New("bill: warehouse not found")

	// ErrReturnExceedsSold means a requested quantity is more than the sale has left to
	// give back after earlier returns. Carried by *ReturnExceededError.
	ErrReturnExceedsSold = errors.New("bill: return quantity exceeds what was sold")
)

// ReturnExceededError reports how much of a line can still be returned, in the line's unit.
type ReturnExceededError struct {
	LineNo    int
	Remaining decimal.Decimal
}

func (e *ReturnExceededError) Error() string {
	return fmt.Sprintf("%v: line %d has %s left to return", ErrReturnExceedsSold, e.LineNo, e.Remaining)
}

func (e *ReturnExceededError) Unwrap() error { return ErrReturnExceedsSold }

// ReturnSaleLine is one line a customer gives back: which sale line, and how many (in
// the unit the line was sold in).
type ReturnSaleLine struct {
	BillItemID uuid.UUID
	Qty        decimal.Decimal
}

// ReturnSaleRequest is the input to ReturnSaleUseCase.
type ReturnSaleRequest struct {
	TenantID    uuid.UUID
	CreatorID   uuid.UUID
	SaleBillID  uuid.UUID
	Items       []ReturnSaleLine
	WarehouseID *uuid.UUID // nil = the warehouse the sale shipped from
	Remark      string
}

// ReturnSaleOutput is the approved return bill.
type ReturnSaleOutput struct {
	BillID       uuid.UUID
	BillNo       string
	ReturnAmount decimal.Decimal
}

// ReturnApprover approves a draft return bill (ApproveReturnBillUseCase).
type ReturnApprover interface {
	Execute(ctx context.Context, req ApproveReturnRequest) error
}

// returnedAmountSource is an optional capability of BillRepo: the total value of the
// approved returns already booked against a sale.
type returnedAmountSource interface {
	ReturnedAmount(ctx context.Context, tenantID, saleBillID uuid.UUID) (decimal.Decimal, error)
}

// ReturnSaleUseCase returns part of an approved sale to stock.
type ReturnSaleUseCase struct {
	repo     BillRepo
	unitRepo ProductUnitRepo // optional; nil = every line is in the base unit
	create   *CreateReturnBillUseCase
	approve  ReturnApprover
}

// NewReturnSaleUseCase constructs the use case.
func NewReturnSaleUseCase(repo BillRepo, create *CreateReturnBillUseCase, approve ReturnApprover) *ReturnSaleUseCase {
	return &ReturnSaleUseCase{repo: repo, create: create, approve: approve}
}

// WithUnitRepo lets quantities in different units of one product share a single pool.
func (uc *ReturnSaleUseCase) WithUnitRepo(unitRepo ProductUnitRepo) *ReturnSaleUseCase {
	uc.unitRepo = unitRepo
	return uc
}

// Execute validates the lines against what the sale has left, then creates and approves
// the return bill. Stock re-enters at the cost the sale booked (see ApproveReturnBillUseCase).
func (uc *ReturnSaleUseCase) Execute(ctx context.Context, req ReturnSaleRequest) (*ReturnSaleOutput, error) {
	if len(req.Items) == 0 {
		return nil, fmt.Errorf("%w: at least one item is required", ErrValidation)
	}
	sale, err := uc.repo.GetBill(ctx, req.TenantID, req.SaleBillID)
	if err != nil {
		return nil, err
	}
	if sale.BillType != domain.BillTypeSale || sale.SubType == domain.BillSubTypePurchaseReturn || sale.Status != domain.StatusApproved {
		return nil, ErrSaleNotReturnable
	}
	if req.WarehouseID != nil && *req.WarehouseID != uuid.Nil {
		ok, err := uc.repo.WarehouseExists(ctx, req.TenantID, *req.WarehouseID)
		if err != nil {
			return nil, fmt.Errorf("return sale: check warehouse: %w", err)
		}
		if !ok {
			return nil, ErrWarehouseNotFound
		}
	}
	lines, err := uc.repo.GetBillItems(ctx, req.TenantID, req.SaleBillID)
	if err != nil {
		return nil, fmt.Errorf("return sale: load items: %w", err)
	}
	byID := make(map[uuid.UUID]*domain.BillItem, len(lines))
	for _, l := range lines {
		byID[l.ID] = l
	}

	// One entry per sale line: repeated ids in the request add up.
	want := make(map[uuid.UUID]decimal.Decimal, len(req.Items))
	order := make([]uuid.UUID, 0, len(req.Items))
	for _, it := range req.Items {
		if _, ok := byID[it.BillItemID]; !ok {
			return nil, ErrReturnItemNotFound
		}
		if !it.Qty.IsPositive() {
			return nil, fmt.Errorf("%w: item qty must be positive", ErrValidation)
		}
		if _, seen := want[it.BillItemID]; !seen {
			order = append(order, it.BillItemID)
		}
		want[it.BillItemID] = want[it.BillItemID].Add(it.Qty)
	}

	rem, _, err := remainingBase(ctx, uc.repo, uc.unitRepo, req.TenantID, req.SaleBillID)
	if err != nil {
		return nil, fmt.Errorf("return sale: %w", err)
	}
	var lineReturned map[uuid.UUID]decimal.Decimal
	if src, ok := uc.repo.(returnLineSource); ok {
		if lineReturned, err = src.ReturnedQtyByLine(ctx, req.TenantID, req.SaleBillID); err != nil {
			return nil, fmt.Errorf("return sale: %w", err)
		}
	}
	items := make([]ReturnItem, 0, len(order))
	for _, id := range order {
		l, qty := byID[id], want[id]
		f, err := baseFactor(ctx, uc.unitRepo, l.ProductID, l.UnitID)
		if err != nil {
			return nil, fmt.Errorf("return sale: %w", err)
		}
		// A line can give back only what it sold less what earlier returns took from it; the
		// product's remainder is the outer bound for returns that carry no line pointer.
		left := decimal.Min(rem[l.ProductID].Div(f), decimal.Max(l.Qty.Sub(lineReturned[l.ID]), decimal.Zero))
		if qty.GreaterThan(left) {
			return nil, &ReturnExceededError{LineNo: l.LineNo, Remaining: left}
		}
		takeBase(rem, l.ProductID, qty.Mul(f))
		items = append(items, ReturnItem{
			ProductID:  l.ProductID,
			UnitID:     l.UnitID,
			UnitName:   l.UnitName,
			LineNo:     len(items) + 1,
			Qty:        qty,
			UnitPrice:  l.UnitPrice,
			LinkItemID: &l.ID,
		})
	}

	warehouseID := req.WarehouseID
	if warehouseID == nil {
		warehouseID = sale.WarehouseID
	}
	remark := req.Remark
	if remark == "" {
		remark = "销售退货 " + sale.BillNo
	}
	created, err := uc.create.Execute(ctx, CreateReturnRequest{
		TenantID:    req.TenantID,
		CreatorID:   req.CreatorID,
		WarehouseID: warehouseID,
		PartnerID:   sale.PartnerID,
		LinkBillID:  &req.SaleBillID,
		Remark:      remark,
		Items:       items,
	})
	if err != nil {
		return nil, err
	}
	if err := uc.approve.Execute(ctx, ApproveReturnRequest{
		TenantID:  req.TenantID,
		BillID:    created.BillID,
		CreatorID: req.CreatorID,
		Strict:    true,
	}); err != nil {
		uc.discardDraft(ctx, req.TenantID, created.BillID)
		return nil, err
	}

	var amount decimal.Decimal
	for _, it := range items {
		amount = amount.Add(it.Qty.Mul(it.UnitPrice).Round(2))
	}
	return &ReturnSaleOutput{BillID: created.BillID, BillNo: created.BillNo, ReturnAmount: amount}, nil
}

// discardDraft voids a return draft whose approval failed, so no orphan is left in the
// list. Best effort: the error that matters is the approval's.
func (uc *ReturnSaleUseCase) discardDraft(ctx context.Context, tenantID, billID uuid.UUID) {
	head, err := uc.repo.GetBill(ctx, tenantID, billID)
	if err != nil || head.Status != domain.StatusDraft {
		return
	}
	if err := uc.repo.WithTx(ctx, func(tx *sql.Tx) error {
		return uc.repo.UpdateBillStatus(ctx, tx, tenantID, billID, domain.StatusCancelled, nil)
	}); err != nil {
		loghelper.Error(ctx, "return_draft_discard", err, map[string]any{"bill_id": billID.String()})
	}
}

// SaleReturnSummary is what has already come back against a sale.
type SaleReturnSummary struct {
	// ReturnedQty is keyed by sale line id, in the unit the line was sold in. Returns that
	// point at a line are counted on it; the rest (platform cancellations and refunds carry
	// no line pointer) fill the product's lines in order.
	ReturnedQty    map[uuid.UUID]decimal.Decimal
	ReturnedAmount decimal.Decimal
}

// Summary totals the approved returns against sale (whose lines are items).
func (uc *ReturnSaleUseCase) Summary(ctx context.Context, tenantID uuid.UUID, sale *domain.BillHead, items []*domain.BillItem) (SaleReturnSummary, error) {
	out := SaleReturnSummary{ReturnedQty: map[uuid.UUID]decimal.Decimal{}}
	if sale.BillType != domain.BillTypeSale || sale.SubType == domain.BillSubTypePurchaseReturn || sale.Status != domain.StatusApproved {
		return out, nil
	}
	pool, err := returnedBase(ctx, uc.repo, uc.unitRepo, tenantID, sale.ID)
	if err != nil {
		return out, err
	}
	var byLine map[uuid.UUID]decimal.Decimal
	if src, ok := uc.repo.(returnLineSource); ok {
		if byLine, err = src.ReturnedQtyByLine(ctx, tenantID, sale.ID); err != nil {
			return out, err
		}
	}
	factors := make(map[uuid.UUID]decimal.Decimal, len(items))
	for _, l := range items {
		f, err := baseFactor(ctx, uc.unitRepo, l.ProductID, l.UnitID)
		if err != nil {
			return out, err
		}
		factors[l.ID] = f
		linked := decimal.Min(byLine[l.ID], l.Qty)
		if linked.IsPositive() {
			out.ReturnedQty[l.ID] = linked
			pool[l.ProductID] = decimal.Max(pool[l.ProductID].Sub(linked.Mul(f)), decimal.Zero)
		}
	}
	for _, l := range items {
		f := factors[l.ID]
		room := l.Qty.Sub(out.ReturnedQty[l.ID]).Mul(f)
		take := decimal.Min(pool[l.ProductID], room)
		if !take.IsPositive() {
			continue
		}
		pool[l.ProductID] = pool[l.ProductID].Sub(take)
		out.ReturnedQty[l.ID] = out.ReturnedQty[l.ID].Add(take.Div(f))
	}
	if src, ok := uc.repo.(returnedAmountSource); ok {
		if out.ReturnedAmount, err = src.ReturnedAmount(ctx, tenantID, sale.ID); err != nil {
			return out, fmt.Errorf("returned amount: %w", err)
		}
	}
	return out, nil
}
