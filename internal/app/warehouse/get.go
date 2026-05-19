package warehouse

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/warehouse"
)

// GetByIDUseCase retrieves a single warehouse by ID.
type GetByIDUseCase struct {
	repo Repository
}

// NewGetByIDUseCase constructs a GetByIDUseCase.
func NewGetByIDUseCase(repo Repository) *GetByIDUseCase {
	return &GetByIDUseCase{repo: repo}
}

// Execute fetches the warehouse. Returns ErrNotFound if absent or not visible to the tenant.
func (uc *GetByIDUseCase) Execute(ctx context.Context, tenantID, id uuid.UUID) (*domain.Warehouse, error) {
	w, err := uc.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("warehouse get: %w", err)
	}
	return w, nil
}
