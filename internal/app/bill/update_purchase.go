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
	TenantID uuid.UUID
	BillID   uuid.UUID
	// OnlyCreatorID, when set, limits the update to a draft that person created; any
	// other draft answers ErrBillNotFound as if it did not exist (a clerk edits only
	// their own drafts).
	OnlyCreatorID uuid.UUID
	// KeepPrices keeps the prices already on the draft: a line matched by line_no (or
	// product) keeps its unit price, a new line gets 0, and the head keeps its shipping
	// fee and tax. Set for a caller who cannot see prices, so an edit never wipes
	// what the boss or manager filled in.
	KeepPrices  bool
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
	repo     BillRepo
	unitRepo ProductUnitRepo
}

// NewUpdatePurchaseDraftUseCase constructs the use case.
func NewUpdatePurchaseDraftUseCase(repo BillRepo) *UpdatePurchaseDraftUseCase {
	return &UpdatePurchaseDraftUseCase{repo: repo}
}

// WithUnitRepo lets a price-blind edit rescale a kept price when a line's unit changes
// (a box priced per box becomes a piece priced per piece). Without it a unit change
// clears the price instead.
func (uc *UpdatePurchaseDraftUseCase) WithUnitRepo(unitRepo ProductUnitRepo) *UpdatePurchaseDraftUseCase {
	uc.unitRepo = unitRepo
	return uc
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

	// Reject any product/warehouse reference outside this tenant before opening
	// the write transaction (validateRefs — same cross-tenant rationale as create).
	productIDs := make([]uuid.UUID, 0, len(req.Items))
	for _, it := range req.Items {
		productIDs = append(productIDs, it.ProductID)
	}
	if err := validateRefs(ctx, uc.repo, req.TenantID, productIDs, req.WarehouseID); err != nil {
		return nil, err
	}
	if err := validatePartner(ctx, uc.repo, req.TenantID, req.PartnerID); err != nil {
		return nil, err
	}

	var result *domain.BillHead
	if err := uc.repo.WithTx(ctx, func(tx *sql.Tx) error {
		head, err := uc.repo.GetBillForUpdate(ctx, tx, req.TenantID, req.BillID)
		if err != nil {
			return fmt.Errorf("update purchase draft: %w", err)
		}
		if req.OnlyCreatorID != uuid.Nil && head.CreatorID != req.OnlyCreatorID {
			return fmt.Errorf("update purchase draft: %w", ErrBillNotFound)
		}
		if head.Status != domain.StatusDraft {
			return fmt.Errorf("update purchase draft: %w: only draft bills can be updated", ErrInvalidBillStatus)
		}

		var oldItems []*domain.BillItem
		if req.KeepPrices {
			oldItems, err = uc.repo.GetBillItems(ctx, req.TenantID, req.BillID)
			if err != nil {
				return fmt.Errorf("update purchase draft: load items: %w", err)
			}
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
			if req.KeepPrices {
				it.UnitPrice = keptPrice(ctx, uc.unitRepo, oldItems, it)
			}
			lineAmt := it.Qty.Mul(it.UnitPrice).Round(2)
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
		if req.KeepPrices {
			shippingFee, taxAmount = head.ShippingFee, head.TaxAmount
		}
		if shippingFee.IsNegative() {
			shippingFee = decimal.Zero
		}
		if taxAmount.IsNegative() {
			taxAmount = decimal.Zero
		}
		// Original-currency total. Currency and rate stay as stored on the draft
		// (changing currency on update is not supported), so a foreign-currency
		// draft keeps converting at its stored rate.
		amountLocal := subtotal.Add(shippingFee).Add(taxAmount).Round(2)
		totalAmount := amountLocal
		if head.Currency != "" && head.Currency != "CNY" {
			if !head.ExchangeRateVal.IsPositive() {
				return fmt.Errorf("update purchase draft: %w: stored exchange_rate is invalid", ErrValidation)
			}
			totalAmount = amountLocal.Mul(head.ExchangeRateVal).Round(2)
		}

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
		head.AmountLocal = amountLocal
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

// keptPrice is the unit price already stored for it: the old line with the same
// line_no and product, else the first old line of that product, else zero. When the
// line's unit changed, the price is rescaled to the new unit (old * newFactor /
// oldFactor, both factors being base units per unit); if either factor cannot be
// resolved it is zero, left for the boss to fill in rather than guessed.
func keptPrice(ctx context.Context, units ProductUnitRepo, old []*domain.BillItem, it CreatePurchaseItemInput) decimal.Decimal {
	var match *domain.BillItem
	for _, o := range old {
		if o.LineNo == it.LineNo && o.ProductID == it.ProductID {
			match = o
			break
		}
	}
	if match == nil {
		for _, o := range old {
			if o.ProductID == it.ProductID {
				match = o
				break
			}
		}
	}
	if match == nil {
		return decimal.Zero
	}
	if sameUnit(match.UnitID, it.UnitID) {
		return match.UnitPrice
	}
	from, ok := unitFactor(ctx, units, it.ProductID, match.UnitID)
	if !ok {
		return decimal.Zero
	}
	to, ok := unitFactor(ctx, units, it.ProductID, it.UnitID)
	if !ok {
		return decimal.Zero
	}
	return match.UnitPrice.Mul(to).Div(from).Round(6)
}

// sameUnit reports whether two optional unit ids name the same unit (nil is the base unit).
func sameUnit(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// unitFactor is the base units in one unit of the product: 1 for the base unit (nil),
// else the looked-up factor. ok is false when it cannot be resolved to a positive number.
func unitFactor(ctx context.Context, units ProductUnitRepo, productID uuid.UUID, unitID *uuid.UUID) (decimal.Decimal, bool) {
	if unitID == nil {
		return decimal.NewFromInt(1), true
	}
	if units == nil {
		return decimal.Zero, false
	}
	f, err := units.GetConversionFactor(ctx, productID, *unitID)
	if err != nil || !f.IsPositive() {
		return decimal.Zero, false
	}
	return f, true
}
