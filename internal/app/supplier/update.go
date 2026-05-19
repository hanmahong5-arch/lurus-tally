package supplier

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/supplier"
)

// UpdateUseCase applies partial updates to an existing supplier.
type UpdateUseCase struct {
	repo Repository
}

// NewUpdateUseCase constructs an UpdateUseCase.
func NewUpdateUseCase(repo Repository) *UpdateUseCase {
	return &UpdateUseCase{repo: repo}
}

// Execute fetches the existing supplier, applies non-nil fields, validates, and persists.
func (uc *UpdateUseCase) Execute(ctx context.Context, tenantID, id uuid.UUID, in domain.UpdateInput) (*domain.Supplier, error) {
	s, err := uc.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("supplier update fetch: %w", err)
	}

	if in.Code != nil {
		s.Code = *in.Code
	}
	if in.Name != nil {
		s.Name = *in.Name
	}
	if in.Contact != nil {
		s.Contact = *in.Contact
	}
	if in.Phone != nil {
		s.Phone = *in.Phone
	}
	if in.Email != nil {
		s.Email = *in.Email
	}
	if in.Address != nil {
		s.Address = *in.Address
	}
	if in.Remark != nil {
		s.Remark = *in.Remark
	}
	s.UpdatedAt = time.Now().UTC()

	if err := s.Validate(); err != nil {
		return nil, fmt.Errorf("supplier update validate: %w", err)
	}
	if err := uc.repo.Update(ctx, s); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("supplier update: %w", err)
	}
	return s, nil
}
