// Package tenant implements use cases for tenant profile management.
package tenant

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	repoTenant "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/tenant"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/tenant"
)

// ErrInconsistentTenantState is returned when a user_identity_mapping exists
// but no tenant_profile is found for the same tenant. This indicates the
// initial Bootstrap was interrupted between the two inserts (atomicity bug)
// and requires operator intervention.
var ErrInconsistentTenantState = errors.New("tenant has mapping but no profile (inconsistent state)")

// ChooseProfileInput carries the chosen profile type and the authenticated
// user's identity (from JWT claims). The same call handles both first-time
// onboarding (creates tenant + mapping + profile in one tx) and the idempotent
// no-op case (caller already has a profile).
type ChooseProfileInput struct {
	ZitadelSub  string // required
	Email       string // optional but recommended
	DisplayName string // optional
	ProfileType string // "cross_border" | "retail"
}

// ChooseProfileUseCase implements first-login onboarding.
//
// Idempotency contract:
//
//	1st call (fresh user)            → creates tenant+mapping+profile, returns profile (201)
//	2nd call, same profile_type      → returns existing profile (no error, 200)
//	2nd call, different profile_type → ErrProfileAlreadySet (409)
//
// All inserts in the fresh path are wrapped in a single transaction so partial
// state is impossible. RLS is honoured via SET LOCAL app.tenant_id inside the tx.
type ChooseProfileUseCase struct {
	store repoTenant.BootstrapStore
}

// NewChooseProfileUseCase wires the use case to a BootstrapStore.
func NewChooseProfileUseCase(store repoTenant.BootstrapStore) *ChooseProfileUseCase {
	return &ChooseProfileUseCase{store: store}
}

// Execute runs the onboarding logic. See type doc for idempotency guarantees.
func (uc *ChooseProfileUseCase) Execute(ctx context.Context, in ChooseProfileInput) (*domain.TenantProfile, error) {
	if in.ZitadelSub == "" {
		return nil, fmt.Errorf("choose profile: zitadel_sub is required")
	}

	pt := domain.ProfileType(in.ProfileType)
	if pt != domain.ProfileTypeCrossBorder && pt != domain.ProfileTypeRetail {
		return nil, fmt.Errorf("choose profile: %w", domain.ErrInvalidProfileType)
	}

	// Lookup existing mapping (sub → tenant). RLS on user_identity_mapping is
	// relaxed (migration 000016) so this works without a pre-set tenant_id.
	mapping, err := uc.store.GetMappingBySub(ctx, in.ZitadelSub)
	if err != nil {
		return nil, fmt.Errorf("choose profile: lookup mapping: %w", err)
	}

	if mapping != nil {
		// Returning user — check if profile already exists for their tenant.
		existing, err := uc.store.GetProfileByTenantID(ctx, mapping.TenantID)
		if err != nil {
			return nil, fmt.Errorf("choose profile: lookup profile: %w", err)
		}
		if existing == nil {
			// Mapping without profile is an inconsistent state (Bootstrap is atomic).
			return nil, ErrInconsistentTenantState
		}
		if existing.ProfileType == pt {
			// Idempotent: same choice, return existing profile (no error).
			return existing, nil
		}
		// Different choice → conflict (caller decides whether to ignore or surface).
		return nil, fmt.Errorf("choose profile: %w", domain.ErrProfileAlreadySet)
	}

	// First-time user — atomic bootstrap.
	tenantID := uuid.New()
	tenantName := deriveTenantName(in.DisplayName, in.Email, in.ZitadelSub)

	if err := uc.store.Bootstrap(ctx, repoTenant.BootstrapInput{
		TenantID:    tenantID,
		TenantName:  tenantName,
		ZitadelSub:  in.ZitadelSub,
		Email:       in.Email,
		DisplayName: in.DisplayName,
		ProfileType: pt,
	}); err != nil {
		return nil, fmt.Errorf("choose profile: bootstrap: %w", err)
	}

	// Re-fetch to return the canonical row (timestamps from DB).
	created, err := uc.store.GetProfileByTenantID(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("choose profile: post-bootstrap fetch: %w", err)
	}
	if created == nil {
		// Should never happen — Bootstrap committed but profile not found.
		return nil, fmt.Errorf("choose profile: bootstrap succeeded but profile not found")
	}
	return created, nil
}

// deriveTenantName produces a sensible default name for the tenant row when
// no explicit name is provided at onboarding time. The user can rename later.
func deriveTenantName(displayName, email, sub string) string {
	if displayName != "" {
		return displayName + " 的企业"
	}
	if email != "" {
		return email + " 的企业"
	}
	if len(sub) >= 8 {
		return "Tenant " + sub[:8]
	}
	return "Tenant " + sub
}
