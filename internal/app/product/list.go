package product

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/product"
)

// ListUseCase retrieves a filtered, paginated list of products.
type ListUseCase struct {
	repo Repository
}

// NewListUseCase constructs the use case.
func NewListUseCase(repo Repository) *ListUseCase {
	return &ListUseCase{repo: repo}
}

// ListOutput wraps the paginated result.
type ListOutput struct {
	Items []*domain.Product
	Total int
}

// Execute returns products matching the filter.
func (uc *ListUseCase) Execute(ctx context.Context, filter domain.ListFilter) (*ListOutput, error) {
	if filter.TenantID == uuid.Nil {
		return nil, fmt.Errorf("list products: tenant_id is required")
	}
	if filter.Limit <= 0 {
		filter.Limit = 20
	}
	if filter.Limit > 200 {
		filter.Limit = 200
	}

	items, total, err := uc.repo.List(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("list products: %w", err)
	}
	return &ListOutput{Items: items, Total: total}, nil
}
