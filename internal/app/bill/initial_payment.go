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

// resolvePayType maps the request's payment method to a PayType: empty means "not
// specified" and books as cash, an unknown value is rejected with ErrInvalidPayMethod.
func resolvePayType(method string) (domainpayment.PayType, error) {
	if method == "" {
		return domainpayment.PayTypeCash, nil
	}
	pt := domainpayment.PayType(method)
	if err := pt.Validate(); err != nil {
		return "", fmt.Errorf("%w: %q", ErrInvalidPayMethod, method)
	}
	return pt, nil
}

// checkSettlementMethod rejects "credit" as the method of money actually received or paid:
// credit is a promise to pay later. A zero amount (a credit sale at the counter) is fine.
func checkSettlementMethod(method string, amount decimal.Decimal) error {
	if amount.IsPositive() && method == string(domainpayment.PayTypeCredit) {
		return fmt.Errorf("%w: %q", ErrInvalidPayMethod, method)
	}
	return nil
}

// recordInitialPayment books the payment that rides along with an approval and lifts
// bill_head.paid_amount to it, inside the approval transaction. Shared by sale and
// purchase approval so both write the same payment_head shape.
func recordInitialPayment(ctx context.Context, tx *sqlTx, repo BillRepo, rec PaymentRecorder,
	head *domain.BillHead, tenantID, billID, creatorID uuid.UUID, amount decimal.Decimal, method string, now time.Time, remark string) error {
	payType, err := resolvePayType(method)
	if err != nil {
		return err
	}
	if err := checkSettlementMethod(method, amount); err != nil {
		return err
	}
	p := &domainpayment.Payment{
		ID:        uuid.New(),
		TenantID:  tenantID,
		BillID:    billID,
		PayType:   payType,
		Amount:    amount,
		CreatorID: creatorID,
		PayDate:   now,
		Remark:    remark,
	}
	if head.PartnerID != nil {
		p.PartnerID = head.PartnerID
	}
	if err := rec.Record(ctx, tx, p); err != nil {
		return fmt.Errorf("record payment: %w", err)
	}
	if err := repo.UpdatePaidAmount(ctx, tx, tenantID, billID, amount); err != nil {
		return fmt.Errorf("update paid_amount: %w", err)
	}
	return nil
}
