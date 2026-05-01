package project

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/project"
)

// GetByIDUseCase retrieves a single project by ID.
type GetByIDUseCase struct {
	repo Repository
}

// NewGetByIDUseCase constructs a GetByIDUseCase.
func NewGetByIDUseCase(repo Repository) *GetByIDUseCase {
	return &GetByIDUseCase{repo: repo}
}

// Execute fetches the project. Returns ErrNotFound if absent or not visible to the tenant.
func (uc *GetByIDUseCase) Execute(ctx context.Context, tenantID, id uuid.UUID) (*domain.Project, error) {
	p, err := uc.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("project get: %w", err)
	}
	return p, nil
}
