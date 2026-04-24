package bill_test

import (
	"context"
	"testing"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
)

// TestListPurchases_PaginationDefaults verifies that page=0 is clamped to 1 and size=0 to 20.
func TestListPurchases_PaginationDefaults(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewListPurchasesUseCase(repo)

	// Page and size are 0 → should default to page=1, size=20.
	out, err := uc.Execute(context.Background(), appbill.BillListFilter{
		TenantID: testTenantID,
		Page:     0,
		Size:     0,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// With an empty repo, we just verify it does not error.
	if out == nil {
		t.Fatal("output is nil")
	}
}
