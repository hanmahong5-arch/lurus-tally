// Package subscription holds the SQL behind the subscription lock.
package subscription

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// Resolver finds the platform account that holds a shop plan.
type Resolver struct{ db *sql.DB }

// NewResolver builds a Resolver.
func NewResolver(db *sql.DB) *Resolver { return &Resolver{db: db} }

// PlanAccountID prefers tenant.platform_account_id, then the owner mapped
// account; found=false when neither exists.
func (r *Resolver) PlanAccountID(ctx context.Context, tenantID uuid.UUID) (int64, bool, error) {
	var id sql.NullInt64
	err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(t.platform_account_id,
			(SELECT m.platform_account_id FROM tally.user_identity_mapping m
			  WHERE m.tenant_id = t.id AND m.is_owner = true AND m.platform_account_id IS NOT NULL
			  ORDER BY m.created_at LIMIT 1))
		FROM tally.tenant t WHERE t.id = $1`, tenantID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("subscription resolver: %w", err)
	}
	if !id.Valid || id.Int64 <= 0 {
		return 0, false, nil
	}
	return id.Int64, true, nil
}
