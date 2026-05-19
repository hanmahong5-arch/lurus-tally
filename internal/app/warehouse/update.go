package warehouse

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/warehouse"
)

// UpdateUseCase applies partial updates to an existing warehouse.
type UpdateUseCase struct {
	repo Repository
}

// NewUpdateUseCase constructs an UpdateUseCase.
func NewUpdateUseCase(repo Repository) *UpdateUseCase {
	return &UpdateUseCase{repo: repo}
}

// Execute fetches the existing warehouse, applies non-nil fields, validates, and persists.
func (uc *UpdateUseCase) Execute(ctx context.Context, tenantID, id uuid.UUID, in domain.UpdateInput) (*domain.Warehouse, error) {
	w, err := uc.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("warehouse update fetch: %w", err)
	}

	if in.Code != nil {
		w.Code = *in.Code
	}
	if in.Name != nil {
		w.Name = *in.Name
	}
	if in.Address != nil {
		w.Address = *in.Address
	}
	if in.Manager != nil {
		w.Manager = *in.Manager
	}
	if in.IsDefault != nil {
		w.IsDefault = *in.IsDefault
	}
	if in.Remark != nil {
		w.Remark = *in.Remark
	}
	w.UpdatedAt = time.Now().UTC()

	if err := w.Validate(); err != nil {
		return nil, fmt.Errorf("warehouse update validate: %w", err)
	}
	if err := uc.repo.Update(ctx, w); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("warehouse update: %w", err)
	}
	return w, nil
}
