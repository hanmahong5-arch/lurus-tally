// Package tenant — get_me.go: assembles the /api/v1/me payload from sub.
package tenant

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	repoTenant "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/tenant"
	domainacct "github.com/hanmahong5-arch/lurus-tally/internal/domain/account"
	domainteam "github.com/hanmahong5-arch/lurus-tally/internal/domain/team"
)

// GetMeInput carries the authenticated user's OIDC subject.
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
	UserSub     string `json:"user_id"`       // OIDC subject
	TenantID    string `json:"tenant_id"`     // "" when not yet onboarded
	Email       string `json:"email"`         // "" when not in mapping
	DisplayName string `json:"display_name"`  // ""
	Role        string `json:"role"`          // owner|manager|clerk|cashier|finance; "" when not yet onboarded
	RoleLabel   string `json:"role_label"`    // 老板|店长|店员|收银|财务; "" when not yet onboarded
	IsOwner     bool   `json:"is_owner"`      // false when not yet onboarded
	ProfileType string `json:"profile_type"`  // "" when no profile set
	IsFirstTime bool   `json:"is_first_time"` // true when no mapping yet
	ShopName    string `json:"shop_name"`     // the name people know the shop by (print name, else tenant name); "" when not yet onboarded

	// Account-center additions (Phase 3). Empty when no per-user profile
	// row exists yet — the row is created lazily on first PUT /account/profile.
	Phone     string `json:"phone,omitempty"`
	AvatarURL string `json:"avatar_url,omitempty"`
}

// ProfileGetter is the minimal port the use case needs from the account
// layer to enrich /me. Implemented by *app/account.GetProfile.
type ProfileGetter interface {
	Execute(ctx context.Context, tenantID uuid.UUID, userID string) (*domainacct.Profile, error)
}

// GetMeUseCase looks up a user's full identity context by OIDC subject.
//
// When profileGetter is supplied, the use case also fans out to the
// account.user_profile table to surface display_name overrides, phone, and
// avatar_url on the response. nil profileGetter is supported so dev / tests
// that haven't wired the account layer still work.
type GetMeUseCase struct {
	store         repoTenant.BootstrapStore
	profileGetter ProfileGetter
	roleResolver  RoleResolver
	shopNamer     ShopNamer
}

// ShopNamer yields the name people know a shop by. Implemented by
// *app/team.Service.ShopDisplayName so invite, join and /me agree on it.
type ShopNamer interface {
	ShopDisplayName(ctx context.Context, tenantID uuid.UUID) string
}

// WithShopNamer returns a copy that reports shop_name on /me.
// nil namer returns the receiver unchanged.
func (uc *GetMeUseCase) WithShopNamer(n ShopNamer) *GetMeUseCase {
	if n == nil {
		return uc
	}
	cp := *uc
	cp.shopNamer = n
	return &cp
}

// RoleResolver resolves the person's current shop role (platform membership,
// cached). Implemented by *app/team.Service.ResolveRole.
type RoleResolver interface {
	ResolveRole(ctx context.Context, tenantID uuid.UUID, sub string) (domainteam.Role, error)
}

// WithRoleResolver returns a copy that reports the live shop role on /me
// instead of the stored projection. nil resolver returns the receiver unchanged.
func (uc *GetMeUseCase) WithRoleResolver(r RoleResolver) *GetMeUseCase {
	if r == nil {
		return uc
	}
	cp := *uc
	cp.roleResolver = r
	return &cp
}

// NewGetMeUseCase constructs the use case without account enrichment.
func NewGetMeUseCase(store repoTenant.BootstrapStore) *GetMeUseCase {
	return &GetMeUseCase{store: store}
}

// WithProfileGetter returns a copy of the use case that also enriches /me
// with the account profile row. Returns the receiver unchanged when getter
// is nil (caller hasn't wired the account layer yet).
func (uc *GetMeUseCase) WithProfileGetter(getter ProfileGetter) *GetMeUseCase {
	if getter == nil {
		return uc
	}
	cp := *uc
	cp.profileGetter = getter
	return &cp
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
		IsFirstTime: false,
	}
	// The stored role may still be a pre-multi-member value (admin/viewer):
	// always report the current vocabulary. A resolver, when wired, gives the
	// live platform-backed role; a person it no longer knows keeps the stored one.
	role := domainteam.NormalizeLegacy(mapping.Role, mapping.IsOwner)
	if uc.roleResolver != nil {
		if live, rerr := uc.roleResolver.ResolveRole(ctx, mapping.TenantID, in.UserSub); rerr == nil {
			role = live
		}
	}
	if uc.shopNamer != nil {
		out.ShopName = uc.shopNamer.ShopDisplayName(ctx, mapping.TenantID)
	}
	out.Role = string(role)
	out.RoleLabel = role.Label()
	out.IsOwner = role == domainteam.RoleOwner
	if profile != nil {
		out.ProfileType = string(profile.ProfileType)
	}

	// Account-center overlay: display_name override, phone, avatar_url.
	// Best-effort — a failure here must not break /me.
	if uc.profileGetter != nil {
		acct, perr := uc.profileGetter.Execute(ctx, mapping.TenantID, in.UserSub)
		if perr == nil && acct != nil {
			if acct.DisplayName != "" {
				out.DisplayName = acct.DisplayName
			}
			out.Phone = acct.Phone
			if acct.HasAvatar {
				out.AvatarURL = "/api/v1/account/avatar"
			}
		}
	}

	return out, nil
}
