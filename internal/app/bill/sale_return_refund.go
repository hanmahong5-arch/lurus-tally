package bill

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
	domainpayment "github.com/hanmahong5-arch/lurus-tally/internal/domain/payment"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/loghelper"
)

// sale_return_refund.go pays the customer back at the counter: a return can carry a refund,
// which is booked as a negative payment on the sale. The customer's balance, the receivables
// and the day's takings by payment method all read payments, so they follow without change.

// RefundExceededError means the refund asks for more than the customer has paid beyond what
// the sale is still worth after its returns.
type RefundExceededError struct {
	Max decimal.Decimal
}

func (e *RefundExceededError) Error() string {
	return fmt.Sprintf("bill: refund exceeds what was paid, at most %s", e.Max)
}

// SaleRefundRequest is a sale return plus the money handed back with it.
type SaleRefundRequest struct {
	Return ReturnSaleRequest
	// Amount is the refund; zero makes this a plain return.
	Amount decimal.Decimal
	// Method is how the money goes back (cash, wechat, ...); empty means cash. credit is not
	// a way of paying anyone back.
	Method string
	// onlyReturn, when set, also caps the refund at what that one return still owes the
	// customer (its value less refunds already booked under its number).
	onlyReturn *domain.BillHead
}

// SaleRefundOutput is the approved return and the refund booked with it.
type SaleRefundOutput struct {
	*ReturnSaleOutput
	// Refunded is the amount booked; zero when no refund was asked for or it failed.
	Refunded decimal.Decimal
	// RefundErr is set when the goods went back but the refund could not be booked.
	RefundErr error
}

// SaleRefundUseCase returns part of a sale and optionally refunds money for it.
type SaleRefundUseCase struct {
	repo BillRepo
	ret  *ReturnSaleUseCase
	rec  PaymentRecorder
}

// NewSaleRefundUseCase constructs the use case.
func NewSaleRefundUseCase(repo BillRepo, ret *ReturnSaleUseCase, rec PaymentRecorder) *SaleRefundUseCase {
	return &SaleRefundUseCase{repo: repo, ret: ret, rec: rec}
}

// refundCap is what can still go back to the customer: paid beyond the sale's value net of returns.
func refundCap(total, paid, returned decimal.Decimal) decimal.Decimal {
	return decimal.Max(paid.Sub(decimal.Max(total.Sub(returned), decimal.Zero)), decimal.Zero)
}

// returnedAmountTxSource is returnedAmountSource read on the caller's transaction.
type returnedAmountTxSource interface {
	ReturnedAmountTx(ctx context.Context, tx *sql.Tx, tenantID, saleBillID uuid.UUID) (decimal.Decimal, error)
}

// returnedAmountIn reads the returns inside the transaction that holds the sale's lock when the
// repo can, so the refund cap is checked against committed data and not a stale snapshot.
func (uc *SaleRefundUseCase) returnedAmountIn(ctx context.Context, tx *sql.Tx, tenantID, saleID uuid.UUID) (decimal.Decimal, error) {
	if src, ok := uc.repo.(returnedAmountTxSource); ok {
		return src.ReturnedAmountTx(ctx, tx, tenantID, saleID)
	}
	return uc.returnedAmount(ctx, tenantID, saleID)
}

func (uc *SaleRefundUseCase) returnedAmount(ctx context.Context, tenantID, saleID uuid.UUID) (decimal.Decimal, error) {
	src, ok := uc.repo.(returnedAmountSource)
	if !ok {
		return decimal.Zero, nil
	}
	return src.ReturnedAmount(ctx, tenantID, saleID)
}

// Execute returns the lines, then books the refund. The refund is checked against what the
// customer paid before any stock moves, so an impossible refund fails the whole call.
func (uc *SaleRefundUseCase) Execute(ctx context.Context, req SaleRefundRequest) (*SaleRefundOutput, error) {
	if req.Amount.IsNegative() {
		return nil, fmt.Errorf("%w: refund amount must not be negative", ErrValidation)
	}
	if !req.Amount.IsPositive() {
		out, err := uc.ret.Execute(ctx, req.Return)
		if err != nil {
			return nil, err
		}
		return &SaleRefundOutput{ReturnSaleOutput: out}, nil
	}
	payType, err := resolveRefundMethod(req.Method)
	if err != nil {
		return nil, err
	}
	if err := uc.precheck(ctx, req); err != nil {
		return nil, err
	}

	out, err := uc.ret.Execute(ctx, req.Return)
	if err != nil {
		return nil, err
	}
	res := &SaleRefundOutput{ReturnSaleOutput: out}
	if err := uc.book(ctx, req, out, payType); err != nil {
		loghelper.Error(ctx, "sale_refund_record", err, map[string]any{"bill_id": req.Return.SaleBillID.String()})
		res.RefundErr = err
		return res, nil
	}
	res.Refunded = req.Amount
	return res, nil
}

