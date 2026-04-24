package payment

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	domainpayment "github.com/hanmahong5-arch/lurus-tally/internal/domain/payment"
)

// ListPaymentsUseCase lists all payments for a given bill.
type ListPaymentsUseCase struct {
	paymentRepo PaymentRepo
}

// NewListPaymentsUseCase constructs the use case.
func NewListPaymentsUseCase(paymentRepo PaymentRepo) *ListPaymentsUseCase {
	return &ListPaymentsUseCase{paymentRepo: paymentRepo}
}

// Execute returns all payments for the given bill, ordered by pay_date ascending.
func (uc *ListPaymentsUseCase) Execute(ctx context.Context, tenantID, billID uuid.UUID) ([]*domainpayment.Payment, error) {
	if tenantID == uuid.Nil {
		return nil, fmt.Errorf("list payments: tenant_id is required")
	}
	if billID == uuid.Nil {
		return nil, fmt.Errorf("list payments: bill_id is required")
	}
	payments, err := uc.paymentRepo.ListByBill(ctx, tenantID, billID)
	if err != nil {
		return nil, fmt.Errorf("list payments: %w", err)
	}
	if payments == nil {
		payments = []*domainpayment.Payment{}
	}
	return payments, nil
}
