package bill

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

// return_strict.go is the all-or-nothing cap a shop-floor return gets at approval.

// returnLineSource is an optional capability of BillRepo: it reads the sale-line pointers
// of a return and totals approved returns per sale line. Repos without it fall back to the
// per-product cap alone.
type returnLineSource interface {
	// ReturnLinks maps each line of billID that points at a sale line to that sale line.
	ReturnLinks(ctx context.Context, tenantID, billID uuid.UUID) (map[uuid.UUID]uuid.UUID, error)
	// ReturnedQtyByLine totals, per sale line and in the line's own unit, what approved
	// returns linked to saleBillID already gave back.
	ReturnedQtyByLine(ctx context.Context, tenantID, saleBillID uuid.UUID) (map[uuid.UUID]decimal.Decimal, error)
}

// strictReturnCheck holds what the sale has left, per line, while one return is approved.
type strictReturnCheck struct {
	saleLines map[uuid.UUID]*domain.BillItem
	links     map[uuid.UUID]uuid.UUID
	returned  map[uuid.UUID]decimal.Decimal
}

func (uc *ApproveReturnBillUseCase) newStrictCheck(ctx context.Context, tenantID, billID, saleID uuid.UUID) (*strictReturnCheck, error) {
	chk := &strictReturnCheck{}
	src, ok := uc.repo.(returnLineSource)
	if !ok {
		return chk, nil
	}
	var err error
	if chk.links, err = src.ReturnLinks(ctx, tenantID, billID); err != nil {
		return nil, fmt.Errorf("return links: %w", err)
	}
	if chk.returned, err = src.ReturnedQtyByLine(ctx, tenantID, saleID); err != nil {
		return nil, fmt.Errorf("returned qty by line: %w", err)
	}
	lines, err := uc.repo.GetBillItems(ctx, tenantID, saleID)
	if err != nil {
		return nil, fmt.Errorf("sale items: %w", err)
	}
	chk.saleLines = make(map[uuid.UUID]*domain.BillItem, len(lines))
	for _, l := range lines {
		chk.saleLines[l.ID] = l
	}
	return chk, nil
}

// take draws one return line from both the sale line it points at and the product's
// remainder (factor = base units per unit of the line). A shortfall in either is an error.
func (c *strictReturnCheck) take(rem map[uuid.UUID]decimal.Decimal, item *domain.BillItem, factor decimal.Decimal) error {
	if link, ok := c.links[item.ID]; ok {
		if sl := c.saleLines[link]; sl != nil {
			left := decimal.Max(sl.Qty.Sub(c.returned[link]), decimal.Zero)
			if item.Qty.GreaterThan(left) {
				return &ReturnExceededError{LineNo: sl.LineNo, Remaining: left}
			}
		}
	}
	want := item.Qty.Mul(factor)
	if got := takeBase(rem, item.ProductID, want); got.LessThan(want) {
		// give back what the draw took so the error reports a consistent remainder
		rem[item.ProductID] = rem[item.ProductID].Add(got)
		lineNo := item.LineNo
		if sl := c.saleLines[c.links[item.ID]]; sl != nil {
			lineNo = sl.LineNo
		}
		return &ReturnExceededError{LineNo: lineNo, Remaining: got.Div(factor)}
	}
	return nil
}
