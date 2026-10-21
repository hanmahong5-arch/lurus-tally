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
)

// purchase_return.go hands goods back to a supplier: the clerk picks lines of an approved
// purchase and a quantity, the goods leave stock and what is still owed to the supplier
// drops, all in one transaction. It mirrors the sale return the other way round.

// ErrPurchaseNotReturnable means the bill is not an approved purchase: nothing came into
// the warehouse (a draft or voided bill), or it is another kind of document.
var ErrPurchaseNotReturnable = errors.New("bill: purchase is not returnable")

// PurchaseReturnLine is one line given back: which purchase line, and how many (in the
// unit the line was bought in).
type PurchaseReturnLine struct {
	BillItemID uuid.UUID
	Qty        decimal.Decimal
}

// PurchaseReturnRequest is the input to PurchaseReturnUseCase.
type PurchaseReturnRequest struct {
	TenantID       uuid.UUID
	CreatorID      uuid.UUID
	PurchaseBillID uuid.UUID
	Items          []PurchaseReturnLine
	Remark         string
}

// PurchaseReturnOutput is the approved return bill and what it did to the payable.
type PurchaseReturnOutput struct {
	BillID       uuid.UUID
	BillNo       string
	ReturnAmount decimal.Decimal
	// PayableReduced is the part of the return that cancelled what was still unpaid on the
	// purchase.
	PayableReduced decimal.Decimal
	// SupplierOwes is the rest: money already paid for goods that went back, owed to us by
	// the supplier. The system does not pay it out.
	SupplierOwes decimal.Decimal
}

// purchaseReturnedSource is an optional capability of BillRepo: per purchase line and in the
// line's own unit, what approved returns linked to the purchase already gave back. The
// production repo implements it; doubles that do not treat nothing as returned.
type purchaseReturnedSource interface {
	PurchaseReturnedQtyByLine(ctx context.Context, tenantID, purchaseBillID uuid.UUID) (map[uuid.UUID]decimal.Decimal, error)
}

// PurchaseReturnUseCase returns part of an approved purchase to the supplier.
type PurchaseReturnUseCase struct {
	repo        BillRepo
	stockUC     StockMovementExecutor
	unitRepo    ProductUnitRepo // optional; nil = every line is in the base unit
	paymentRepo PaymentRecorder // optional; nil = the payable is left untouched
}

// NewPurchaseReturnUseCase constructs the use case.
func NewPurchaseReturnUseCase(repo BillRepo, stockUC StockMovementExecutor) *PurchaseReturnUseCase {
	return &PurchaseReturnUseCase{repo: repo, stockUC: stockUC}
}

// WithUnitRepo converts quantities bought in an auxiliary unit to the base unit on the way out.
func (uc *PurchaseReturnUseCase) WithUnitRepo(unitRepo ProductUnitRepo) *PurchaseReturnUseCase {
	uc.unitRepo = unitRepo
	return uc
}

// WithPayments lets a return cancel what is still owed on the purchase: the offset is booked
// as a credit-type payment so it shows in the purchase's payment history.
func (uc *PurchaseReturnUseCase) WithPayments(rec PaymentRecorder) *PurchaseReturnUseCase {
	uc.paymentRepo = rec
	return uc
}

