package bill_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

// TestUpdatePurchaseDraft_OnlyAllowedInDraft verifies that updating an approved bill returns an error.
func TestUpdatePurchaseDraft_OnlyAllowedInDraft(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewUpdatePurchaseDraftUseCase(repo)

	warehouseID := uuid.New()
	billID := seedDraftBill(repo, 1, warehouseID)
	// Force to approved.
	repo.billsByID[billID].Status = domain.StatusApproved

	_, err := uc.Execute(context.Background(), appbill.UpdatePurchaseDraftRequest{
		TenantID: testTenantID,
		BillID:   billID,
		Items: []appbill.CreatePurchaseItemInput{
			{ProductID: uuid.New(), Qty: decimal.NewFromFloat(1), UnitPrice: decimal.NewFromFloat(10), LineNo: 1},
		},
	})
	if err == nil {
		t.Fatal("expected error when updating approved bill, got nil")
	}
}

// TestUpdatePurchaseDraft_RecalculatesTotals verifies that updating items recalculates totals.
func TestUpdatePurchaseDraft_RecalculatesTotals(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewUpdatePurchaseDraftUseCase(repo)

	warehouseID := uuid.New()
	billID := seedDraftBill(repo, 1, warehouseID)

	productID := uuid.New()
	req := appbill.UpdatePurchaseDraftRequest{
		TenantID:    testTenantID,
		BillID:      billID,
		ShippingFee: decimal.NewFromFloat(20),
		TaxAmount:   decimal.Zero,
		BillDate:    time.Now(),
		Items: []appbill.CreatePurchaseItemInput{
			{ProductID: productID, Qty: decimal.NewFromFloat(5), UnitPrice: decimal.NewFromFloat(10), LineNo: 1},
		},
	}
	head, err := uc.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// subtotal = 5*10 = 50; total = 50 + 20 = 70
	if !head.Subtotal.Equal(decimal.NewFromFloat(50)) {
		t.Errorf("Subtotal = %s, want 50", head.Subtotal)
	}
	if !head.TotalAmount.Equal(decimal.NewFromFloat(70)) {
		t.Errorf("TotalAmount = %s, want 70", head.TotalAmount)
	}
}
