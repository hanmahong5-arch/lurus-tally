package customer

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/customer"
)

// RestoreUseCase restores a soft-deleted customer.
type RestoreUseCase struct {
	repo Repository
}

// NewRestoreUseCase constructs a RestoreUseCase.
func NewRestoreUseCase(repo Repository) *RestoreUseCase {
	return &RestoreUseCase{repo: repo}
}

// Execute clears deleted_at and returns the restored customer.
func (uc *RestoreUseCase) Execute(ctx context.Context, tenantID, id uuid.UUID) (*domain.Customer, error) {
	c, err := uc.repo.Restore(ctx, tenantID, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("customer restore: %w", err)
	}
	return c, nil
}
