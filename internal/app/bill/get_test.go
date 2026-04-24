package bill_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
)

// TestGetPurchase_NotFound_Returns404 verifies that a missing bill returns ErrBillNotFound.
func TestGetPurchase_NotFound_Returns404(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewGetPurchaseUseCase(repo)

	_, err := uc.Execute(context.Background(), testTenantID, uuid.New())
	if err == nil {
		t.Fatal("expected error for missing bill, got nil")
	}
	if !errors.Is(err, appbill.ErrBillNotFound) {
		t.Errorf("expected ErrBillNotFound, got %v", err)
	}
}

// TestGetPurchase_ExistingBill_ReturnsHeadAndItems verifies that a seeded bill is returned with items.
func TestGetPurchase_ExistingBill_ReturnsHeadAndItems(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewGetPurchaseUseCase(repo)

	warehouseID := uuid.New()
	billID := seedDraftBill(repo, 2, warehouseID)

	out, err := uc.Execute(context.Background(), testTenantID, billID)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Head.ID != billID {
		t.Errorf("head.ID = %s, want %s", out.Head.ID, billID)
	}
	if len(out.Items) != 2 {
		t.Errorf("items count = %d, want 2", len(out.Items))
	}
}
