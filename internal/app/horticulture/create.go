package horticulture

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/horticulture"
)

// CreateUseCase creates a new nursery dictionary entry.
type CreateUseCase struct {
	repo Repository
}

// NewCreateUseCase constructs a CreateUseCase.
func NewCreateUseCase(repo Repository) *CreateUseCase {
	return &CreateUseCase{repo: repo}
}

// Execute validates the input and creates a new NurseryDict entry.
// Returns ErrDuplicateName if a duplicate name exists for the tenant.
func (uc *CreateUseCase) Execute(ctx context.Context, in domain.CreateInput) (*domain.NurseryDict, error) {
	t := in.Type
	if t == "" {
		t = domain.NurseryTypeTree
	}
	spec := in.SpecTemplate
	if len(spec) == 0 {
		spec = json.RawMessage("{}")
	}
	now := time.Now().UTC()
	d := &domain.NurseryDict{
		ID:            uuid.New(),
		TenantID:      in.TenantID,
		Name:          in.Name,
		LatinName:     in.LatinName,
		Family:        in.Family,
		Genus:         in.Genus,
		Type:          t,
		IsEvergreen:   in.IsEvergreen,
		ClimateZones:  in.ClimateZones,
		BestSeason:    in.BestSeason,
		SpecTemplate:  spec,
		DefaultUnitID: in.DefaultUnitID,
		PhotoURL:      in.PhotoURL,
		Remark:        in.Remark,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := d.Validate(); err != nil {
		return nil, fmt.Errorf("nursery dict create validate: %w", err)
	}
	if err := uc.repo.Create(ctx, d); err != nil {
		if errors.Is(err, ErrDuplicateName) {
			return nil, ErrDuplicateName
		}
		return nil, fmt.Errorf("nursery dict create: %w", err)
	}
	return d, nil
}
