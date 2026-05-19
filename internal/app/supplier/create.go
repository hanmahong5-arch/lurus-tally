package supplier

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/supplier"
)

// CreateUseCase creates a new supplier.
type CreateUseCase struct {
	repo Repository
}

// NewCreateUseCase constructs a CreateUseCase.
func NewCreateUseCase(repo Repository) *CreateUseCase {
	return &CreateUseCase{repo: repo}
}

// Execute validates the input and creates a new Supplier.
func (uc *CreateUseCase) Execute(ctx context.Context, in domain.CreateInput) (*domain.Supplier, error) {
	now := time.Now().UTC()
	s := &domain.Supplier{
		ID:        uuid.New(),
		TenantID:  in.TenantID,
		Code:      in.Code,
		Name:      in.Name,
		Contact:   in.Contact,
		Phone:     in.Phone,
		Email:     in.Email,
		Address:   in.Address,
		Remark:    in.Remark,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.Validate(); err != nil {
		return nil, fmt.Errorf("supplier create validate: %w", err)
	}
	if err := uc.repo.Create(ctx, s); err != nil {
		if errors.Is(err, ErrDuplicateName) {
			return nil, ErrDuplicateName
		}
		return nil, fmt.Errorf("supplier create: %w", err)
	}
	return s, nil
}
