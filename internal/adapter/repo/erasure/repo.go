// Package erasure persists the PIPL §47 erasure cascade tally receives from
// lurus-platform. It redacts identity PII under the correct RLS pin and unlinks
// the platform account, touching only the data subject's personal data.
package erasure

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dbscope"
)

// Repo redacts platform-account-linked identity PII.
type Repo struct {
	db *sql.DB
}

// New constructs the repo over the shared pool.
func New(db *sql.DB) *Repo { return &Repo{db: db} }

// EraseByPlatformAccount finds every tenant whose bootstrap owner is platform
// account accountID, redacts that owner's user_identity_mapping PII
// (email / display_name / zitadel_sub → tombstone) and unlinks the account from
// the tenant. Personal data only — the tenant's business records are NOT
// touched (PIPL §47 scope is the data subject, not the company's books).
//
// Each tenant is processed under its own RLS pin (app.tenant_id = that tenant's
// id), so a stale pooled connection cannot mis-scope the redaction into a silent
// under-erasure. The redaction + unlink run in one transaction per tenant, so a
// partial cascade can never leave a redacted identity still linked.
//
// Idempotent: a replay finds no linked tenant (platform_account_id was cleared
// on the first successful run) and returns 0.
func (r *Repo) EraseByPlatformAccount(ctx context.Context, accountID int64) (int, error) {
	// tally.tenant is the (non-RLS) tenant registry → plain pool read.
	rows, err := r.db.QueryContext(ctx,
		`SELECT id FROM tally.tenant WHERE platform_account_id = $1`, accountID)
	if err != nil {
		return 0, fmt.Errorf("list tenants for account %d: %w", accountID, err)
	}
	var tenantIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return 0, fmt.Errorf("scan tenant id: %w", err)
		}
		tenantIDs = append(tenantIDs, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, fmt.Errorf("iterate tenants: %w", err)
	}
	_ = rows.Close()

	for _, tid := range tenantIDs {
		if err := r.eraseTenant(ctx, tid); err != nil {
			return 0, fmt.Errorf("erase tenant %s: %w", tid, err)
		}
	}
	return len(tenantIDs), nil
}

// eraseTenant redacts the bootstrap owner's PII and unlinks the account for one
// tenant, under that tenant's RLS pin, in a single transaction.
func (r *Repo) eraseTenant(ctx context.Context, tenantID string) error {
	return dbscope.WithPinnedConn(ctx, r.db, tenantID, func(ctx context.Context) error {
		tx, err := dbscope.BeginTx(ctx, r.db, nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()

		// Redact the bootstrap owner's PII. zitadel_sub is UNIQUE NOT NULL, so it
		// is tombstoned (not nulled) with the row id to stay unique while breaking
		// the OIDC sub → identity link, so the erased subject can no longer log in.
		if _, err := tx.ExecContext(ctx,
			`UPDATE tally.user_identity_mapping
			    SET email        = 'erased@tally.invalid',
			        display_name = NULL,
			        zitadel_sub  = 'erased:' || id::text,
			        updated_at   = now()
			  WHERE tenant_id = $1 AND is_owner = true`, tenantID); err != nil {
			return fmt.Errorf("redact owner identity: %w", err)
		}

		// Unlink the platform account so a replay finds nothing. tally.tenant is
		// not RLS-scoped; the pinned conn is harmless for this statement.
		if _, err := tx.ExecContext(ctx,
			`UPDATE tally.tenant SET platform_account_id = NULL WHERE id = $1`, tenantID); err != nil {
			return fmt.Errorf("unlink platform account: %w", err)
		}
		return tx.Commit()
	})
}
