package project

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/project"
)

// CreateUseCase creates a new project.
type CreateUseCase struct {
	repo Repository
}

// NewCreateUseCase constructs a CreateUseCase.
func NewCreateUseCase(repo Repository) *CreateUseCase {
	return &CreateUseCase{repo: repo}
}

// Execute validates the input and creates a new Project.
// Defaults Status to StatusActive if empty.
// Returns ErrDuplicateCode if a duplicate code exists for the tenant.
func (uc *CreateUseCase) Execute(ctx context.Context, in domain.CreateInput) (*domain.Project, error) {
	status := in.Status
	if status == "" {
		status = domain.StatusActive
	}
	now := time.Now().UTC()
	p := &domain.Project{
		ID:             uuid.New(),
		TenantID:       in.TenantID,
		Code:           in.Code,
		Name:           in.Name,
		CustomerID:     in.CustomerID,
		ContractAmount: in.ContractAmount,
		StartDate:      in.StartDate,
		EndDate:        in.EndDate,
		Status:         status,
		Address:        in.Address,
		Manager:        in.Manager,
		Remark:         in.Remark,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("project create validate: %w", err)
	}
	if err := uc.repo.Create(ctx, p); err != nil {
		if errors.Is(err, ErrDuplicateCode) {
			return nil, ErrDuplicateCode
		}
		return nil, fmt.Errorf("project create: %w", err)
	}
	return p, nil
}
