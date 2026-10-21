package bill_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
)

func centDec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

// 12.5 * 3.33 = 41.625: the bill total lands on the cent, so "pay in full" settles it.
func TestCreateSaleDraft_TotalRoundedToTheCent(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewCreateSaleUseCase(repo)
	_, err := uc.Execute(context.Background(), appbill.CreateSaleRequest{
		TenantID: testTenantID, CreatorID: testCreatorID, BillDate: time.Now(),
		Items: []appbill.SaleItem{
			{ProductID: uuid.New(), WarehouseID: uuid.New(), Qty: centDec("12.5"), UnitPrice: centDec("3.33"), LineNo: 1},
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := repo.storedHead.TotalAmount; !got.Equal(centDec("41.63")) {
		t.Errorf("TotalAmount = %s, want 41.63", got)
	}
	if got := repo.storedItems[0].LineAmount; !got.Equal(centDec("41.63")) {
		t.Errorf("LineAmount = %s, want 41.63", got)
	}
	if got := repo.storedItems[0].UnitPrice; !got.Equal(centDec("3.33")) {
		t.Errorf("UnitPrice = %s, want 3.33 (unit price keeps its own precision)", got)
	}
}

// The USD total and its CNY equivalent both carry two decimals.
func TestCreatePurchaseDraft_ForeignTotalsRoundedToTheCent(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewCreatePurchaseDraftUseCase(repo)
	_, err := uc.Execute(context.Background(), appbill.CreatePurchaseDraftRequest{
		TenantID: testTenantID, CreatorID: testCreatorID, BillDate: time.Now(),
		Currency: "USD", ExchangeRate: centDec("7.2531"),
		Items: []appbill.CreatePurchaseItemInput{
			{ProductID: uuid.New(), Qty: centDec("3"), UnitPrice: centDec("1.1111"), LineNo: 1},
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	head := repo.storedHead
	if !head.AmountLocal.Equal(centDec("3.33")) {
		t.Errorf("AmountLocal = %s, want 3.33", head.AmountLocal)
	}
	// 3.33 * 7.2531 = 24.152823 -> 24.15
	if !head.TotalAmount.Equal(centDec("24.15")) {
		t.Errorf("TotalAmount = %s, want 24.15", head.TotalAmount)
	}
}

// POS: 1.5 * 3.33 = 4.995 -> 5.00 total; tendering 5 books 5, no change.
func TestQuickCheckout_FractionalCentTotal_NoPhantomChange(t *testing.T) {
	repo := newMockBillRepo()
	payRepo := newMockPaymentRepo()
	uc := newQuickCheckoutUC(repo, newMockStockUC(), newMockProductUnitRepo(), payRepo)
	items := quickCheckoutItems(0, 1)
	items[0].Qty = centDec("1.5")
	items[0].UnitPrice = centDec("3.33")

	result, err := uc.Execute(context.Background(), appbill.QuickCheckoutRequest{
		TenantID: testTenantID, CreatorID: testCreatorID, PaymentMethod: "cash",
		PaidAmount: decimal.NewFromInt(5), Items: items,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.TotalAmount.Equal(centDec("5")) || !result.ChangeAmount.IsZero() || !result.ReceivableAmount.IsZero() {
		t.Errorf("total=%s change=%s receivable=%s, want 5/0/0", result.TotalAmount, result.ChangeAmount, result.ReceivableAmount)
	}
	if len(payRepo.recorded) != 1 || !payRepo.recorded[0].Amount.Equal(centDec("5.00")) {
		t.Fatalf("payments = %+v, want one of 5.00", payRepo.recorded)
	}
}
