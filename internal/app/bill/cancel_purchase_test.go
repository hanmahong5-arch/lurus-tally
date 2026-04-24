package bill_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

// TestCancelPurchase_Draft_Succeeds verifies that cancelling a draft bill works.
func TestCancelPurchase_Draft_Succeeds(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewCancelPurchaseUseCase(repo)

	warehouseID := uuid.New()
	billID := seedDraftBill(repo, 1, warehouseID)

	if err := uc.Execute(context.Background(), testTenantID, billID); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if repo.billsByID[billID].Status != domain.StatusCancelled {
		t.Errorf("status = %d, want %d (Cancelled)", repo.billsByID[billID].Status, domain.StatusCancelled)
	}
}

// TestCancelPurchase_Approved_Returns422WithActionHint verifies that cancelling an approved bill
// returns ErrCannotCancelApproved.
func TestCancelPurchase_Approved_Returns422WithActionHint(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewCancelPurchaseUseCase(repo)

	warehouseID := uuid.New()
	billID := seedDraftBill(repo, 1, warehouseID)
	repo.billsByID[billID].Status = domain.StatusApproved

	err := uc.Execute(context.Background(), testTenantID, billID)
	if err == nil {
		t.Fatal("expected error for cancelling approved bill, got nil")
	}
	if !errors.Is(err, appbill.ErrCannotCancelApproved) {
		t.Errorf("expected ErrCannotCancelApproved, got %v", err)
	}
}
