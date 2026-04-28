// Package tenant — get_me.go: assembles the /api/v1/me payload from sub.
package tenant

import (
	"context"
	"fmt"

	repoTenant "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/tenant"
)

// GetMeInput carries the authenticated user's Zitadel sub.
// TenantID is intentionally not on the input — it is derived from the mapping.
type GetMeInput struct {
	UserSub string
}

// GetMeOutput is the response payload for GET /api/v1/me.
//
// Fields are ALL populated when the user has completed onboarding. Before
// onboarding (no mapping), TenantID + ProfileType are empty strings and
// IsFirstTime is true so the frontend can route to /setup.
type GetMeOutput struct {
	UserSub     string `json:"user_id"`        // Zitadel sub
	TenantID    string `json:"tenant_id"`      // "" when not yet onboarded
	Email       string `json:"email"`          // "" when not in mapping
	DisplayName string `json:"display_name"`   // ""
	Role        string `json:"role"`           // "" when not yet onboarded
	IsOwner     bool   `json:"is_owner"`       // false when not yet onboarded
	ProfileType string `json:"profile_type"`   // "" when no profile set
	IsFirstTime bool   `json:"is_first_time"`  // true when no mapping yet
}

// GetMeUseCase looks up a user's full identity context by Zitadel sub.
type GetMeUseCase struct {
	store repoTenant.BootstrapStore
}

// NewGetMeUseCase constructs the use case.
func NewGetMeUseCase(store repoTenant.BootstrapStore) *GetMeUseCase {
	return &GetMeUseCase{store: store}
}

// Execute resolves sub → mapping → tenant_id → profile.
//
// First-time users (no mapping yet) get a partially-empty payload with
// IsFirstTime=true. Returning users get a fully-populated payload.
func (uc *GetMeUseCase) Execute(ctx context.Context, in GetMeInput) (*GetMeOutput, error) {
	if in.UserSub == "" {
		return nil, fmt.Errorf("get me: user_sub is required")
	}

	mapping, err := uc.store.GetMappingBySub(ctx, in.UserSub)
	if err != nil {
		return nil, fmt.Errorf("get me: lookup mapping: %w", err)
	}

	if mapping == nil {
		// First login — no tenant yet.
		return &GetMeOutput{
			UserSub:     in.UserSub,
			IsFirstTime: true,
		}, nil
	}

	profile, err := uc.store.GetProfileByTenantID(ctx, mapping.TenantID)
	if err != nil {
		return nil, fmt.Errorf("get me: lookup profile: %w", err)
	}

	out := &GetMeOutput{
		UserSub:     in.UserSub,
		TenantID:    mapping.TenantID.String(),
		Email:       mapping.Email,
		DisplayName: mapping.DisplayName,
		Role:        mapping.Role,
		IsOwner:     mapping.IsOwner,
		IsFirstTime: false,
	}
	if profile != nil {
		out.ProfileType = string(profile.ProfileType)
	}
	return out, nil
}
