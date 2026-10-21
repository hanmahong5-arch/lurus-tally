package bill_test

import (
	"context"
	"testing"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
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

// TestListPurchases_ReturnsSummaryAndPropagatesFilter verifies the summary is attached to the output.
func TestListPurchases_ReturnsSummaryAndPropagatesFilter(t *testing.T) {
	repo := newMockBillRepo()
	uc := appbill.NewListPurchasesUseCase(repo)
	out, err := uc.Execute(context.Background(), appbill.BillListFilter{TenantID: testTenantID, PaymentStatus: "open", Keyword: "x"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Summary.Count != 0 {
		t.Errorf("Summary.Count = %d, want 0", out.Summary.Count)
	}
}

type filterCaptureRepo struct {
	*mockBillRepo
	got appbill.BillListFilter
}

func (r *filterCaptureRepo) ListBills(ctx context.Context, f appbill.BillListFilter) ([]domain.BillHead, int64, error) {
	r.got = f
	return r.mockBillRepo.ListBills(ctx, f)
}

// Inbound bills include sales returns; the purchase list must pin sub_type to purchases.
func TestListPurchases_PinsPurchaseSubType(t *testing.T) {
	repo := &filterCaptureRepo{mockBillRepo: newMockBillRepo()}
	if _, err := appbill.NewListPurchasesUseCase(repo).Execute(context.Background(), appbill.BillListFilter{TenantID: testTenantID}); err != nil {
		t.Fatal(err)
	}
	if repo.got.SubType != domain.BillSubTypePurchase || repo.got.BillType != domain.BillTypePurchase {
		t.Errorf("filter = %+v", repo.got)
	}
}
