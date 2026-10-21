package bill

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

// SaleRefundOnlyRequest books a refund for a return that already went through, when the
// refund was not recorded with it (see SaleRefundOutput.RefundErr).
type SaleRefundOnlyRequest struct {
	TenantID   uuid.UUID
	CreatorID  uuid.UUID
	SaleBillID uuid.UUID
	Amount     decimal.Decimal
	// Method is how the money went back; empty means cash.
	Method string
	// ReturnBillID is the return it belongs to; it must give back this very sale, and its
	// number labels the payment.
	ReturnBillID uuid.UUID
}

// ErrRefundReturnMismatch means the return named for a refund is not a return of that sale.
var ErrRefundReturnMismatch = errors.New("bill: return does not belong to this sale")

// RecordRefund books the refund under the same cap as a refund made with the return: the
// customer cannot be paid back more than they paid beyond the sale's value net of returns, so
// a second click on the same refund is refused.
func (uc *SaleRefundUseCase) RecordRefund(ctx context.Context, req SaleRefundOnlyRequest) error {
	if !req.Amount.IsPositive() {
		return fmt.Errorf("%w: refund amount must be positive", ErrValidation)
	}
	payType, err := resolveRefundMethod(req.Method)
	if err != nil {
		return err
	}
	sale, err := uc.repo.GetBill(ctx, req.TenantID, req.SaleBillID)
	if err != nil {
		return err
	}
	if sale.BillType != domain.BillTypeSale || sale.SubType == domain.BillSubTypePurchaseReturn || sale.Status != domain.StatusApproved {
		return ErrSaleNotReturnable
	}
	ret, err := uc.repo.GetBill(ctx, req.TenantID, req.ReturnBillID)
	if err != nil {
		if errors.Is(err, ErrBillNotFound) {
			return ErrRefundReturnMismatch
		}
		return err
	}
	src, ok := uc.repo.(interface {
		LinkedBillID(ctx context.Context, tenantID, billID uuid.UUID) (uuid.UUID, bool, error)
	})
	if !ok || ret.SubType != domain.BillSubTypeSaleReturn || ret.Status != domain.StatusApproved {
		return ErrRefundReturnMismatch
	}
	linked, has, err := src.LinkedBillID(ctx, req.TenantID, ret.ID)
	if err != nil {
		return err
	}
	if !has || linked != req.SaleBillID {
		return ErrRefundReturnMismatch
	}
	return uc.book(ctx,
		SaleRefundRequest{
			Return:     ReturnSaleRequest{TenantID: req.TenantID, CreatorID: req.CreatorID, SaleBillID: req.SaleBillID},
			Amount:     req.Amount,
			onlyReturn: ret,
		},
		&ReturnSaleOutput{BillNo: ret.BillNo}, payType)
}
