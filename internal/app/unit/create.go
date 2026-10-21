// Package unit implements use cases for the unit_def catalogue.
package unit

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/unit"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/codegen"
)

// Repository defines the persistence interface required by unit use cases.
type Repository interface {
	Create(ctx context.Context, u *domain.UnitDef) error
	List(ctx context.Context, filter domain.ListFilter) ([]*domain.UnitDef, error)
	GetByID(ctx context.Context, tenantID, id uuid.UUID) (*domain.UnitDef, error)
	Delete(ctx context.Context, tenantID, id uuid.UUID) error
}

// CreateUseCase creates a tenant-custom unit.
type CreateUseCase struct {
	repo  Repository
	codes codegen.Source
}

// NewCreateUseCase constructs the use case.
func NewCreateUseCase(repo Repository) *CreateUseCase {
	return &CreateUseCase{repo: repo, codes: codegen.Default()}
}

// WithCodeSource replaces the clock/randomness used for generated codes (tests).
func (uc *CreateUseCase) WithCodeSource(s codegen.Source) *CreateUseCase {
	uc.codes = s
	return uc
}

// Execute validates and persists the new unit.
// An empty Code is server-assigned (U + yymmdd + 4 symbols, see codegen) and
// regenerated on a unique violation; an explicit Code is used verbatim.
func (uc *CreateUseCase) Execute(ctx context.Context, in domain.CreateInput) (*domain.UnitDef, error) {
	if in.TenantID == uuid.Nil {
		return nil, fmt.Errorf("create unit: tenant_id is required")
	}
	if in.Name == "" {
		return nil, fmt.Errorf("create unit: name is required")
	}

	validTypes := map[domain.UnitType]struct{}{
		domain.UnitTypeCount:  {},
		domain.UnitTypeWeight: {},
		domain.UnitTypeLength: {},
		domain.UnitTypeVolume: {},
		domain.UnitTypeArea:   {},
		domain.UnitTypeTime:   {},
	}
	if _, ok := validTypes[in.UnitType]; !ok {
		return nil, fmt.Errorf(
			"create unit: invalid unit_type %q: must be one of count|weight|length|volume|area|time",
			string(in.UnitType),
		)
	}

	now := time.Now().UTC()
	u := &domain.UnitDef{
		ID:        uuid.New(),
		TenantID:  in.TenantID,
		Code:      in.Code,
		Name:      in.Name,
		UnitType:  in.UnitType,
		IsSystem:  false,
		CreatedAt: now,
		UpdatedAt: now,
	}

	err := uc.codes.Create(codegen.PrefixUnit, in.Code, codegen.IsUniqueViolation, func(code string) error {
		u.Code = code
		return uc.repo.Create(ctx, u)
	})
	if err != nil {
		return nil, fmt.Errorf("create unit: %w", err)
	}
	return u, nil
}
