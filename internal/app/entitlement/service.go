// Package entitlement answers "does the caller's subscription plan grant
// feature X?" by resolving the caller's platform account and reading the
// entitlements platform authored for the active plan. It is the enforcement
// half of the billing integration: without it every account behaves like the
// top tier (paid == free).
package entitlement

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/platformclient"
)

// productID is tally's identifier in the platform product registry. Canonical
// bare form, kept in sync with app/billing.ProductID (both are "tally").
const productID = "tally"

// PlatformPort is the slice of the platform client the gate depends on.
// *platformclient.Client satisfies it.
type PlatformPort interface {
	GetAccountByZitadelSub(ctx context.Context, sub string) (*platformclient.Account, error)
	GetEntitlements(ctx context.Context, accountID int64, productID string) (map[string]string, error)
}

// ErrUnauthenticated is returned when no caller identity (Zitadel sub) is present.
var ErrUnauthenticated = errors.New("entitlement: caller is not authenticated")

// Service checks plan entitlements for the calling account.
type Service struct {
	platform PlatformPort
	log      *slog.Logger
}

// NewService constructs a Service. log may be nil (falls back to slog default).
func NewService(p PlatformPort, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{platform: p, log: log}
}

// Has reports whether the caller (by Zitadel sub) holds a truthy entitlement
// for `key` under the tally product.
//
// Availability posture (matches tally's billing graceful-degrade):
//   - empty sub                    → ErrUnauthenticated (caller not signed in)
//   - account not found            → (false, nil): no platform account means no
//     paid plan, so deny — fail-CLOSED on a definitive "no"
//   - platform unreachable / error → (true, nil): a platform blip must not lock
//     paying users out, so fail-OPEN and log a warning
//   - entitlements fetched         → (truthy(value), nil)
//
// The caller is rejected only when platform positively reports the entitlement
// absent — never merely because platform was momentarily unreachable.
func (s *Service) Has(ctx context.Context, sub, key string) (bool, error) {
	if sub == "" {
		return false, ErrUnauthenticated
	}
	acc, err := s.platform.GetAccountByZitadelSub(ctx, sub)
	if err != nil {
		if platformclient.IsCode(err, platformclient.ErrCodeNotFound) {
			return false, nil
		}
		s.log.Warn("entitlement: account resolve failed — failing open", "key", key, "err", err)
		return true, nil
	}
	ents, err := s.platform.GetEntitlements(ctx, acc.ID, productID)
	if err != nil {
		s.log.Warn("entitlement: fetch failed — failing open", "key", key, "account_id", acc.ID, "err", err)
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
