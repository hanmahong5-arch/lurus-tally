package bill

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
	domainpayment "github.com/hanmahong5-arch/lurus-tally/internal/domain/payment"
)

// offsetPayable cancels up to the unpaid part of the purchase with the returned value.
// The offset is booked as a credit-type payment and added to paid_amount, which therefore
// never goes above the purchase total. Without a payment recorder the payable is untouched.
func (uc *PurchaseReturnUseCase) offsetPayable(ctx context.Context, tx *sqlTx, po *domain.BillHead,
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
