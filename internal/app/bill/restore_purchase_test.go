package bill_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

// TestRestorePurchaseUseCase_Execute_SetsDraft verifies that a cancelled bill is restored to draft.
func TestRestorePurchaseUseCase_Execute_SetsDraft(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewRestorePurchaseUseCase(repo)

	warehouseID := uuid.New()
	billID := seedDraftBill(repo, 1, warehouseID)
	// Force status to Cancelled.
	repo.billsByID[billID].Status = domain.StatusCancelled

	if err := uc.Execute(context.Background(), testTenantID, billID); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if repo.billsByID[billID].Status != domain.StatusDraft {
		t.Errorf("status = %d, want %d (Draft)", repo.billsByID[billID].Status, domain.StatusDraft)
	}
}

// TestRestorePurchaseUseCase_Execute_ReturnsErrorForApproved verifies that restoring an
// approved bill returns ErrCannotRestoreApproved.
func TestRestorePurchaseUseCase_Execute_ReturnsErrorForApproved(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewRestorePurchaseUseCase(repo)

	warehouseID := uuid.New()
	billID := seedDraftBill(repo, 1, warehouseID)
	repo.billsByID[billID].Status = domain.StatusApproved

	err := uc.Execute(context.Background(), testTenantID, billID)
	if err == nil {
		t.Fatal("expected error for restoring approved bill, got nil")
	}
	if !errors.Is(err, appbill.ErrCannotRestoreApproved) {
		t.Errorf("expected ErrCannotRestoreApproved, got %v", err)
	}
}

// TestRestorePurchaseUseCase_Execute_IdempotentOnDraft verifies that restoring an already-draft
// bill is a no-op (returns nil).
func TestRestorePurchaseUseCase_Execute_IdempotentOnDraft(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewRestorePurchaseUseCase(repo)

	warehouseID := uuid.New()
	billID := seedDraftBill(repo, 1, warehouseID)
	// Bill is already in draft status.

	if err := uc.Execute(context.Background(), testTenantID, billID); err != nil {
		t.Fatalf("Execute on draft bill: %v", err)
	}
	// Status must remain Draft.
	if repo.billsByID[billID].Status != domain.StatusDraft {
		t.Errorf("status = %d, want %d (Draft)", repo.billsByID[billID].Status, domain.StatusDraft)
	}
}