// Execute validates the lines against what the purchase has left, then books the return:
// a linked 采购退货 bill, one stock-out per line (through the stock use case, so the average
// cost stays right) and the payable offset. A shortage of stock fails the whole return.
func (uc *PurchaseReturnUseCase) Execute(ctx context.Context, req PurchaseReturnRequest) (*PurchaseReturnOutput, error) {
	if req.TenantID == uuid.Nil || req.CreatorID == uuid.Nil {
		return nil, fmt.Errorf("%w: tenant_id and creator_id are required", ErrValidation)
	}
	if len(req.Items) == 0 {
		return nil, fmt.Errorf("%w: at least one item is required", ErrValidation)
	}
	for _, it := range req.Items {
		if !it.Qty.IsPositive() {
			return nil, fmt.Errorf("%w: item qty must be positive", ErrValidation)
		}
	}

	var out *PurchaseReturnOutput
	err := uc.repo.WithTx(ctx, func(tx *sql.Tx) error {
		// One return at a time per purchase, so two clicks cannot both pass the cap.
		if err := uc.repo.AcquireBillAdvisoryLock(ctx, tx, req.TenantID, req.PurchaseBillID); err != nil {
			return fmt.Errorf("purchase return: %w: %v", ErrBillApprovalConflict, err)
		}
		po, err := uc.repo.GetBillForUpdate(ctx, tx, req.TenantID, req.PurchaseBillID)
		if err != nil {
			return fmt.Errorf("purchase return: load purchase: %w", err)
		}
		if po.BillType != domain.BillTypePurchase || po.SubType != domain.BillSubTypePurchase || po.Status != domain.StatusApproved {
			return ErrPurchaseNotReturnable
		}
		lines, err := uc.repo.GetBillItems(ctx, req.TenantID, req.PurchaseBillID)
		if err != nil {
			return fmt.Errorf("purchase return: load items: %w", err)
		}
		byID := make(map[uuid.UUID]*domain.BillItem, len(lines))
		for _, l := range lines {
			byID[l.ID] = l
		}

		// One entry per purchase line: repeated ids in the request add up.
		want := make(map[uuid.UUID]decimal.Decimal, len(req.Items))
		order := make([]uuid.UUID, 0, len(req.Items))
		for _, it := range req.Items {
			if _, ok := byID[it.BillItemID]; !ok {
				return ErrReturnItemNotFound
			}
			if _, seen := want[it.BillItemID]; !seen {
				order = append(order, it.BillItemID)
			}
			want[it.BillItemID] = want[it.BillItemID].Add(it.Qty)
		}

		returned, err := uc.returnedByLine(ctx, req.TenantID, req.PurchaseBillID)
		if err != nil {
			return err
		}

		rate := po.ExchangeRateVal
		if !rate.IsPositive() {
			rate = decimal.NewFromInt(1)
		}
		items := make([]*domain.BillItem, 0, len(order))
		links := make(map[uuid.UUID]uuid.UUID, len(order))
		var amount decimal.Decimal
		for _, id := range order {
			l, qty := byID[id], want[id]
			left := decimal.Max(l.Qty.Sub(returned[l.ID]), decimal.Zero)
			if qty.GreaterThan(left) {
				return &ReturnExceededError{LineNo: l.LineNo, Remaining: left}
			}
			// Prices are in the bill's invoice currency; the payable is in base currency.
			lineAmt := qty.Mul(l.UnitPrice).Mul(rate).Round(2)
			amount = amount.Add(lineAmt)
			itemID := uuid.New()
			links[itemID] = l.ID
			items = append(items, &domain.BillItem{
				ID:         itemID,
				TenantID:   req.TenantID,
				ProductID:  l.ProductID,
				UnitID:     l.UnitID,
				UnitName:   l.UnitName,
				LineNo:     len(items) + 1,
				Qty:        qty,
				UnitPrice:  l.UnitPrice,
				LineAmount: lineAmt,
			})
		}

		warehouseID, err := resolveApprovalWarehouse(ctx, uc.repo, tx, po, req.TenantID, req.PurchaseBillID)
		if err != nil {
			return fmt.Errorf("purchase return: %w", err)
		}

		billNo, err := uc.repo.NextBillNo(ctx, tx, req.TenantID, "PR")
		if err != nil {
			return fmt.Errorf("purchase return: generate bill_no: %w", err)
		}
		remark := "采购退货 " + po.BillNo
		if req.Remark != "" {
			remark += "：" + req.Remark
		}
		now := time.Now().UTC()
		billID := uuid.New()
		head := &domain.BillHead{
			ID:          billID,
			TenantID:    req.TenantID,
			BillNo:      billNo,
			BillType:    domain.BillTypeSale, // 出库
			SubType:     domain.BillSubTypePurchaseReturn,
			Status:      domain.StatusDraft,
			WarehouseID: &warehouseID,
			PartnerID:   po.PartnerID,
			CreatorID:   req.CreatorID,
			BillDate:    now,
			Subtotal:    amount,
			TotalAmount: amount,
			Remark:      remark,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		for _, it := range items {
			it.HeadID = billID
		}
		if err := uc.repo.CreateBill(ctx, tx, head, items); err != nil {
			return fmt.Errorf("purchase return: persist bill: %w", err)
		}
		linker, ok := uc.repo.(billLinker)
		if !ok {
			return fmt.Errorf("purchase return: repo cannot link a return to its purchase")
		}
		if err := linker.SetLinkBill(ctx, tx, req.TenantID, billID, req.PurchaseBillID); err != nil {
			return fmt.Errorf("purchase return: link purchase: %w", err)
		}
		itemLinker, ok := uc.repo.(itemLinker)
		if !ok {
			return fmt.Errorf("purchase return: repo cannot link return lines to purchase lines")
		}
		if err := itemLinker.SetLinkItems(ctx, tx, req.TenantID, req.PurchaseBillID, links); err != nil {
			return fmt.Errorf("purchase return: link purchase lines: %w", err)
		}

		for _, it := range items {
			convFactor, factor := "1", decimal.NewFromInt(1)
			if it.UnitID != nil && uc.unitRepo != nil {
				f, err := uc.unitRepo.GetConversionFactor(ctx, it.ProductID, *it.UnitID)
				if err != nil {
					return fmt.Errorf("purchase return: line_no=%d: %w", it.LineNo, err)
				}
				convFactor, factor = f.String(), f
			}
			unitID := uuid.Nil
			if it.UnitID != nil {
				unitID = *it.UnitID
			}
			// Goods go back at the price they came in at (in base currency, per base unit,
			// the way approval costed them), so stock value and the payable fall together.
			if _, err := uc.stockUC.ExecuteInTx(ctx, tx, appstock.RecordMovementRequest{
				TenantID:      req.TenantID,
				ProductID:     it.ProductID,
				WarehouseID:   warehouseID,
				Direction:     domainstock.DirectionOut,
				Qty:           it.Qty,
				UnitID:        unitID,
				ConvFactor:    convFactor,
				UnitCost:      it.UnitPrice.Mul(rate).Div(factor),
				ExplicitCost:  true,
				ReferenceType: domainstock.RefReturn,
				ReferenceID:   &billID,
			}); err != nil {
				return fmt.Errorf("purchase return: stock out for line_no=%d: %w", it.LineNo, err)
			}
		}

		if err := uc.repo.UpdateBillStatus(ctx, tx, req.TenantID, billID, domain.StatusApproved, map[string]any{
			"approved_at": now,
			"approved_by": req.CreatorID,
		}); err != nil {
			return fmt.Errorf("purchase return: update status: %w", err)
		}

		reduced, err := uc.offsetPayable(ctx, tx, po, billNo, req.CreatorID, amount, now)
		if err != nil {
			return err
		}
		middleware.IncBillApproved("purchase_return", req.TenantID.String())
		out = &PurchaseReturnOutput{
			BillID:         billID,
			BillNo:         billNo,
			ReturnAmount:   amount,
			PayableReduced: reduced,
			SupplierOwes:   amount.Sub(reduced),
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// offsetPayable cancels up to the unpaid part of the purchase with the returned value.
// The offset is booked as a credit-type payment and added to paid_amount, which therefore
// never goes above the purchase total. Without a payment recorder the payable is untouched.
func (uc *PurchaseReturnUseCase) offsetPayable(ctx context.Context, tx *sql.Tx, po *domain.BillHead,
	returnNo string, creatorID uuid.UUID, amount decimal.Decimal, now time.Time) (decimal.Decimal, error) {
	if uc.paymentRepo == nil {
		return decimal.Zero, nil
	}
	offset := decimal.Min(amount, decimal.Max(po.TotalAmount.Sub(po.PaidAmount), decimal.Zero))
	if !offset.IsPositive() {
		return decimal.Zero, nil
	}
	p := &domainpayment.Payment{
		ID:        uuid.New(),
		TenantID:  po.TenantID,
		BillID:    po.ID,
		PayType:   domainpayment.PayTypeCredit,
		Amount:    offset,
		PartnerID: po.PartnerID,
		CreatorID: creatorID,
		PayDate:   now,
		Remark:    "采购退货冲抵 " + returnNo,
	}
	if err := uc.paymentRepo.Record(ctx, tx, p); err != nil {
		return decimal.Zero, fmt.Errorf("purchase return: record offset: %w", err)
	}
	if err := uc.repo.UpdatePaidAmount(ctx, tx, po.TenantID, po.ID, po.PaidAmount.Add(offset)); err != nil {
		return decimal.Zero, fmt.Errorf("purchase return: update paid_amount: %w", err)
	}
	return offset, nil
}

func (uc *PurchaseReturnUseCase) returnedByLine(ctx context.Context, tenantID, purchaseID uuid.UUID) (map[uuid.UUID]decimal.Decimal, error) {
	src, ok := uc.repo.(purchaseReturnedSource)
	if !ok {
		return map[uuid.UUID]decimal.Decimal{}, nil
	}
	m, err := src.PurchaseReturnedQtyByLine(ctx, tenantID, purchaseID)
	if err != nil {
		return nil, fmt.Errorf("purchase return: returned quantities: %w", err)
	}
	return m, nil
}

// Summary is what has already gone back per purchase line, in the line's own unit.
// Anything but an approved purchase has nothing returned.
func (uc *PurchaseReturnUseCase) Summary(ctx context.Context, tenantID uuid.UUID, po *domain.BillHead) (map[uuid.UUID]decimal.Decimal, error) {
	if po.BillType != domain.BillTypePurchase || po.SubType != domain.BillSubTypePurchase || po.Status != domain.StatusApproved {
		return map[uuid.UUID]decimal.Decimal{}, nil
	}
	return uc.returnedByLine(ctx, tenantID, po.ID)
}
