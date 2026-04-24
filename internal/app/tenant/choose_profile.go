// Package tenant implements use cases for tenant profile management.
package tenant

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/tenant"
)

// profileRepository defines the subset of the repo needed by ChooseProfileUseCase.
type profileRepository interface {
	GetByTenantID(ctx context.Context, tenantID uuid.UUID) (*domain.TenantProfile, error)
	Create(ctx context.Context, p *domain.TenantProfile) error
}

// ChooseProfileInput carries the tenant ID and chosen profile type.
type ChooseProfileInput struct {
	TenantID    uuid.UUID
	ProfileType string
}

// ChooseProfileUseCase creates the initial profile record for a tenant.
// It explicitly rejects "hybrid" (admin-only) and any unknown type.
// It returns ErrProfileAlreadySet when a profile record exists.
type ChooseProfileUseCase struct {
	repo profileRepository
}

// NewChooseProfileUseCase constructs the use case.
func NewChooseProfileUseCase(repo profileRepository) *ChooseProfileUseCase {
	return &ChooseProfileUseCase{repo: repo}
}

// Execute validates input, guards against duplicate profiles, and persists.
func (uc *ChooseProfileUseCase) Execute(ctx context.Context, in ChooseProfileInput) (*domain.TenantProfile, error) {
	// Validate profile_type; hybrid is explicitly rejected for end-users.
	pt := domain.ProfileType(in.ProfileType)
	if pt != domain.ProfileTypeCrossBorder && pt != domain.ProfileTypeRetail {
		return nil, fmt.Errorf("choose profile: %w", domain.ErrInvalidProfileType)
	}

	// Guard: reject if a profile already exists.
	existing, err := uc.repo.GetByTenantID(ctx, in.TenantID)
	if err != nil {
		return nil, fmt.Errorf("choose profile: lookup existing: %w", err)
	}
	if existing != nil {
		return nil, fmt.Errorf("choose profile: %w", domain.ErrProfileAlreadySet)
	}

	p, err := domain.NewTenantProfile(in.TenantID, pt)
	if err != nil {
		return nil, fmt.Errorf("choose profile: build entity: %w", err)
	}

	if err := uc.repo.Create(ctx, p); err != nil {
		return nil, fmt.Errorf("choose profile: persist: %w", err)
	}
	return p, nil
}
