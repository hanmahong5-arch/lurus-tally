// Package erasure runs the PIPL §47 / GDPR Art.17 erasure cascade that
// lurus-platform drives into tally: when a platform account is purged, the
// personal data tally holds for that data subject must be erased too, or it
// survives as orphaned PII (PIPL §47 exposure).
//
// Scope is the DATA SUBJECT, not the company. A platform account is pinned to a
// tenant's bootstrap owner (migration 000051), so erasure redacts that owner's
// identity PII and unlinks the account — the tenant's business records (stock,
// bills, ledgers) are deliberately left intact.
package erasure

import (
	"context"
	"fmt"
	"log/slog"
)

// Repo redacts the personal data of the tally identity tied to a platform
// account, across every tenant that account owns, and unlinks the account.
// Returns the number of tenants affected.
type Repo interface {
	EraseByPlatformAccount(ctx context.Context, accountID int64) (int, error)
}

// Service is the erasure use case.
type Service struct {
	repo Repo
	log  *slog.Logger
}

// NewService constructs the service. A nil logger falls back to slog.Default.
func NewService(repo Repo, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{repo: repo, log: log}
}

// Erase redacts the PII of the tally identity owned by platform accountID and
// unlinks it, returning the number of tenants affected. It is idempotent: a
// replay (account already erased, or never present in tally) affects zero
// tenants and returns (0, nil) — the caller maps zero to a 200 no_data,
// matching the platform erase contract's replay semantics.
func (s *Service) Erase(ctx context.Context, accountID int64) (int, error) {
	if accountID <= 0 {
		return 0, fmt.Errorf("erasure: invalid account_id %d", accountID)
	}
	n, err := s.repo.EraseByPlatformAccount(ctx, accountID)
	if err != nil {
		return 0, fmt.Errorf("erasure: account %d: %w", accountID, err)
	}
	s.log.InfoContext(ctx, "privacy-erase: tally cascade applied",
		"account_id", accountID, "tenants_affected", n)
	return n, nil
}
