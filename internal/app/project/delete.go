package project

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// DeleteUseCase soft-deletes a project.
type DeleteUseCase struct {
	repo Repository
}

// NewDeleteUseCase constructs a DeleteUseCase.
func NewDeleteUseCase(repo Repository) *DeleteUseCase {
	return &DeleteUseCase{repo: repo}
}

// Execute soft-deletes the project. Returns ErrNotFound if absent or already deleted.
func (uc *DeleteUseCase) Execute(ctx context.Context, tenantID, id uuid.UUID) error {
	if err := uc.repo.Delete(ctx, tenantID, id); err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrNotFound
		}
		return fmt.Errorf("project delete: %w", err)
	}
	return nil
}
