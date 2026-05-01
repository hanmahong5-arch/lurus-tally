package horticulture

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/horticulture"
)

// GetByIDUseCase retrieves a single nursery dict entry by ID.
type GetByIDUseCase struct {
	repo Repository
}

// NewGetByIDUseCase constructs a GetByIDUseCase.
func NewGetByIDUseCase(repo Repository) *GetByIDUseCase {
	return &GetByIDUseCase{repo: repo}
}

// Execute fetches the entry. Returns ErrNotFound if absent or not visible to the tenant.
func (uc *GetByIDUseCase) Execute(ctx context.Context, tenantID, id uuid.UUID) (*domain.NurseryDict, error) {
	d, err := uc.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("nursery dict get: %w", err)
	}
	return d, nil
}
