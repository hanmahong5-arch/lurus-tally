package bill

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

// return_remaining.go answers "how much of this sale can still go back into stock",
// in base units so lines entered in a box and lines entered in pieces share one pool.

// baseFactor is the number of base units in one unit of (product, unit); 1 when the
// line is already in the base unit or no unit repo is wired.
func baseFactor(ctx context.Context, unitRepo ProductUnitRepo, productID uuid.UUID, unitID *uuid.UUID) (decimal.Decimal, error) {
	if unitID == nil || unitRepo == nil {
		return decimal.NewFromInt(1), nil
	}
	f, err := unitRepo.GetConversionFactor(ctx, productID, *unitID)
	if err != nil {
		return decimal.Zero, err
	}
	if !f.IsPositive() {
		return decimal.NewFromInt(1), nil
	}
	return f, nil
}

// returnedBase totals, per product and in base units, what approved returns already
// gave back against saleBillID. Repos without the capability report nothing returned.
func returnedBase(ctx context.Context, repo BillRepo, unitRepo ProductUnitRepo, tenantID, saleBillID uuid.UUID) (map[uuid.UUID]decimal.Decimal, error) {
	pool := map[uuid.UUID]decimal.Decimal{}
	src, ok := repo.(returnedQtySource)
	if !ok {
		return pool, nil
	}
	returned, err := src.ReturnedQty(ctx, tenantID, saleBillID)
	if err != nil {
		return nil, fmt.Errorf("returned qty: %w", err)
	}
	for _, r := range returned {
		f, err := baseFactor(ctx, unitRepo, r.ProductID, r.UnitID)
		if err != nil {
			return nil, fmt.Errorf("returned qty: %w", err)
		}
		pool[r.ProductID] = pool[r.ProductID].Add(r.Qty.Mul(f))
	}
	return pool, nil
}

// remainingBase is, per product and in base units, what the sale shipped minus what
// approved returns already gave back. ok is false when originBillID is not a sale
// (nothing to cap against). A sale that never shipped has an empty remainder.
func remainingBase(ctx context.Context, repo BillRepo, unitRepo ProductUnitRepo, tenantID, originBillID uuid.UUID) (map[uuid.UUID]decimal.Decimal, bool, error) {
	orig, err := repo.GetBill(ctx, tenantID, originBillID)
	if errors.Is(err, ErrBillNotFound) {
		return nil, false, nil // a dangling pointer caps nothing
	}
	if err != nil {
		return nil, false, fmt.Errorf("remaining qty: load sale: %w", err)
	}
	if orig.BillType != domain.BillTypeSale || orig.SubType == domain.BillSubTypePurchaseReturn {
		return nil, false, nil
	}
	rem := map[uuid.UUID]decimal.Decimal{}
	if orig.Status != domain.StatusApproved {
		return rem, true, nil
	}
	lines, err := repo.GetBillItems(ctx, tenantID, originBillID)
	if err != nil {
		return nil, false, fmt.Errorf("remaining qty: load items: %w", err)
	}
	for _, l := range lines {
		f, err := baseFactor(ctx, unitRepo, l.ProductID, l.UnitID)
		if err != nil {
			return nil, false, fmt.Errorf("remaining qty: %w", err)
		}
		rem[l.ProductID] = rem[l.ProductID].Add(l.Qty.Mul(f))
	}
	returned, err := returnedBase(ctx, repo, unitRepo, tenantID, originBillID)
	if err != nil {
		return nil, false, err
	}
	for p, q := range returned {
		rem[p] = decimal.Max(rem[p].Sub(q), decimal.Zero)
	}
	return rem, true, nil
}

// takeBase draws wantBase from the product's remainder and returns the base quantity
// actually granted (never more than what is left).
func takeBase(rem map[uuid.UUID]decimal.Decimal, productID uuid.UUID, wantBase decimal.Decimal) decimal.Decimal {
	take := decimal.Min(rem[productID], wantBase)
	if !take.IsPositive() {
		return decimal.Zero
	}
	rem[productID] = rem[productID].Sub(take)
	return take
}
