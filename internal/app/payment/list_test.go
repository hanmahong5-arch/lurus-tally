package payment_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	apppayment "github.com/hanmahong5-arch/lurus-tally/internal/app/payment"
	domainpayment "github.com/hanmahong5-arch/lurus-tally/internal/domain/payment"
)

// TestListPayments_ByBillID_ReturnsAll verifies that listPaymentsUseCase returns all seeded records.
func TestListPayments_ByBillID_ReturnsAll(t *testing.T) {
	payRepo := newMockPaymentRepo()
	payRepo.recorded = []*domainpayment.Payment{
		{
			ID:        uuid.New(),
			TenantID:  testTenantID,
			BillID:    testBillID,
			PayType:   domainpayment.PayTypeCash,
			Amount:    decimal.NewFromFloat(100),
			CreatorID: testCreatorID,
			PayDate:   time.Now(),
		},
		{
			ID:        uuid.New(),
			TenantID:  testTenantID,
			BillID:    testBillID,
			PayType:   domainpayment.PayTypeWechat,
			Amount:    decimal.NewFromFloat(50),
			CreatorID: testCreatorID,
			PayDate:   time.Now(),
		},
	}

	uc := apppayment.NewListPaymentsUseCase(payRepo)
	payments, err := uc.Execute(context.Background(), testTenantID, testBillID)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(payments) != 2 {
		t.Errorf("payments count = %d, want 2", len(payments))
	}
}

// TestListPayments_EmptyBill_ReturnsEmptySlice verifies no payments returns empty (not nil).
func TestListPayments_EmptyBill_ReturnsEmptySlice(t *testing.T) {
	payRepo := newMockPaymentRepo()
	uc := apppayment.NewListPaymentsUseCase(payRepo)

	payments, err := uc.Execute(context.Background(), testTenantID, testBillID)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if payments == nil {
		t.Error("expected empty slice, got nil")
	}
	if len(payments) != 0 {
		t.Errorf("expected 0 payments, got %d", len(payments))
	}
}
