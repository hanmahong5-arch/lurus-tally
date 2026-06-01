// Package onboarding implements the persistence operations needed to clear
// demo data seeded by the onboarding use case.
//
// Only demo-marked rows (remark='DEMO') are touched; production data is safe.
// Associated stock_movement and stock_snapshot rows are removed by FK cascade
// when the product row is deleted.
package onboarding

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dbscope"
)

// Repo implements DemoDeleter for the onboarding use case.
type Repo struct {
	db *sql.DB
}

// New creates a Repo backed by db.
func New(db *sql.DB) *Repo {
	return &Repo{db: db}
}

// DeleteDemoProducts hard-deletes all products with remark='DEMO' owned by
// tenantID. Stock snapshots and movements reference product_id via FK; the
// schema uses ON DELETE CASCADE so those rows are also removed automatically.
func (r *Repo) DeleteDemoProducts(ctx context.Context, tenantID uuid.UUID) error {
	const q = `
		DELETE FROM tally.product
		WHERE tenant_id = $1
		  AND remark = 'DEMO'
		  AND deleted_at IS NULL`

	dbh := dbscope.From(ctx, r.db)
	_, err := dbh.ExecContext(ctx, q, tenantID)
	if err != nil {
		return fmt.Errorf("onboarding repo: delete demo products: %w", err)
	}
	return nil
}
