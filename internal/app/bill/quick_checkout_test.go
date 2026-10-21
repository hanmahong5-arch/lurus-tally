package bill_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
)

func newQuickCheckoutUC(repo *mockBillRepo, stockUC *mockStockUC, unitRepo *mockProductUnitRepo, payRepo *mockPaymentRepo) *appbill.QuickCheckoutUseCase {
	approveUC := appbill.NewApproveSaleUseCase(repo, stockUC, unitRepo, payRepo)
	return appbill.NewQuickCheckoutUseCase(repo, approveUC)
}

// TestQuickCheckout_ValidRequest_ReturnsBillID verifies the happy path returns a non-nil bill ID.
func TestQuickCheckout_ValidRequest_ReturnsBillID(t *testing.T) {
	repo := newMockBillRepo()
	stockUC := newMockStockUC()
	unitRepo := newMockProductUnitRepo()
	payRepo := newMockPaymentRepo()

	req := appbill.QuickCheckoutRequest{
		TenantID:      testTenantID,
		CreatorID:     testCreatorID,
		CustomerName:  "Walk-in Customer",
		PaymentMethod: "cash",
		PaidAmount:    decimal.NewFromFloat(50),
		Items: []appbill.SaleItem{
			{
				ProductID:   uuid.New(),
				WarehouseID: uuid.New(),
				Qty:         decimal.NewFromFloat(2),
				UnitPrice:   decimal.NewFromFloat(25),
				LineNo:      1,
			},
		},
	}

	uc := newQuickCheckoutUC(repo, stockUC, unitRepo, payRepo)
	result, err := uc.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if result.BillID == uuid.Nil {
		t.Error("BillID is nil UUID")
	}
	if result.BillNo == "" {
		t.Error("BillNo is empty")
	}
	wantTotal := decimal.NewFromFloat(50)
	if !result.TotalAmount.Equal(wantTotal) {
		t.Errorf("TotalAmount = %s, want %s", result.TotalAmount, wantTotal)
	}
	if !result.ReceivableAmount.IsZero() {
		t.Errorf("ReceivableAmount = %s, want 0 (fully paid)", result.ReceivableAmount)
	}

	// stock movement should have been called
	if len(stockUC.calls) != 1 {
		t.Errorf("stock movements = %d, want 1", len(stockUC.calls))
	}
	// payment should have been recorded
	if len(payRepo.recorded) != 1 {
		t.Errorf("payment records = %d, want 1", len(payRepo.recorded))
	}
}

// TestQuickCheckout_InsufficientStock_Returns422 verifies that stock failure causes a full rollback.
func TestQuickCheckout_InsufficientStock_Returns422(t *testing.T) {
	repo := newMockBillRepo()
	stockUC := newMockStockUC()
	stockUC.failOnIdx = 0
	stockUC.failErr = &appstock.InsufficientStockError{
		Available: decimal.NewFromFloat(0),
		Requested: decimal.NewFromFloat(5),
	}
	unitRepo := newMockProductUnitRepo()
	payRepo := newMockPaymentRepo()

	req := appbill.QuickCheckoutRequest{
		TenantID:      testTenantID,
		CreatorID:     testCreatorID,
		PaymentMethod: "cash",
		PaidAmount:    decimal.NewFromFloat(100),
		Items: []appbill.SaleItem{
			{
				ProductID:   uuid.New(),
				WarehouseID: uuid.New(),
				Qty:         decimal.NewFromFloat(5),
				UnitPrice:   decimal.NewFromFloat(20),
				LineNo:      1,
			},
		},
	}

	uc := newQuickCheckoutUC(repo, stockUC, unitRepo, payRepo)
	_, err := uc.Execute(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for insufficient stock, got nil")
	}
	// no payment recorded
	if len(payRepo.recorded) != 0 {
		t.Errorf("expected no payment recorded after rollback, got %d", len(payRepo.recorded))
	}
}

// TestQuickCheckout_EmptyItems_ReturnsError verifies validation error for zero items.
func TestQuickCheckout_EmptyItems_ReturnsError(t *testing.T) {
	repo := newMockBillRepo()
	uc := newQuickCheckoutUC(repo, newMockStockUC(), newMockProductUnitRepo(), newMockPaymentRepo())

	_, err := uc.Execute(context.Background(), appbill.QuickCheckoutRequest{
		TenantID:  testTenantID,
		CreatorID: testCreatorID,
		Items:     nil,
	})
	if err == nil {
		t.Fatal("expected validation error for empty items, got nil")
	}
}

