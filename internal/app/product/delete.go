package product

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// DeleteUseCase soft-deletes a product by setting deleted_at.
type DeleteUseCase struct {
	repo Repository
}

// NewDeleteUseCase constructs the use case.
func NewDeleteUseCase(repo Repository) *DeleteUseCase {
	return &DeleteUseCase{repo: repo}
}

// Execute removes the product identified by id within tenantID.
func (uc *DeleteUseCase) Execute(ctx context.Context, tenantID, id uuid.UUID) error {
	if tenantID == uuid.Nil {
		return fmt.Errorf("delete product: tenant_id is required")
	}
	if err := uc.repo.Delete(ctx, tenantID, id); err != nil {
		return fmt.Errorf("delete product: %w", err)
	}
	return nil
}
