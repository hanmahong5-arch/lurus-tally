package unit

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/unit"
)

// ListUseCase returns unit_defs visible to the tenant (system + tenant-custom).
type ListUseCase struct {
	repo Repository
}

// NewListUseCase constructs the use case.
func NewListUseCase(repo Repository) *ListUseCase {
	return &ListUseCase{repo: repo}
}

// Execute returns all units visible to the given tenant.
func (uc *ListUseCase) Execute(ctx context.Context, filter domain.ListFilter) ([]*domain.UnitDef, error) {
	if filter.TenantID == uuid.Nil {
		return nil, fmt.Errorf("list units: tenant_id is required")
	}
	units, err := uc.repo.List(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("list units: %w", err)
	}
	return units, nil
}
