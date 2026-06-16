// Package onboarding implements the persistence operations needed to clear
// demo data seeded by the onboarding use case.
//
// Only demo-marked rows (remark='DEMO') are touched; production data is safe.
// The product's stock rows are deleted explicitly first: stock_movement is
// ON DELETE RESTRICT and stock_snapshot/stock_lot are NO ACTION, so a bare
// product DELETE would raise 23503 once a demo SKU has opening stock.
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
// tenantID, together with the stock rows they own. stock_movement.product_id is
// ON DELETE RESTRICT (migration 000022) and stock_snapshot/stock_lot reference
// product(id) with NO ACTION, so the child rows must be removed before the
// product — a bare product DELETE raises SQLSTATE 23503 once a demo SKU has
// opening stock. All deletes run in one tenant-pinned transaction so RLS binds
// them (dbscope.BeginTx inherits the request's app.tenant_id) and the cleanup is
// atomic.
func (r *Repo) DeleteDemoProducts(ctx context.Context, tenantID uuid.UUID) error {
	tx, err := dbscope.BeginTx(ctx, r.db, nil)
	if err != nil {
		return fmt.Errorf("onboarding repo: begin clear-demo tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Children first, then the product. stock_movement is deleted before
	// stock_lot in case a movement references a lot. Each delete is tenant- and
	// demo-scoped via the same subquery; $1 is reused for both predicates.
	const demoSelect = `SELECT id FROM tally.product ` +
		`WHERE tenant_id = $1 AND remark = 'DEMO' AND deleted_at IS NULL`
	childDeletes := []string{
		`DELETE FROM tally.stock_movement WHERE tenant_id = $1 AND product_id IN (` + demoSelect + `)`,
		`DELETE FROM tally.stock_lot      WHERE tenant_id = $1 AND product_id IN (` + demoSelect + `)`,
		`DELETE FROM tally.stock_snapshot WHERE tenant_id = $1 AND product_id IN (` + demoSelect + `)`,
	}
	for _, q := range childDeletes {
		if _, err := tx.ExecContext(ctx, q, tenantID); err != nil {
			return fmt.Errorf("onboarding repo: clear demo stock rows: %w", err)
		}
	}

	const delProducts = `
		DELETE FROM tally.product
		WHERE tenant_id = $1
		  AND remark = 'DEMO'
		  AND deleted_at IS NULL`
	if _, err := tx.ExecContext(ctx, delProducts, tenantID); err != nil {
		return fmt.Errorf("onboarding repo: delete demo products: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("onboarding repo: commit clear-demo: %w", err)
	}
	return nil
}
