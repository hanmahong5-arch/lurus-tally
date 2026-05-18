// Package auth holds Personal Access Token use cases and the Repository port.
// The PG implementation lives in internal/adapter/repo/auth.
package auth

import (
	"context"
	"errors"

	"github.com/google/uuid"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/auth"
)

// ErrNotFound is returned when a lookup did not match any row.
// Middleware treats this identically to a bad token (HTTP 401).
var ErrNotFound = errors.New("auth: PAT not found")

// Repository is the persistence port for personal_access_token.
//
// Implementations must operate in the tally schema. GetByPrefix runs BEFORE
// app.tenant_id is set on the connection (see migration 000031 RLS relax),
// so the policy allows pre-auth lookup by prefix only.
type Repository interface {
	// Create inserts a new PAT row. The hash field must already be populated
	// by domain.GenerateToken; the repo never sees plaintext secrets.
	Create(ctx context.Context, p *domain.PAT) error

	// GetByPrefix returns the PAT with the given lookup prefix, or
	// ErrNotFound when no row matches. Revoked rows are NOT filtered here —
	// callers must check IsActive(now) after verifying the hash so that
	// timing-attack surface is the same whether the row exists or not.
	GetByPrefix(ctx context.Context, prefix string) (*domain.PAT, error)

	// ListByTenant returns all non-revoked PATs for the given tenant, newest first.
	ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]*domain.PAT, error)

	// Revoke sets revoked_at = now() for the (tenantID, id) pair. Returns
	// ErrNotFound when no matching row exists. Idempotent: revoking an
	// already-revoked row is a no-op (succeeds).
	Revoke(ctx context.Context, tenantID, id uuid.UUID) error

	// TouchLastUsed updates last_used_at to now(). Best-effort: callers
	// should ignore errors so a transient DB hiccup doesn't break a valid
	// auth path.
	TouchLastUsed(ctx context.Context, id uuid.UUID) error
}
