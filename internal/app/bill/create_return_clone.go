package bill

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

// ErrNothingToReverse means a cancelled order has no stock left to give back: the sale
// never shipped (a draft or voided bill) or earlier refunds already returned every unit.
var ErrNothingToReverse = errors.New("bill: nothing to reverse")

// ReturnedLine is the quantity of one product/unit already given back against a sale.
type ReturnedLine struct {
	ProductID uuid.UUID
	UnitID    *uuid.UUID
	Qty       decimal.Decimal
}

// returnedQtySource is an optional capability of BillRepo: it totals what approved sales
// returns (refunds, earlier cancellations, hand-made returns) already gave back against a
// sale. The production repo implements it; doubles that do not treat nothing as returned.
type returnedQtySource interface {
	ReturnedQty(ctx context.Context, tenantID, saleBillID uuid.UUID) ([]ReturnedLine, error)
}

// CloneOriginal copies the not-yet-returned lines and the warehouse of an approved sale
// bill, ready to be returned in full (a cancelled platform order). Quantities already
// returned by refunds are deducted so a refund followed by a cancellation does not put
// the same goods back twice. A sale that never shipped, or is already fully returned,
// yields ErrNothingToReverse. The warehouse falls back to the tenant default when the
// sale never recorded one; with neither, ErrWarehouseRequired is returned up front so no
// orphan draft is left behind to fail at approval.
func (uc *CreateReturnBillUseCase) CloneOriginal(ctx context.Context, tenantID, originalBillID uuid.UUID) (*uuid.UUID, []ReturnItem, error) {
	orig, err := uc.repo.GetBill(ctx, tenantID, originalBillID)
	if err != nil {
		return nil, nil, fmt.Errorf("clone original bill: %w", err)
	}
	if orig.BillType != domain.BillTypeSale || orig.SubType == domain.BillSubTypePurchaseReturn {
		return nil, nil, fmt.Errorf("%w: original bill is not a sale", ErrValidation)
	}
	if orig.Status != domain.StatusApproved {
		// Never shipped (draft, e.g. an oversold order) or already voided: no stock moved.
		return nil, nil, ErrNothingToReverse
	}
	lines, err := uc.repo.GetBillItems(ctx, tenantID, originalBillID)
	if err != nil {
		return nil, nil, fmt.Errorf("clone original bill: load items: %w", err)
	}
	if len(lines) == 0 {
		return nil, nil, fmt.Errorf("%w: original sale has no lines", ErrValidation)
	}

	pool, err := returnedBase(ctx, uc.repo, uc.unitRepo, tenantID, originalBillID)
	if err != nil {
		return nil, nil, fmt.Errorf("clone original bill: %w", err)
	}

	items := make([]ReturnItem, 0, len(lines))
	for _, l := range lines {
		f, err := baseFactor(ctx, uc.unitRepo, l.ProductID, l.UnitID)
		if err != nil {
			return nil, nil, fmt.Errorf("clone original bill: %w", err)
		}
		take := decimal.Min(pool[l.ProductID], l.Qty.Mul(f))
		left := l.Qty
		if take.IsPositive() {
			pool[l.ProductID] = pool[l.ProductID].Sub(take)
			left = l.Qty.Sub(take.Div(f))
		}
		if !left.IsPositive() {
			continue
		}
		items = append(items, ReturnItem{
			ProductID: l.ProductID,
			UnitID:    l.UnitID,
			UnitName:  l.UnitName,
			LineNo:    l.LineNo,
			Qty:       left,
			UnitPrice: l.UnitPrice,
		})
	}
	if len(items) == 0 {
		return nil, nil, ErrNothingToReverse
	}

	warehouseID := orig.WarehouseID
	if warehouseID == nil {
		id, ok, err := uc.repo.DefaultWarehouseID(ctx, tenantID)
		if err != nil {
			return nil, nil, fmt.Errorf("clone original bill: default warehouse: %w", err)
		}
		if !ok {
			return nil, nil, ErrWarehouseRequired
		}
		warehouseID = &id
	}
	return warehouseID, items, nil
}

// importedOrderResolver resolves the sale bill a platform order became.
type importedOrderResolver interface {
	ImportedOrderBillID(ctx context.Context, tenantID uuid.UUID, platform, orderNo string) (uuid.UUID, bool, error)
}

// CapToRemaining trims lines of a return remarked as a cancellation or platform refund
// to what the sale it points at still has left to give back, so a refund that arrives
// after a cancellation (or the other way round) cannot restock the same goods twice.
// Remarks without a resolvable sale leave the lines untouched; when nothing is left it
// yields ErrNothingToReverse.
func (uc *CreateReturnBillUseCase) CapToRemaining(ctx context.Context, tenantID uuid.UUID, remark string, items []ReturnItem) ([]ReturnItem, error) {
	ref, ok := ParseOriginRef(remark)
	if !ok {
		return items, nil
	}
	origin := ref.BillID
	if origin == uuid.Nil {
		res, ok := uc.repo.(importedOrderResolver)
		if !ok {
			return items, nil
		}
		id, found, err := res.ImportedOrderBillID(ctx, tenantID, ref.Platform, ref.PlatformOrderNo)
		if err != nil {
			return nil, fmt.Errorf("cap return: %w", err)
		}
		if !found {
			return items, nil
		}
		origin = id
	}
	rem, isSale, err := remainingBase(ctx, uc.repo, uc.unitRepo, tenantID, origin)
	if err != nil {
		return nil, fmt.Errorf("cap return: %w", err)
	}
	if !isSale {
		return items, nil
	}
	capped := make([]ReturnItem, 0, len(items))
	for _, it := range items {
		f, err := baseFactor(ctx, uc.unitRepo, it.ProductID, it.UnitID)
		if err != nil {
			return nil, fmt.Errorf("cap return: %w", err)
		}
		want := it.Qty.Mul(f)
		got := takeBase(rem, it.ProductID, want)
		if !got.IsPositive() {
			continue
		}
		if got.LessThan(want) {
			it.Qty = got.Div(f)
		}
		capped = append(capped, it)
	}
	if len(capped) == 0 {
		return nil, ErrNothingToReverse
	}
	return capped, nil
}
