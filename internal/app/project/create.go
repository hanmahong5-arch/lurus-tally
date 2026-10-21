package project

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/project"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/codegen"
)

// CreateUseCase creates a new project.
type CreateUseCase struct {
	repo  Repository
	codes codegen.Source
}

// NewCreateUseCase constructs a CreateUseCase.
func NewCreateUseCase(repo Repository) *CreateUseCase {
	return &CreateUseCase{repo: repo, codes: codegen.Default()}
}

// WithCodeSource replaces the clock/randomness used for generated codes (tests).
func (uc *CreateUseCase) WithCodeSource(s codegen.Source) *CreateUseCase {
	uc.codes = s
	return uc
}

// Execute validates the input and creates a new Project.
// Defaults Status to StatusActive if empty.
// An empty Code is server-assigned (PJ + yymmdd + 4 symbols, see codegen) and
// redrawn on ErrDuplicateCode; the domain invariant "a project has a code" is
// unchanged, so Validate runs once the code is in place.
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
	isDup := func(err error) bool { return errors.Is(err, ErrDuplicateCode) }
	err := uc.codes.Create(codegen.PrefixProject, in.Code, isDup, func(code string) error {
		p.Code = code
		if err := p.Validate(); err != nil {
			return fmt.Errorf("project create validate: %w", err)
		}
		if err := uc.repo.Create(ctx, p); err != nil {
			if errors.Is(err, ErrDuplicateCode) {
				return ErrDuplicateCode
			}
			return fmt.Errorf("project create: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return p, nil
}
