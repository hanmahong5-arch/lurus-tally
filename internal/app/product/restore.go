package product

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/product"
)

// RestoreUseCase un-deletes a soft-deleted product by clearing its deleted_at timestamp.
type RestoreUseCase struct {
	repo Repository
}

// NewRestoreUseCase constructs the use case.
func NewRestoreUseCase(repo Repository) *RestoreUseCase {
	return &RestoreUseCase{repo: repo}
}

// Execute restores the soft-deleted product. Returns ErrNotFound (from the repo) when the
// product does not exist, is not deleted, or belongs to a different tenant.
func (uc *RestoreUseCase) Execute(ctx context.Context, tenantID, id uuid.UUID) (*domain.Product, error) {
	if tenantID == uuid.Nil {
		return nil, fmt.Errorf("restore product: tenant_id is required")
	}
	if id == uuid.Nil {
		return nil, fmt.Errorf("restore product: product_id is required")
	}

	p, err := uc.repo.Restore(ctx, tenantID, id)
	if err != nil {
		return nil, fmt.Errorf("restore product: %w", err)
	}
	return p, nil
}
