package warehouse

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/warehouse"
)

// CreateUseCase creates a new warehouse.
type CreateUseCase struct {
	repo Repository
}

// NewCreateUseCase constructs a CreateUseCase.
func NewCreateUseCase(repo Repository) *CreateUseCase {
	return &CreateUseCase{repo: repo}
}

// Execute validates the input and creates a new Warehouse.
func (uc *CreateUseCase) Execute(ctx context.Context, in domain.CreateInput) (*domain.Warehouse, error) {
	now := time.Now().UTC()
	w := &domain.Warehouse{
		ID:        uuid.New(),
		TenantID:  in.TenantID,
		Code:      in.Code,
		Name:      in.Name,
		Address:   in.Address,
		Manager:   in.Manager,
		IsDefault: in.IsDefault,
		Remark:    in.Remark,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := w.Validate(); err != nil {
		return nil, fmt.Errorf("warehouse create validate: %w", err)
	}
	if err := uc.repo.Create(ctx, w); err != nil {
		if errors.Is(err, ErrDuplicateName) {
			return nil, ErrDuplicateName
		}
		return nil, fmt.Errorf("warehouse create: %w", err)
	}
	return w, nil
}
