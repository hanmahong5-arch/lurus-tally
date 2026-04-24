package bill

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

// GetPurchaseOutput holds the bill head and its items.
type GetPurchaseOutput struct {
	Head  *domain.BillHead
	Items []*domain.BillItem
}

// GetPurchaseUseCase retrieves a single purchase bill with its line items.
type GetPurchaseUseCase struct {
	repo BillRepo
}

// NewGetPurchaseUseCase constructs the use case.
func NewGetPurchaseUseCase(repo BillRepo) *GetPurchaseUseCase {
	return &GetPurchaseUseCase{repo: repo}
}

// Execute loads the bill and its items. Returns ErrBillNotFound when the bill does not exist.
func (uc *GetPurchaseUseCase) Execute(ctx context.Context, tenantID, billID uuid.UUID) (*GetPurchaseOutput, error) {
	if tenantID == uuid.Nil {
		return nil, fmt.Errorf("get purchase: tenant_id is required")
	}
	if billID == uuid.Nil {
		return nil, fmt.Errorf("get purchase: bill_id is required")
	}

	head, err := uc.repo.GetBill(ctx, tenantID, billID)
	if err != nil {
		return nil, fmt.Errorf("get purchase: %w", err)
	}

	items, err := uc.repo.GetBillItems(ctx, tenantID, billID)
	if err != nil {
		return nil, fmt.Errorf("get purchase: items: %w", err)
	}

	return &GetPurchaseOutput{Head: head, Items: items}, nil
}
