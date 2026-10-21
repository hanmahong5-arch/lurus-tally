package bill_test

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
)

// TestQuickCheckout_CashTender_WrittenToPaymentRemark verifies the cash handed over and the
// change are kept on the payment, so a reprint from another device can show them.
func TestQuickCheckout_CashTender_WrittenToPaymentRemark(t *testing.T) {
	payRepo := newMockPaymentRepo()
	uc := newQuickCheckoutUC(newMockBillRepo(), newMockStockUC(), newMockProductUnitRepo(), payRepo)

	_, err := uc.Execute(context.Background(), appbill.QuickCheckoutRequest{
		TenantID: testTenantID, CreatorID: testCreatorID, PaymentMethod: "cash",
		PaidAmount: decimal.NewFromInt(100),
		Items:      quickCheckoutItems(40, 2),
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(payRepo.recorded) != 1 {
		t.Fatalf("payments = %d, want 1", len(payRepo.recorded))
	}
	if got, want := payRepo.recorded[0].Remark, "实收 100.00，找零 20.00"; got != want {
		t.Errorf("payment remark = %q, want %q", got, want)
	}
}

// TestQuickCheckout_NonCashPayment_NoTenderRemark verifies change is a cash-drawer matter only.
func TestQuickCheckout_NonCashPayment_NoTenderRemark(t *testing.T) {
	payRepo := newMockPaymentRepo()
	uc := newQuickCheckoutUC(newMockBillRepo(), newMockStockUC(), newMockProductUnitRepo(), payRepo)

	_, err := uc.Execute(context.Background(), appbill.QuickCheckoutRequest{
		TenantID: testTenantID, CreatorID: testCreatorID, PaymentMethod: "wechat",
		PaidAmount: decimal.NewFromInt(80),
		Items:      quickCheckoutItems(40, 2),
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(payRepo.recorded) != 1 || payRepo.recorded[0].Remark != "" {
		t.Errorf("payments = %+v, want one without remark", payRepo.recorded)
	}
}
