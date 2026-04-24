package product

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/product"
)

// GetUseCase retrieves a single product by ID within a tenant.
type GetUseCase struct {
	repo Repository
}

// NewGetUseCase constructs the use case.
func NewGetUseCase(repo Repository) *GetUseCase {
	return &GetUseCase{repo: repo}
}

// Execute fetches the product or returns a not-found error.
func (uc *GetUseCase) Execute(ctx context.Context, tenantID, id uuid.UUID) (*domain.Product, error) {
	if tenantID == uuid.Nil {
		return nil, fmt.Errorf("get product: tenant_id is required")
	}
	p, err := uc.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		return nil, fmt.Errorf("get product: %w", err)
	}
	return p, nil
}
