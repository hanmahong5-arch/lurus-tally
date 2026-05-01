package project

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/project"
)

// RestoreUseCase restores a soft-deleted project.
type RestoreUseCase struct {
	repo Repository
}

// NewRestoreUseCase constructs a RestoreUseCase.
func NewRestoreUseCase(repo Repository) *RestoreUseCase {
	return &RestoreUseCase{repo: repo}
}

// Execute restores the project. Returns ErrNotFound if no matching soft-deleted row exists.
func (uc *RestoreUseCase) Execute(ctx context.Context, tenantID, id uuid.UUID) (*domain.Project, error) {
	p, err := uc.repo.Restore(ctx, tenantID, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("project restore: %w", err)
	}
	return p, nil
}
