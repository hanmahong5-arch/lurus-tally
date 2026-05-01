package horticulture

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/horticulture"
)

// RestoreUseCase restores a soft-deleted nursery dict entry.
type RestoreUseCase struct {
	repo Repository
}

// NewRestoreUseCase constructs a RestoreUseCase.
func NewRestoreUseCase(repo Repository) *RestoreUseCase {
	return &RestoreUseCase{repo: repo}
}

// Execute restores the entry. Returns ErrNotFound if no matching soft-deleted row exists.
func (uc *RestoreUseCase) Execute(ctx context.Context, tenantID, id uuid.UUID) (*domain.NurseryDict, error) {
	d, err := uc.repo.Restore(ctx, tenantID, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("nursery dict restore: %w", err)
	}
	return d, nil
}
