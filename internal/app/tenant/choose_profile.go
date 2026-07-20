// Package tenant implements use cases for tenant profile management.
package tenant

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	repoTenant "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/tenant"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/tenant"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/platformclient"
)

// PlatformAccountUpserter abstracts the platform account upsert call so this
// use case stays testable without a real HTTP client. lurus-platform is the
// canonical owner of account / wallet / subscription / VIP records — Tally
// must register every IdP subject there on first onboarding so the user can
// subscribe, top up wallet, and receive notifications.
type PlatformAccountUpserter interface {
	UpsertAccount(ctx context.Context, req platformclient.UpsertAccountRequest) (*platformclient.Account, error)
}

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
	IDPSubject  string // required (OIDC `sub`)
	Email       string // optional but recommended
	DisplayName string // optional
	ProfileType string // "cross_border" | "retail" | "horticulture"
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
	store    repoTenant.BootstrapStore
	upserter PlatformAccountUpserter // may be nil when platform integration is disabled
	logger   *slog.Logger
}

// NewChooseProfileUseCase wires the use case to a BootstrapStore. The
// upserter and logger are optional — passing nil disables the platform
// account provisioning step (clusters without PLATFORM_INTERNAL_KEY) and
// falls back to slog.Default() respectively.
func NewChooseProfileUseCase(store repoTenant.BootstrapStore, upserter PlatformAccountUpserter, logger *slog.Logger) *ChooseProfileUseCase {
	if logger == nil {
		logger = slog.Default()
	}
	return &ChooseProfileUseCase{store: store, upserter: upserter, logger: logger}
}

// Execute runs the onboarding logic. See type doc for idempotency guarantees.
func (uc *ChooseProfileUseCase) Execute(ctx context.Context, in ChooseProfileInput) (*domain.TenantProfile, error) {
	if in.IDPSubject == "" {
		return nil, fmt.Errorf("choose profile: idp_subject is required")
	}

	pt := domain.ProfileType(in.ProfileType)
	if !domain.IsUserSelectableProfile(pt) {
		return nil, fmt.Errorf("choose profile: %w", domain.ErrInvalidProfileType)
	}

	// Lookup existing mapping (sub → tenant). RLS on user_identity_mapping is
	// relaxed (migration 000016) so this works without a pre-set tenant_id.
	mapping, err := uc.store.GetMappingBySub(ctx, in.IDPSubject)
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
			// Heal path: re-run platform upsert in case a previous call failed
			// (network blip, platform rolling upgrade) and left tally with a
			// local tenant but no platform account. Upsert is idempotent. This
			// also back-fills platform_account_id for tenants onboarded before
			// Wave 2 (the column was added in migration 000051).
			uc.upsertPlatformAccount(ctx, mapping.TenantID, in)
			return existing, nil
		}
		// Different choice → conflict (caller decides whether to ignore or surface).
		return nil, fmt.Errorf("choose profile: %w", domain.ErrProfileAlreadySet)
	}

	// First-time user — atomic bootstrap.
	tenantID := uuid.New()
	tenantName := deriveTenantName(in.DisplayName, in.Email, in.IDPSubject)

	if err := uc.store.Bootstrap(ctx, repoTenant.BootstrapInput{
		TenantID:    tenantID,
		TenantName:  tenantName,
		IDPSubject:  in.IDPSubject,
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

	// KS1 denominator: one signup per brand-new tenant. Counted only on this
	// created path — returning-user logins fall through the mapping branch
	// above and never reach here, so the tally stays a true signup count.
	middleware.IncTenantSignup(string(pt))

	// Provisioning: register the user on lurus-platform so wallet /
	// subscription / VIP records exist from the very first login. Failure
	// is non-blocking — the user has a working tally tenant either way and
	// the next /tenant/profile call (idempotent path above) will heal.
	uc.upsertPlatformAccount(ctx, tenantID, in)

	return created, nil
}

// upsertPlatformAccount calls platform's account upsert endpoint and never
// returns an error to the caller — failures are logged at WARN so the user
// can still finish onboarding even if platform is briefly unavailable. The
// next ChooseProfile invocation (and any future reconcile worker) will
// re-attempt because platform's upsert is idempotent on idp_subject.
//
// When the JWT carries no `email` claim (admin users, username-only or
// phone-OTP logins) we synthesize a placeholder so platform still owns a
// canonical account row. The placeholder will be overwritten on a future
// call once the user adds a real email and signs in again.
func (uc *ChooseProfileUseCase) upsertPlatformAccount(ctx context.Context, tenantID uuid.UUID, in ChooseProfileInput) {
	if uc.upserter == nil {
		// Platform integration disabled (PLATFORM_INTERNAL_KEY unset). Same
		// behaviour as billing handler — degrade gracefully.
		return
	}
	email := in.Email
	if email == "" {
		email = in.IDPSubject + "@idp.local"
		uc.logger.Info("platform account upsert: synthesized email placeholder",
			slog.String("idp_subject", in.IDPSubject),
			slog.String("synthesized_email", email))
	}
	acc, err := uc.upserter.UpsertAccount(ctx, platformclient.UpsertAccountRequest{
		IDPSubject:  in.IDPSubject,
		Email:       email,
		DisplayName: in.DisplayName,
	})
	if err != nil {
		uc.logger.Warn("platform account upsert failed (non-blocking)",
			slog.String("idp_subject", in.IDPSubject),
			slog.String("error", err.Error()))
		return
	}
	uc.logger.Info("platform account upserted",
		slog.String("idp_subject", in.IDPSubject),
		slog.Int64("account_id", acc.ID))

	// Pin the account id on the tenant so the LLM usage reporter can attribute
	// spend (unified-billing Wave 2). Non-blocking: a failed write just means
	// the next onboarding/login heals it; usage events for an unpinned tenant
	// are skipped in shadow rather than misattributed.
	if acc.ID > 0 {
		if err := uc.store.SetPlatformAccountID(ctx, tenantID, acc.ID); err != nil {
			uc.logger.Warn("persist platform account_id failed (non-blocking)",
				slog.String("tenant_id", tenantID.String()),
				slog.Int64("account_id", acc.ID),
				slog.String("error", err.Error()))
		}
	}
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
