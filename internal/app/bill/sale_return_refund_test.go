package bill_test

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	domainpayment "github.com/hanmahong5-arch/lurus-tally/internal/domain/payment"
)

// newRefundFixture is the sale fixture (5 units at 20.00) with the sale booked at 100.00 and
// the given amount paid, plus a refund recorder.
func newRefundFixture(paid string) (*saleReturnFixture, *mockPaymentRepo, *appbill.SaleRefundUseCase) {
	f := newSaleReturnFixture()
	sale := f.repo.billsByID[f.saleID]
	sale.TotalAmount = decimal.RequireFromString("100.00")
	sale.PaidAmount = decimal.RequireFromString(paid)
	pay := newMockPaymentRepo()
	return f, pay, appbill.NewSaleRefundUseCase(f.repo, f.uc, pay)
}

func refundReq(f *saleReturnFixture, qty int64, amount, method string) appbill.SaleRefundRequest {
	return appbill.SaleRefundRequest{
		Return: appbill.ReturnSaleRequest{
			TenantID: testTenantID, CreatorID: testCreatorID, SaleBillID: f.saleID,
			Items: []appbill.ReturnSaleLine{{BillItemID: f.lineID, Qty: decimal.NewFromInt(qty)}},
		},
		Amount: decimal.RequireFromString(amount),
		Method: method,
	}
}

// 现金付清 100 的单退 2 件（40）并退现金 40：写一条负的现金付款，已收降到 60，
// 单据净额 60 与已收相等，不再显示"应退"。
func TestSaleRefund_CashRefundBooksNegativePayment(t *testing.T) {
	f, pay, uc := newRefundFixture("100.00")
	out, err := uc.Execute(context.Background(), refundReq(f, 2, "40", "cash"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.RefundErr != nil || !out.Refunded.Equal(decimal.NewFromInt(40)) {
		t.Fatalf("refunded=%s err=%v, want 40 / nil", out.Refunded, out.RefundErr)
	}
	if len(pay.recorded) != 1 {
		t.Fatalf("payments = %d, want 1", len(pay.recorded))
	}
	p := pay.recorded[0]
	if !p.Amount.Equal(decimal.NewFromInt(-40)) || p.PayType != domainpayment.PayTypeCash || p.BillID != f.saleID {
		t.Errorf("payment = %+v, want -40 cash on the sale", p)
	}
	if got := f.repo.billsByID[f.saleID].PaidAmount; !got.Equal(decimal.NewFromInt(60)) {
		t.Errorf("paid after refund = %s, want 60", got)
	}
}

// 防的事故：赊账单（没收过钱）也勾了"同时退款"，凭空记出一笔支出。
// 退款超过实收时整单拒绝，库存也不动。
func TestSaleRefund_MoreThanPaidIsRejectedBeforeAnythingMoves(t *testing.T) {
	f, pay, uc := newRefundFixture("0")
	_, err := uc.Execute(context.Background(), refundReq(f, 2, "10", "cash"))
	var over *appbill.RefundExceededError
	if !errors.As(err, &over) || !over.Max.IsZero() {
		t.Fatalf("err = %v, want RefundExceededError with max 0", err)
	}
	if len(f.stockUC.calls) != 0 || len(pay.recorded) != 0 {
		t.Errorf("stock calls=%d payments=%d, want nothing booked", len(f.stockUC.calls), len(pay.recorded))
	}
}

// 已收 100，退 2 件（40），最多能退 40：退 50 要拒绝。
func TestSaleRefund_CapIsPaidBeyondNetTotal(t *testing.T) {
	f, _, uc := newRefundFixture("100.00")
	_, err := uc.Execute(context.Background(), refundReq(f, 2, "50", "cash"))
	var over *appbill.RefundExceededError
	if !errors.As(err, &over) || !over.Max.Equal(decimal.NewFromInt(40)) {
		t.Fatalf("err = %v, want RefundExceededError with max 40", err)
	}
	// 部分退款（只退 15）允许，余下记为"应退"留给店主。
	out, err := uc.Execute(context.Background(), refundReq(f, 2, "15", "wechat"))
	if err != nil || !out.Refunded.Equal(decimal.NewFromInt(15)) {
		t.Fatalf("partial refund: out=%+v err=%v", out, err)
	}
}

func TestSaleRefund_RejectsCreditAndUnknownMethods(t *testing.T) {
	f, _, uc := newRefundFixture("100.00")
	for _, m := range []string{"credit", "bitcoin"} {
		if _, err := uc.Execute(context.Background(), refundReq(f, 1, "10", m)); !errors.Is(err, appbill.ErrInvalidPayMethod) {
			t.Errorf("method %q: err = %v, want ErrInvalidPayMethod", m, err)
		}
	}
	if len(f.stockUC.calls) != 0 {
		t.Errorf("stock moved for a rejected refund method")
	}
}

// 不勾退款：和原来的退货完全一样，不写付款。
func TestSaleRefund_ZeroAmountIsPlainReturn(t *testing.T) {
	f, pay, uc := newRefundFixture("100.00")
	out, err := uc.Execute(context.Background(), refundReq(f, 2, "0", ""))
	if err != nil || !out.Refunded.IsZero() || len(pay.recorded) != 0 {
		t.Fatalf("out=%+v err=%v payments=%d, want a plain return", out, err, len(pay.recorded))
	}
}

// 退货已经入库后记退款失败：退货不回滚，把失败原因带出去让前端提示"退款没记上"。
func TestSaleRefund_RecordFailureKeepsReturnAndReportsIt(t *testing.T) {
	f, pay, uc := newRefundFixture("100.00")
	pay.recordErr = errors.New("db down")
	out, err := uc.Execute(context.Background(), refundReq(f, 2, "40", "cash"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.RefundErr == nil || !out.Refunded.IsZero() {
		t.Errorf("refunded=%s err=%v, want a reported refund failure", out.Refunded, out.RefundErr)
	}
	if len(f.stockUC.calls) != 1 {
		t.Errorf("stock calls = %d, want the return kept", len(f.stockUC.calls))
	}
}
