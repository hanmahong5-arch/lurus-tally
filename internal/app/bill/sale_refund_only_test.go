package bill_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
)

func refundOnlyReq(f *saleReturnFixture, retID uuid.UUID, amount, method string) appbill.SaleRefundOnlyRequest {
	return appbill.SaleRefundOnlyRequest{
		TenantID: testTenantID, CreatorID: testCreatorID, SaleBillID: f.saleID,
		Amount: decimal.RequireFromString(amount), Method: method, ReturnBillID: retID,
	}
}

// 退货入库后退款没记上：补记一笔负付款，已收降下来，今日结单里现金随之减少。
func TestSaleRefundOnly_BooksTheRefundThatFailed(t *testing.T) {
	f, pay, uc := newRefundFixture("100.00")
	pay.recordErr = errors.New("db down")
	out, err := uc.Execute(context.Background(), refundReq(f, 2, "40", "cash"))
	if err != nil {
		t.Fatalf("return: %v", err)
	}
	pay.recordErr = nil

	if err := uc.RecordRefund(context.Background(), refundOnlyReq(f, out.BillID, "40", "cash")); err != nil {
		t.Fatalf("RecordRefund: %v", err)
	}
	if len(pay.recorded) != 1 || !pay.recorded[0].Amount.Equal(decimal.NewFromInt(-40)) {
		t.Fatalf("payments = %+v, want one of -40", pay.recorded)
	}
	if got := f.repo.billsByID[f.saleID].PaidAmount; !got.Equal(decimal.NewFromInt(60)) {
		t.Errorf("paid after refund = %s, want 60", got)
	}
}

// 防的事故：补记按钮点两次，同一笔钱退两遍。第二次超过上限，被拒绝。
func TestSaleRefundOnly_SecondClickIsRejected(t *testing.T) {
	f, pay, uc := newRefundFixture("100.00")
	pay.recordErr = errors.New("db down")
	out, err := uc.Execute(context.Background(), refundReq(f, 2, "40", "cash"))
	if err != nil {
		t.Fatalf("return: %v", err)
	}
	pay.recordErr = nil
	if err := uc.RecordRefund(context.Background(), refundOnlyReq(f, out.BillID, "40", "cash")); err != nil {
		t.Fatalf("first: %v", err)
	}
	err = uc.RecordRefund(context.Background(), refundOnlyReq(f, out.BillID, "40", "cash"))
	var over *appbill.RefundExceededError
	if !errors.As(err, &over) {
		t.Fatalf("second err = %v, want RefundExceededError", err)
	}
	if len(pay.recorded) != 1 {
		t.Errorf("payments = %d, want 1", len(pay.recorded))
	}
}

func TestSaleRefundOnly_RejectsNonPositiveAndCredit(t *testing.T) {
	f, _, uc := newRefundFixture("100.00")
	if err := uc.RecordRefund(context.Background(), refundOnlyReq(f, uuid.New(), "0", "cash")); !errors.Is(err, appbill.ErrValidation) {
		t.Errorf("zero err = %v, want ErrValidation", err)
	}
	if err := uc.RecordRefund(context.Background(), refundOnlyReq(f, uuid.New(), "10", "credit")); !errors.Is(err, appbill.ErrInvalidPayMethod) {
		t.Errorf("credit err = %v, want ErrInvalidPayMethod", err)
	}
}

// 防的事故：return_bill_no 随便填，付款备注里写出别的单号。现在要传退货单 ID，
// 必须是这张销售单的退货单，备注用服务端查到的单号。
func TestSaleRefundOnly_ReturnMustBelongToTheSale(t *testing.T) {
	f, pay, uc := newRefundFixture("100.00")
	pay.recordErr = errors.New("db down")
	out, err := uc.Execute(context.Background(), refundReq(f, 2, "40", "cash"))
	if err != nil {
		t.Fatalf("return: %v", err)
	}
	pay.recordErr = nil

	for name, id := range map[string]uuid.UUID{"unknown": uuid.New(), "the sale itself": f.saleID} {
		err := uc.RecordRefund(context.Background(), refundOnlyReq(f, id, "40", "cash"))
		if !errors.Is(err, appbill.ErrRefundReturnMismatch) {
			t.Errorf("%s: err = %v, want ErrRefundReturnMismatch", name, err)
		}
	}
	f.repo.linked[out.BillID] = uuid.New() // linked to some other sale
	if err := uc.RecordRefund(context.Background(), refundOnlyReq(f, out.BillID, "40", "cash")); !errors.Is(err, appbill.ErrRefundReturnMismatch) {
		t.Errorf("other sale's return: err = %v, want ErrRefundReturnMismatch", err)
	}
	if len(pay.recorded) != 0 {
		t.Fatalf("payments = %d, want 0", len(pay.recorded))
	}
	f.repo.linked[out.BillID] = f.saleID
	if err := uc.RecordRefund(context.Background(), refundOnlyReq(f, out.BillID, "40", "cash")); err != nil {
		t.Fatalf("own return: %v", err)
	}
	if got := pay.recorded[0].Remark; got != "退货退款 "+out.BillNo {
		t.Errorf("remark = %q, want the return's own number %q", got, out.BillNo)
	}
}

// 防的事故：同一张销售单退了两次，用 A 单补记退款时不能拿到 A+B 的总额，A 也不能补记两遍。
func TestSaleRefundOnly_CappedByTheReturnItself(t *testing.T) {
	f, pay, uc := newRefundFixture("100.00")
	pay.recordErr = errors.New("db down")
	a, err := uc.Execute(context.Background(), refundReq(f, 2, "40", "cash")) // A = 40
	if err != nil {
		t.Fatalf("return A: %v", err)
	}
	if _, err := uc.Execute(context.Background(), refundReq(f, 1, "20", "cash")); err != nil { // B = 20
		t.Fatalf("return B: %v", err)
	}
	pay.recordErr = nil

	var over *appbill.RefundExceededError
	err = uc.RecordRefund(context.Background(), refundOnlyReq(f, a.BillID, "60", "cash"))
	if !errors.As(err, &over) || !over.Max.Equal(decimal.NewFromInt(40)) {
		t.Fatalf("A for 60: err = %v, want RefundExceededError with max 40", err)
	}
	if err := uc.RecordRefund(context.Background(), refundOnlyReq(f, a.BillID, "40", "cash")); err != nil {
		t.Fatalf("A for 40: %v", err)
	}
	err = uc.RecordRefund(context.Background(), refundOnlyReq(f, a.BillID, "10", "cash"))
	if !errors.As(err, &over) || !over.Max.IsZero() {
		t.Fatalf("A again: err = %v, want RefundExceededError with max 0", err)
	}
	if len(pay.recorded) != 1 {
		t.Errorf("payments = %d, want 1", len(pay.recorded))
	}
}
