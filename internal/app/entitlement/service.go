// Package entitlement answers "does the calling tenant's plan grant feature X?"
// by resolving the tenant's platform account and reading the entitlements
// platform authored for the active plan. It is the enforcement half of the
// billing integration: without it every tenant behaves like the top tier
// (paid == free).
package entitlement

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/google/uuid"
)

// productID is tally's identifier in the platform product registry. Canonical
// bare form, kept in sync with app/billing.ProductID (both are "tally").
const productID = "tally"

// TenantResolver maps a tally tenant to its pinned platform account (the plan
// holder). *repo/tenant.TenantRepo satisfies it — the SAME mapping the usage
// reporter uses, so the entitlement gate and metering agree on which account a
// tenant's activity belongs to.
type TenantResolver interface {
	GetPlatformAccountID(ctx context.Context, tenantID uuid.UUID) (int64, bool, error)
}

// EntitlementsPort reads the entitlements platform authored for an account's
// active plan. *platformclient.Client satisfies it.
type EntitlementsPort interface {
	GetEntitlements(ctx context.Context, accountID int64, productID string) (map[string]string, error)
}

// ErrUnauthenticated is returned when no caller identity (tenant) is present.
var ErrUnauthenticated = errors.New("entitlement: caller is not authenticated")

// Service checks plan entitlements for the calling TENANT. tally's plan is
// per-tenant: the subscription lives on the tenant's bootstrap-owner account
// (migration 000051), shared by every member and every PAT/automation caller.
// Resolving by tenant (not by an individual Zitadel sub) is what makes the gate
// correct for non-owner users and PAT callers — keying on sub would wrongly
// deny everyone but the owner (and 401 every PAT caller, whose sub is empty).
type Service struct {
	tenants  TenantResolver
	platform EntitlementsPort
	log      *slog.Logger
}

// NewService constructs a Service. log may be nil (falls back to slog default).
func NewService(tenants TenantResolver, platform EntitlementsPort, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{tenants: tenants, platform: platform, log: log}
}

// Has reports whether the calling tenant's plan grants a truthy entitlement for
// `key` under the tally product.
//
// Availability posture (matches tally's billing graceful-degrade):
//   - nil tenant                   → ErrUnauthenticated (no tenant in context)
//   - tenant has no pinned account → (false, nil): no resolvable plan, so deny
//     — fail-CLOSED on a definitive "no"
//   - resolver / platform error    → (true, nil): a blip must not lock paying
//     tenants out, so fail-OPEN and log a warning
//   - entitlements fetched         → (truthy(value), nil)
func (s *Service) Has(ctx context.Context, tenantID uuid.UUID, key string) (bool, error) {
	if tenantID == uuid.Nil {
		return false, ErrUnauthenticated
	}
	accountID, ok, err := s.tenants.GetPlatformAccountID(ctx, tenantID)
	if err != nil {
		s.log.Warn("entitlement: tenant->account resolve failed — failing open",
			"key", key, "tenant", tenantID.String(), "err", err)
		return true, nil
	}
	if !ok {
		// Tenant not yet pinned to a platform account → no resolvable plan → deny.
		return false, nil
	}
	ents, err := s.platform.GetEntitlements(ctx, accountID, productID)
	if err != nil {
		s.log.Warn("entitlement: fetch failed — failing open",
			"key", key, "account_id", accountID, "err", err)
		return true, nil
	}
	return truthy(ents[key]), nil
}

// truthy treats absent/empty and the usual false-ish strings as not-granted;
// any other non-empty value (e.g. "true", "advanced", "2000") grants the gate.
func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "false", "0", "no", "off":
		return false
	default:
		return true
	}
}
