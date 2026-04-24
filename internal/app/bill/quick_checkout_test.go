package bill_test

import (
	"context"
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
