// Package tenant — get_profile.go: read the current tenant's profile by ID.
package tenant

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	repoTenant "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/tenant"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/tenant"
)

// ErrProfileNotFound is returned when no tenant_profile row exists for the
// given tenant_id. Handlers should map this to 404.
var ErrProfileNotFound = errors.New("tenant profile not found")

// GetTenantProfileUseCase reads the canonical profile row for a tenant.
// It is the read-side of POST /tenant/profile (write-side is ChooseProfileUseCase).
//
// Per DL-2: this returns the SAME field set for every profile_type — there is
// no per-profile branching in the response. UI consumers branch on
// profile_type to show/hide their own fields.
type GetTenantProfileUseCase struct {
	store repoTenant.BootstrapStore
}

// NewGetTenantProfileUseCase builds the use case.
func NewGetTenantProfileUseCase(store repoTenant.BootstrapStore) *GetTenantProfileUseCase {
	return &GetTenantProfileUseCase{store: store}
}

// Execute returns the TenantProfile for the given tenant. Returns
// ErrProfileNotFound when no row exists (caller maps to 404).
func (uc *GetTenantProfileUseCase) Execute(ctx context.Context, tenantID uuid.UUID) (*domain.TenantProfile, error) {
	if tenantID == uuid.Nil {
		return nil, fmt.Errorf("get tenant profile: tenant_id is required")
	}
	p, err := uc.store.GetProfileByTenantID(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("get tenant profile: %w", err)
	}
	if p == nil {
		return nil, ErrProfileNotFound
	}
	return p, nil
}
