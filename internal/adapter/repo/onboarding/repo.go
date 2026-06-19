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
	appob "github.com/hanmahong5-arch/lurus-tally/internal/app/onboarding"
)

// Repo implements DemoDeleter and DemoSaleWriter for the onboarding use case.
type Repo struct {
	db *sql.DB
}

// New creates a Repo backed by db.
func New(db *sql.DB) *Repo {
	return &Repo{db: db}
}

var (
	_ appob.DemoDeleter    = (*Repo)(nil)
	_ appob.DemoSaleWriter = (*Repo)(nil)
)

// demoSaleBillNoPrefix marks seeded demo sale bills; bill_no = prefix + uuid8.
const demoSaleBillNoPrefix = "DEMO-S-"

// InsertDemoSale writes one approved demo sale (bill_head + bill_item, status=2
// 已审核, remark="DEMO", partner NULL = walk-in) inside a tenant-pinned tx so RLS
// binds, and returns the new bill_item id. The caller links its stock movement
// to this id (reference_type=sale), mirroring a real approved sale: revenue /
// margin reports read the bill, velocity reads the movement, and clear-demo
// removes both. creator_id falls back to the tenant id — the same machine-actor
// sentinel the payment/replenish handlers use when there is no human sub.
func (r *Repo) InsertDemoSale(ctx context.Context, in appob.DemoSaleBill) (uuid.UUID, error) {
	tx, err := dbscope.BeginTx(ctx, r.db, nil)
	if err != nil {
		return uuid.Nil, fmt.Errorf("onboarding repo: begin demo-sale tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	headID := uuid.New()
	itemID := uuid.New()
	billNo := demoSaleBillNoPrefix + headID.String()[:8]
	total := in.Qty.Mul(in.UnitPrice)

	const insHead = `
		INSERT INTO tally.bill_head
			(id, tenant_id, bill_no, bill_type, sub_type, status, creator_id, bill_date, total_amount, remark, source)
		VALUES ($1, $2, $3, '出库', '销售', 2, $4, $5, $6, 'DEMO', 'demo')`
	if _, err := tx.ExecContext(ctx, insHead,
		headID, in.TenantID, billNo, in.TenantID, in.OccurredAt, total.String(),
	); err != nil {
		return uuid.Nil, fmt.Errorf("onboarding repo: insert demo sale head: %w", err)
	}

	const insItem = `
		INSERT INTO tally.bill_item
			(id, tenant_id, head_id, product_id, warehouse_id, qty, unit_price, purchase_price, line_amount)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	if _, err := tx.ExecContext(ctx, insItem,
		itemID, in.TenantID, headID, in.ProductID, in.WarehouseID,
		in.Qty.String(), in.UnitPrice.String(), in.UnitCost.String(), total.String(),
	); err != nil {
		return uuid.Nil, fmt.Errorf("onboarding repo: insert demo sale item: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return uuid.Nil, fmt.Errorf("onboarding repo: commit demo sale: %w", err)
	}
	return itemID, nil
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
	// stock_lot in case a movement references a lot. The seeded demo sale bills
	// (remark='DEMO') go too: bill_item.product_id is RESTRICT, so a demo bill
	// would block the product DELETE; deleting bill_head cascades bill_item
	// (ON DELETE CASCADE). Each delete is tenant- and demo-scoped; $1 is reused.
	const demoSelect = `SELECT id FROM tally.product ` +
		`WHERE tenant_id = $1 AND remark = 'DEMO' AND deleted_at IS NULL`
	childDeletes := []string{
		`DELETE FROM tally.bill_head      WHERE tenant_id = $1 AND remark = 'DEMO'`,
		`DELETE FROM tally.stock_movement WHERE tenant_id = $1 AND product_id IN (` + demoSelect + `)`,
		`DELETE FROM tally.stock_lot      WHERE tenant_id = $1 AND product_id IN (` + demoSelect + `)`,
		`DELETE FROM tally.stock_snapshot WHERE tenant_id = $1 AND product_id IN (` + demoSelect + `)`,
	}
	for _, q := range childDeletes {
		if _, err := tx.ExecContext(ctx, q, tenantID); err != nil {
			return fmt.Errorf("onboarding repo: clear demo child rows: %w", err)
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
