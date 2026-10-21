package payment

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
	domainpayment "github.com/hanmahong5-arch/lurus-tally/internal/domain/payment"
)

// ErrInvalidPayType is returned when the payment method is not one of the accepted values.
var ErrInvalidPayType = errors.New("record payment: unsupported payment method")

// ErrCreditNotPayment is returned when "credit" (赊账) is offered as the way money was
// received or paid: credit is a promise to pay later, not a payment.
var ErrCreditNotPayment = errors.New("record payment: credit is not a payment method")

// ErrBillNotApproved is returned when a payment is recorded against a bill that is
// not approved (draft or cancelled).
var ErrBillNotApproved = errors.New("record payment: bill is not approved")

// ErrBillNotPayable is returned when a payment targets a purchase return: that document
// hands goods back to a supplier and is settled by offsetting the purchase, never paid.
var ErrBillNotPayable = errors.New("record payment: bill takes no payments")

// ExceedsOutstandingError is returned when a payment is larger than what is still
// owed on the bill: total minus sales returns minus what is already paid. Its text is
// the user-facing Chinese message ("待收" for sales, "待付" for purchases).
type ExceedsOutstandingError struct {
	Amount      decimal.Decimal
	Outstanding decimal.Decimal
	Payable     bool
}

func (e *ExceedsOutstandingError) Error() string {
	label := "待收"
	if e.Payable {
		label = "待付"
	}
	return fmt.Sprintf("本次 ¥%s 超过%s ¥%s", e.Amount.StringFixed(2), label, e.Outstanding.StringFixed(2))
}

// returnedAmounter is an optional capability of the bill reader: the value of approved
// sales returns against a sale. The production repo implements it; test doubles that
// do not are treated as having no returns.
type returnedAmounter interface {
	ReturnedAmount(ctx context.Context, tenantID, saleBillID uuid.UUID) (decimal.Decimal, error)
}

// returnedAmounterTx is returnedAmounter read on the caller's transaction, so the figure is
// the committed one the bill lock protects. Preferred when the repo has it.
type returnedAmounterTx interface {
	ReturnedAmountTx(ctx context.Context, tx *sql.Tx, tenantID, saleBillID uuid.UUID) (decimal.Decimal, error)
}

// readReturned is the value of approved sales returns against a sale, read inside tx when
// the repo can. Repos with neither capability have no returns.
func readReturned(ctx context.Context, repo BillReader, tx *sql.Tx, tenantID, billID uuid.UUID) (decimal.Decimal, error) {
	if r, ok := repo.(returnedAmounterTx); ok {
		return r.ReturnedAmountTx(ctx, tx, tenantID, billID)
	}
	if r, ok := repo.(returnedAmounter); ok {
		return r.ReturnedAmount(ctx, tenantID, billID)
	}
	return decimal.Zero, nil
}

// resolvePayType maps the request's method to a PayType. Empty means "not specified" and
// defaults to cash; an unknown value is rejected so a typo is never silently booked as cash,
// and credit is rejected because it is not money changing hands.
func resolvePayType(method string) (domainpayment.PayType, error) {
	if method == "" {
		return domainpayment.PayTypeCash, nil
	}
	pt := domainpayment.PayType(method)
	if err := pt.Validate(); err != nil {
		return "", fmt.Errorf("%w: %q", ErrInvalidPayType, method)
	}
	if pt == domainpayment.PayTypeCredit {
		return "", ErrCreditNotPayment
	}
	return pt, nil
}

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
// Prevents concurrent over-payment by locking the bill_head row (GetBillForUpdate)
// before summing existing payments and inserting the new one.
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
	_, err := uc.ExecuteReturning(ctx, req)
	return err
}

// ExecuteReturning is Execute that also hands back the persisted payment.
func (uc *RecordPaymentUseCase) ExecuteReturning(ctx context.Context, req RecordPaymentRequest) (*domainpayment.Payment, error) {
	if req.TenantID == uuid.Nil {
		return nil, fmt.Errorf("record payment: tenant_id is required")
	}
	if req.BillID == uuid.Nil {
		return nil, fmt.Errorf("record payment: bill_id is required")
	}
	if req.CreatorID == uuid.Nil {
		return nil, fmt.Errorf("record payment: creator_id is required")
	}
	if req.Amount.IsZero() || req.Amount.IsNegative() {
		return nil, fmt.Errorf("record payment: amount must be positive")
	}
	if !req.Amount.Equal(req.Amount.Round(2)) {
		return nil, ErrReceiptAmount
	}

	payType, err := resolvePayType(req.PayType)
	if err != nil {
		return nil, err
	}

	// We use the bill repo's WithTx so both bill_head and payment_head writes share a transaction.
	var created *domainpayment.Payment
	err = uc.billRepo.WithTx(ctx, func(tx *sql.Tx) error {
		// A sales return approval locks the original sale with this advisory lock; take it
		// first so the returned amount read below cannot be stale.
		if bl, ok := uc.billRepo.(billLocker); ok {
			if err := bl.AcquireBillAdvisoryLock(ctx, tx, req.TenantID, req.BillID); err != nil {
				return fmt.Errorf("record payment: lock bill: %w", err)
			}
		}
		// Load bill with row lock to validate status.
		head, err := uc.billRepo.GetBillForUpdate(ctx, tx, req.TenantID, req.BillID)
		if err != nil {
			return fmt.Errorf("record payment: load bill: %w", err)
		}
		if head.SubType == domain.BillSubTypePurchaseReturn {
			return ErrBillNotPayable
		}
		if head.Status != domain.StatusApproved {
			return fmt.Errorf("%w: current status is %d", ErrBillNotApproved, head.Status)
		}

		// Compute new cumulative paid amount. The over-payment race is prevented by
		// the bill_head row lock above, not by locking payment_head here.
		currentPaid, err := uc.paymentRepo.SumByBill(ctx, tx, req.TenantID, req.BillID)
		if err != nil {
			return fmt.Errorf("record payment: sum by bill: %w", err)
		}
		amount := req.Amount
		newPaid := currentPaid.Add(amount)

		// The ceiling is the bill total net of approved sales returns, the same balance
		// the sale detail and the receivables ageing show.
		net := head.TotalAmount
		if head.BillType == domain.BillTypeSale {
			returned, err := readReturned(ctx, uc.billRepo, tx, req.TenantID, req.BillID)
			if err != nil {
				return fmt.Errorf("record payment: returned amount: %w", err)
			}
			net = decimal.Max(head.TotalAmount.Sub(returned), decimal.Zero)
		}
		// A bill booked before totals were rounded to the cent can owe a fraction of a
		// cent. Paying the remainder rounded to the cent settles it and books the raw
		// remainder, so no one-cent balance is left behind.
		if left := net.Sub(currentPaid); left.IsPositive() && amount.Equal(left.Round(2)) {
			amount = left
			newPaid = net
		}
		if newPaid.GreaterThan(net) {
			return &ExceedsOutstandingError{
				Amount:      req.Amount,
				Outstanding: decimal.Max(net.Sub(currentPaid), decimal.Zero),
				Payable:     head.BillType == domain.BillTypePurchase,
			}
		}

		now := time.Now().UTC()
		p := &domainpayment.Payment{
			ID:          uuid.New(),
			TenantID:    req.TenantID,
			BillID:      req.BillID,
			PayType:     payType,
			Amount:      amount,
			TotalAmount: amount,
			CreatorID:   req.CreatorID,
			PayDate:     now,
			Remark:      req.Remark,
			CreatedAt:   now,
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

		created = p
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}
