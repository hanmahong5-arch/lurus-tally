package horticulture

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/horticulture"
)

// UpdateUseCase applies partial updates to an existing nursery dict entry.
type UpdateUseCase struct {
	repo Repository
}

// NewUpdateUseCase constructs an UpdateUseCase.
func NewUpdateUseCase(repo Repository) *UpdateUseCase {
	return &UpdateUseCase{repo: repo}
}

// Execute fetches the existing entry, applies non-nil fields from UpdateInput, validates,
// and persists the changes.
func (uc *UpdateUseCase) Execute(ctx context.Context, tenantID, id uuid.UUID, in domain.UpdateInput) (*domain.NurseryDict, error) {
	d, err := uc.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("nursery dict update fetch: %w", err)
	}

	if in.Name != nil {
		d.Name = *in.Name
	}
	if in.LatinName != nil {
		d.LatinName = *in.LatinName
	}
	if in.Family != nil {
		d.Family = *in.Family
	}
	if in.Genus != nil {
		d.Genus = *in.Genus
	}
	if in.Type != nil {
		d.Type = *in.Type
	}
	if in.IsEvergreen != nil {
		d.IsEvergreen = *in.IsEvergreen
	}
	if in.ClimateZones != nil {
		d.ClimateZones = in.ClimateZones
	}
	if in.BestSeason != nil {
		d.BestSeason = *in.BestSeason
	}
	if len(in.SpecTemplate) > 0 {
		d.SpecTemplate = in.SpecTemplate
	}
	if in.DefaultUnitID != nil {
		d.DefaultUnitID = in.DefaultUnitID
	}
	if in.PhotoURL != nil {
		d.PhotoURL = *in.PhotoURL
	}
	if in.Remark != nil {
		d.Remark = *in.Remark
	}
	d.UpdatedAt = time.Now().UTC()

	if err := d.Validate(); err != nil {
		return nil, fmt.Errorf("nursery dict update validate: %w", err)
	}
	if err := uc.repo.Update(ctx, d); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("nursery dict update: %w", err)
	}
	return d, nil
}
