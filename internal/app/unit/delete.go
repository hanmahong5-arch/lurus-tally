package unit

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// DeleteUseCase deletes a tenant-custom unit.
// System units (is_system = true) must not be deleted; the repo enforces this.
type DeleteUseCase struct {
	repo Repository
}

// NewDeleteUseCase constructs the use case.
func NewDeleteUseCase(repo Repository) *DeleteUseCase {
	return &DeleteUseCase{repo: repo}
}

// Execute deletes the unit or returns an error if it is a system unit.
func (uc *DeleteUseCase) Execute(ctx context.Context, tenantID, id uuid.UUID) error {
	if tenantID == uuid.Nil {
		return fmt.Errorf("delete unit: tenant_id is required")
	}

	u, err := uc.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		return fmt.Errorf("delete unit: %w", err)
	}
	if u == nil {
		return fmt.Errorf("delete unit: not found")
	}
	if u.IsSystem {
		return fmt.Errorf(
			"delete unit: system unit %q cannot be deleted: "+
				"only tenant-custom units may be deleted; system units are shared across all tenants",
			u.Code,
		)
	}

	if err := uc.repo.Delete(ctx, tenantID, id); err != nil {
		return fmt.Errorf("delete unit: %w", err)
	}
	return nil
}