func resolveRefundMethod(method string) (domainpayment.PayType, error) {
	pt, err := resolvePayType(method)
	if err != nil {
		return "", err
	}
	if pt == domainpayment.PayTypeCredit {
		return "", fmt.Errorf("%w: %q", ErrInvalidPayMethod, method)
	}
	return pt, nil
}

// precheck rejects a refund larger than the cap this return would leave, using the
// current paid amount and returns plus the value of the lines being returned.
func (uc *SaleRefundUseCase) precheck(ctx context.Context, req SaleRefundRequest) error {
	r := req.Return
	sale, err := uc.repo.GetBill(ctx, r.TenantID, r.SaleBillID)
	if err != nil {
		return err
	}
	if sale.BillType != domain.BillTypeSale || sale.SubType == domain.BillSubTypePurchaseReturn || sale.Status != domain.StatusApproved {
		return ErrSaleNotReturnable
	}
	lines, err := uc.repo.GetBillItems(ctx, r.TenantID, r.SaleBillID)
	if err != nil {
		return fmt.Errorf("sale refund: load items: %w", err)
	}
	price := make(map[uuid.UUID]decimal.Decimal, len(lines))
	for _, l := range lines {
		price[l.ID] = l.UnitPrice
	}
	var value decimal.Decimal
	for _, it := range r.Items {
		value = value.Add(it.Qty.Mul(price[it.BillItemID]).Round(2))
	}
	before, err := uc.returnedAmount(ctx, r.TenantID, r.SaleBillID)
	if err != nil {
		return fmt.Errorf("sale refund: %w", err)
	}
	if limit := refundCap(sale.TotalAmount, sale.PaidAmount, before.Add(value)); req.Amount.GreaterThan(limit) {
		return &RefundExceededError{Max: limit}
	}
	return nil
}

// book writes the negative payment and lowers paid_amount, re-checking the cap under the
// sale's lock now that the return itself is counted.
func (uc *SaleRefundUseCase) book(ctx context.Context, req SaleRefundRequest, ret *ReturnSaleOutput, payType domainpayment.PayType) error {
	r := req.Return
	return uc.repo.WithTx(ctx, func(tx *sql.Tx) error {
		if err := uc.repo.AcquireBillAdvisoryLock(ctx, tx, r.TenantID, r.SaleBillID); err != nil {
			return fmt.Errorf("%w: %v", ErrBillApprovalConflict, err)
		}
		sale, err := uc.repo.GetBillForUpdate(ctx, tx, r.TenantID, r.SaleBillID)
		if err != nil {
			return fmt.Errorf("load sale: %w", err)
		}
		returned, err := uc.returnedAmountIn(ctx, tx, r.TenantID, r.SaleBillID)
		if err != nil {
			return err
		}
		if limit := refundCap(sale.TotalAmount, sale.PaidAmount, returned); req.Amount.GreaterThan(limit) {
			return &RefundExceededError{Max: limit}
		}
		if req.onlyReturn != nil {
			left, err := uc.returnRefundLeft(ctx, r.TenantID, r.SaleBillID, req.onlyReturn)
			if err != nil {
				return err
			}
			if req.Amount.GreaterThan(left) {
				return &RefundExceededError{Max: left}
			}
		}
		now := time.Now().UTC()
		p := &domainpayment.Payment{
			ID:        uuid.New(),
			TenantID:  r.TenantID,
			BillID:    r.SaleBillID,
			PayType:   payType,
			Amount:    req.Amount.Neg(),
			PartnerID: sale.PartnerID,
			CreatorID: r.CreatorID,
			PayDate:   now,
			Remark:    "退货退款 " + ret.BillNo,
		}
		if err := uc.rec.Record(ctx, tx, p); err != nil {
			return fmt.Errorf("record refund: %w", err)
		}
		if err := uc.repo.UpdatePaidAmount(ctx, tx, r.TenantID, r.SaleBillID, sale.PaidAmount.Sub(req.Amount)); err != nil {
			return fmt.Errorf("update paid_amount: %w", err)
		}
		return nil
	})
}

// returnRefundLeft is what is still unrefunded of one return: its value less the negative
// payments already labelled with its number.
func (uc *SaleRefundUseCase) returnRefundLeft(ctx context.Context, tenantID, saleID uuid.UUID, ret *domain.BillHead) (decimal.Decimal, error) {
	lister, ok := uc.rec.(interface {
		ListByBill(ctx context.Context, tenantID, billID uuid.UUID) ([]*domainpayment.Payment, error)
	})
	if !ok {
		return ret.TotalAmount, nil
	}
	pays, err := lister.ListByBill(ctx, tenantID, saleID)
	if err != nil {
		return decimal.Zero, fmt.Errorf("list refunds: %w", err)
	}
	left := ret.TotalAmount
	label := "退货退款 " + ret.BillNo
	for _, p := range pays {
		if p.Amount.IsNegative() && p.Remark == label {
			left = left.Add(p.Amount)
		}
	}
	return decimal.Max(left, decimal.Zero), nil
}
