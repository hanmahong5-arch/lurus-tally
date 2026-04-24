package payment

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
	domainpayment "github.com/hanmahong5-arch/lurus-tally/internal/domain/payment"
)

// RecordPaymentRequest is the input to RecordPaymentUseCase.
type RecordPaymentRequest struct {
	TenantID  uuid.UUID
	BillID    uuid.UUID
	CreatorID uuid.UUID
	Amount    decimal.Decimal
	PayType   string
	Remark    string
}

// RecordPaymentUseCase records an additional payment against an approved bill.
// Prevents concurrent over-payment via SumByBill FOR UPDATE.
type RecordPaymentUseCase struct {
	billRepo    BillReader
	paymentRepo PaymentRepo
}

// NewRecordPaymentUseCase constructs the use case.
func NewRecordPaymentUseCase(billRepo BillReader, paymentRepo PaymentRepo) *RecordPaymentUseCase {
	return &RecordPaymentUseCase{billRepo: billRepo, paymentRepo: paymentRepo}
}

// Execute validates and persists one payment record, updating paid_amount on the bill.
func (uc *RecordPaymentUseCase) Execute(ctx context.Context, req RecordPaymentRequest) error {
	if req.TenantID == uuid.Nil {
		return fmt.Errorf("record payment: tenant_id is required")
	}
	if req.BillID == uuid.Nil {
		return fmt.Errorf("record payment: bill_id is required")
	}
	if req.CreatorID == uuid.Nil {
		return fmt.Errorf("record payment: creator_id is required")
	}
	if req.Amount.IsZero() || req.Amount.IsNegative() {
		return fmt.Errorf("record payment: amount must be positive")
	}

	payType := domainpayment.PayType(req.PayType)
	if err := payType.Validate(); err != nil {
		payType = domainpayment.PayTypeCash // default fallback for invalid/empty
	}

	// We use the bill repo's WithTx so both bill_head and payment_head writes share a transaction.
	return uc.billRepo.WithTx(ctx, func(tx *sql.Tx) error {
		// Load bill with row lock to validate status.
		head, err := uc.billRepo.GetBillForUpdate(ctx, tx, req.TenantID, req.BillID)
		if err != nil {
			return fmt.Errorf("record payment: load bill: %w", err)
		}
		if head.Status != domain.StatusApproved {
			return fmt.Errorf("record payment: bill must be approved; current status is %d", head.Status)
		}

		// Compute new cumulative paid amount (prevents over-payment race condition).
		currentPaid, err := uc.paymentRepo.SumByBill(ctx, tx, req.TenantID, req.BillID)
		if err != nil {
			return fmt.Errorf("record payment: sum by bill: %w", err)
		}
		newPaid := currentPaid.Add(req.Amount)
		if newPaid.GreaterThan(head.TotalAmount) {
			return fmt.Errorf("record payment: total paid (%s) would exceed bill total (%s)", newPaid, head.TotalAmount)
		}

		now := time.Now().UTC()
		p := &domainpayment.Payment{
			ID:        uuid.New(),
			TenantID:  req.TenantID,
			BillID:    req.BillID,
			PayType:   payType,
			Amount:    req.Amount,
			CreatorID: req.CreatorID,
			PayDate:   now,
			Remark:    req.Remark,
			CreatedAt: now,
		}
		if head.PartnerID != nil {
			p.PartnerID = head.PartnerID
		}

		if err := uc.paymentRepo.Record(ctx, tx, p); err != nil {
			return fmt.Errorf("record payment: persist: %w", err)
		}

		// Update cumulative paid_amount on bill_head.
		if err := uc.billRepo.UpdatePaidAmount(ctx, tx, req.TenantID, req.BillID, newPaid); err != nil {
			return fmt.Errorf("record payment: update paid_amount: %w", err)
		}

		return nil
	})
}
