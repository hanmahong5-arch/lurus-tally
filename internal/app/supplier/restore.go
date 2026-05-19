package supplier

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/supplier"
)

// RestoreUseCase restores a soft-deleted supplier.
type RestoreUseCase struct {
	repo Repository
}

// NewRestoreUseCase constructs a RestoreUseCase.
func NewRestoreUseCase(repo Repository) *RestoreUseCase {
	return &RestoreUseCase{repo: repo}
}

// Execute restores the supplier. Returns ErrNotFound if no matching soft-deleted row exists.
func (uc *RestoreUseCase) Execute(ctx context.Context, tenantID, id uuid.UUID) (*domain.Supplier, error) {
	s, err := uc.repo.Restore(ctx, tenantID, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("supplier restore: %w", err)
	}
	return s, nil
}