func quickCheckoutItems(unitPrice, qty int64) []appbill.SaleItem {
	return []appbill.SaleItem{{
		ProductID:   uuid.New(),
		WarehouseID: uuid.New(),
		Qty:         decimal.NewFromInt(qty),
		UnitPrice:   decimal.NewFromInt(unitPrice),
		LineNo:      1,
	}}
}

// TestQuickCheckout_Overpay_BooksTotalAndReturnsChange verifies tendering 100 on an 80
// bill books 80, reports 20 change, and never lets paid_amount exceed total_amount.
func TestQuickCheckout_Overpay_BooksTotalAndReturnsChange(t *testing.T) {
	repo := newMockBillRepo()
	payRepo := newMockPaymentRepo()
	uc := newQuickCheckoutUC(repo, newMockStockUC(), newMockProductUnitRepo(), payRepo)

	result, err := uc.Execute(context.Background(), appbill.QuickCheckoutRequest{
		TenantID: testTenantID, CreatorID: testCreatorID, PaymentMethod: "cash",
		PaidAmount: decimal.NewFromInt(100),
		Items:      quickCheckoutItems(40, 2),
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := result.ChangeAmount.String(); got != "20" {
		t.Errorf("ChangeAmount = %s, want 20", got)
	}
	if !result.ReceivableAmount.IsZero() {
		t.Errorf("ReceivableAmount = %s, want 0", result.ReceivableAmount)
	}
	if len(payRepo.recorded) != 1 || !payRepo.recorded[0].Amount.Equal(decimal.NewFromInt(80)) {
		t.Fatalf("payments = %+v, want one of 80", payRepo.recorded)
	}
	head := repo.billsByID[result.BillID]
	if head.PaidAmount.GreaterThan(head.TotalAmount) {
		t.Errorf("paid_amount %s exceeds total_amount %s", head.PaidAmount, head.TotalAmount)
	}
}

// TestQuickCheckout_WithPartner_PersistsPartnerOnBillAndPayment verifies a customer profile
// lands on bill_head and the payment, and the name is not duplicated into the remark.
func TestQuickCheckout_WithPartner_PersistsPartnerOnBillAndPayment(t *testing.T) {
	repo := newMockBillRepo()
	payRepo := newMockPaymentRepo()
	uc := newQuickCheckoutUC(repo, newMockStockUC(), newMockProductUnitRepo(), payRepo)
	partnerID := uuid.New()

	result, err := uc.Execute(context.Background(), appbill.QuickCheckoutRequest{
		TenantID: testTenantID, CreatorID: testCreatorID, PaymentMethod: "cash",
		PartnerID: &partnerID, CustomerName: "老王",
		PaidAmount: decimal.NewFromInt(10),
		Items:      quickCheckoutItems(40, 2),
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	head := repo.billsByID[result.BillID]
	if head.PartnerID == nil || *head.PartnerID != partnerID {
		t.Errorf("bill partner_id = %v, want %s", head.PartnerID, partnerID)
	}
	if head.Remark != "" {
		t.Errorf("remark = %q, want empty when partner is set", head.Remark)
	}
	if len(payRepo.recorded) != 1 || payRepo.recorded[0].PartnerID == nil || *payRepo.recorded[0].PartnerID != partnerID {
		t.Errorf("payment partner not carried: %+v", payRepo.recorded)
	}
}

// TestQuickCheckout_WithoutPartner_KeepsNameInRemark verifies the walk-in name fallback.
func TestQuickCheckout_WithoutPartner_KeepsNameInRemark(t *testing.T) {
	repo := newMockBillRepo()
	uc := newQuickCheckoutUC(repo, newMockStockUC(), newMockProductUnitRepo(), newMockPaymentRepo())
	result, err := uc.Execute(context.Background(), appbill.QuickCheckoutRequest{
		TenantID: testTenantID, CreatorID: testCreatorID, CustomerName: "散客",
		Items: quickCheckoutItems(10, 1),
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := repo.billsByID[result.BillID].Remark; got != "散客" {
		t.Errorf("remark = %q, want 散客", got)
	}
}

// TestQuickCheckout_UnknownPartner_ReturnsErrValidation verifies a missing partner is a
// validation error and nothing is written.
func TestQuickCheckout_UnknownPartner_ReturnsErrValidation(t *testing.T) {
	repo := newMockBillRepo()
	stockUC := newMockStockUC()
	missing := uuid.New()
	repo.missingRefs = map[uuid.UUID]struct{}{missing: {}}
	uc := newQuickCheckoutUC(repo, stockUC, newMockProductUnitRepo(), newMockPaymentRepo())

	_, err := uc.Execute(context.Background(), appbill.QuickCheckoutRequest{
		TenantID: testTenantID, CreatorID: testCreatorID, PartnerID: &missing,
		Items: quickCheckoutItems(10, 1),
	})
	if !errors.Is(err, appbill.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	if repo.storedHead != nil || len(stockUC.calls) != 0 {
		t.Error("nothing may be written for an unknown partner")
	}
}

// TestQuickCheckout_Discount_ReducesTotalAndReceivable verifies a whole-bill discount lowers the
// bill total and the receivable, keeps subtotal at the line sum, and rejects bad discounts.
func TestQuickCheckout_Discount_ReducesTotalAndReceivable(t *testing.T) {
	repo := newMockBillRepo()
	payRepo := newMockPaymentRepo()
	uc := newQuickCheckoutUC(repo, newMockStockUC(), newMockProductUnitRepo(), payRepo)

	result, err := uc.Execute(context.Background(), appbill.QuickCheckoutRequest{
		TenantID: testTenantID, CreatorID: testCreatorID, PaymentMethod: "cash",
		PaidAmount:     decimal.RequireFromString("70"),
		DiscountAmount: decimal.RequireFromString("10"),
		Items:          quickCheckoutItems(40, 2),
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := result.TotalAmount.String(); got != "70" {
		t.Errorf("TotalAmount = %s, want 70", got)
	}
	if !result.ReceivableAmount.IsZero() {
		t.Errorf("ReceivableAmount = %s, want 0", result.ReceivableAmount)
	}
	head := repo.billsByID[result.BillID]
	if got := head.Subtotal.String(); got != "80" {
		t.Errorf("Subtotal = %s, want 80", got)
	}
	if got := head.TotalAmount.String(); got != "70" {
		t.Errorf("head.TotalAmount = %s, want 70", got)
	}

	for _, d := range []string{"-1", "80.01"} {
		_, err := uc.Execute(context.Background(), appbill.QuickCheckoutRequest{
			TenantID: testTenantID, CreatorID: testCreatorID, PaymentMethod: "cash",
			DiscountAmount: decimal.RequireFromString(d),
			Items:          quickCheckoutItems(40, 2),
		})
		if !errors.Is(err, appbill.ErrValidation) {
			t.Errorf("discount %s: err = %v, want ErrValidation", d, err)
		}
	}
}

// TestQuickCheckout_InvalidPayMethod_Rejected verifies an unknown payment method is
// rejected up front, leaving no bill, stock movement or payment behind.
func TestQuickCheckout_InvalidPayMethod_Rejected(t *testing.T) {
	repo := newMockBillRepo()
	stockUC := newMockStockUC()
	payRepo := newMockPaymentRepo()
	uc := newQuickCheckoutUC(repo, stockUC, newMockProductUnitRepo(), payRepo)

	_, err := uc.Execute(context.Background(), appbill.QuickCheckoutRequest{
		TenantID: testTenantID, CreatorID: testCreatorID,
		PaymentMethod: "bitcoin", PaidAmount: decimal.NewFromInt(50),
		Items: []appbill.SaleItem{{
			ProductID: uuid.New(), WarehouseID: uuid.New(),
			Qty: decimal.NewFromInt(2), UnitPrice: decimal.NewFromInt(25), LineNo: 1,
		}},
	})
	if !errors.Is(err, appbill.ErrInvalidPayMethod) {
		t.Fatalf("err = %v, want ErrInvalidPayMethod", err)
	}
	if len(stockUC.calls) != 0 || len(payRepo.recorded) != 0 || len(repo.billsByID) != 0 {
		t.Errorf("writes leaked: stock=%d pay=%d bills=%d", len(stockUC.calls), len(payRepo.recorded), len(repo.billsByID))
	}
}

// TestQuickCheckout_PaidAmountOverTwoDecimals_ReturnsErrValidation verifies cash is taken in
// whole cents, so no change of a fraction of a cent is ever reported.
func TestQuickCheckout_PaidAmountOverTwoDecimals_ReturnsErrValidation(t *testing.T) {
	repo := newMockBillRepo()
	stockUC := newMockStockUC()
	uc := newQuickCheckoutUC(repo, stockUC, newMockProductUnitRepo(), newMockPaymentRepo())

	_, err := uc.Execute(context.Background(), appbill.QuickCheckoutRequest{
		TenantID: testTenantID, CreatorID: testCreatorID,
		Items: quickCheckoutItems(5, 1), PaidAmount: decimal.RequireFromString("5.001"),
	})
	if !errors.Is(err, appbill.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	if repo.storedHead != nil || len(stockUC.calls) != 0 {
		t.Error("nothing may be written for a bad paid amount")
	}
}
