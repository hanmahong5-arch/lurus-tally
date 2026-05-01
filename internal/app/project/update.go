package project

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/project"
)

// UpdateUseCase applies partial updates to an existing project.
type UpdateUseCase struct {
	repo Repository
}

// NewUpdateUseCase constructs an UpdateUseCase.
func NewUpdateUseCase(repo Repository) *UpdateUseCase {
	return &UpdateUseCase{repo: repo}
}

// Execute fetches the existing project, applies non-nil fields from UpdateInput,
// validates the merged entity, and persists the changes.
func (uc *UpdateUseCase) Execute(ctx context.Context, tenantID, id uuid.UUID, in domain.UpdateInput) (*domain.Project, error) {
	p, err := uc.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("project update fetch: %w", err)
	}

	if in.Code != nil {
		p.Code = *in.Code
	}
	if in.Name != nil {
		p.Name = *in.Name
	}
	if in.CustomerID != nil {
		p.CustomerID = in.CustomerID
	}
	if in.ContractAmount != nil {
		p.ContractAmount = in.ContractAmount
	}
	if in.StartDate != nil {
		p.StartDate = in.StartDate
	}
	if in.EndDate != nil {
		p.EndDate = in.EndDate
	}
	if in.Status != nil {
		p.Status = *in.Status
	}
	if in.Address != nil {
		p.Address = *in.Address
	}
	if in.Manager != nil {
		p.Manager = *in.Manager
	}
	if in.Remark != nil {
		p.Remark = *in.Remark
	}
	p.UpdatedAt = time.Now().UTC()

	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("project update validate: %w", err)
	}
	if err := uc.repo.Update(ctx, p); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("project update: %w", err)
	}
	return p, nil
}
