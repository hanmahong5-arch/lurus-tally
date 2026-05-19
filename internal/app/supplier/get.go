package supplier

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/supplier"
)

// GetByIDUseCase retrieves a single supplier by ID.
type GetByIDUseCase struct {
	repo Repository
}

// NewGetByIDUseCase constructs a GetByIDUseCase.
func NewGetByIDUseCase(repo Repository) *GetByIDUseCase {
	return &GetByIDUseCase{repo: repo}
}

// Execute fetches the supplier. Returns ErrNotFound if absent or not visible to the tenant.
func (uc *GetByIDUseCase) Execute(ctx context.Context, tenantID, id uuid.UUID) (*domain.Supplier, error) {
	s, err := uc.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("supplier get: %w", err)
	}
	return s, nil
}
