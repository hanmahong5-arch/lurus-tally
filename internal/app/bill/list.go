package bill

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

// ListPurchasesOutput is returned by ListPurchasesUseCase.
type ListPurchasesOutput struct {
	Items []domain.BillHead
	Total int64
}

// ListPurchasesUseCase lists purchase bills with pagination and optional filters.
type ListPurchasesUseCase struct {
	repo BillRepo
}

// NewListPurchasesUseCase constructs the use case.
func NewListPurchasesUseCase(repo BillRepo) *ListPurchasesUseCase {
	return &ListPurchasesUseCase{repo: repo}
}

// Execute returns paginated purchase bills for the tenant.
// BillListFilter.BillType is forced to BillTypePurchase.
func (uc *ListPurchasesUseCase) Execute(ctx context.Context, f BillListFilter) (*ListPurchasesOutput, error) {
	if f.TenantID == uuid.Nil {
		return nil, fmt.Errorf("list purchases: tenant_id is required")
	}
	// Enforce purchase type.
	f.BillType = domain.BillTypePurchase

	// Apply pagination defaults.
	if f.Page <= 0 {
		f.Page = 1
	}
	if f.Size <= 0 {
		f.Size = 20
	}

	bills, total, err := uc.repo.ListBills(ctx, f)
	if err != nil {
		return nil, fmt.Errorf("list purchases: %w", err)
	}
	return &ListPurchasesOutput{Items: bills, Total: total}, nil
}
