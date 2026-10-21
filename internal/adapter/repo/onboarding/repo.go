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

// ConvertedRemark marks a sample product kept as a real one after clear-demo;
// the checklist reads it so such a product does not count as the owner's own.
// Editing the product's note replaces it and the product then counts.
const ConvertedRemark = appob.ConvertedRemark

// demoCodePrefix is the code prefix of seeded sample products.
const demoCodePrefix = "DEMO-"

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
			(id, tenant_id, head_id, product_id, warehouse_id, unit_name, line_no, remark,
			 qty, unit_price, purchase_price, line_amount)
		VALUES ($1, $2, $3, $4, $5,
			COALESCE((SELECT unit_name FROM tally.product_sku
				WHERE tenant_id = $2 AND product_id = $4 AND is_default AND deleted_at IS NULL
				  AND COALESCE(unit_name, '') <> '' LIMIT 1), '件'),
			1, '', $6, $7, $8, $9)`
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

// DeleteDemoProducts runs ClearDemo and drops the result; kept for callers that
// only care about success.
func (r *Repo) DeleteDemoProducts(ctx context.Context, tenantID uuid.UUID) error {
	_, err := r.ClearDemo(ctx, tenantID)
	return err
}

// ClearDemo removes the tenant's demo data in one tenant-pinned transaction.
//
// Demo products (remark='DEMO') are hard-deleted together with the stock rows
// they own: stock_movement.product_id is ON DELETE RESTRICT (migration 000022)
// and stock_snapshot/stock_lot reference product(id) with NO ACTION, so a bare
// product DELETE raises SQLSTATE 23503 once a demo SKU has opening stock.
// A demo product that an approved bill the owner keeps still uses is not
// deleted: it loses the DEMO marker and becomes a real product (stock rows and
// SKUs stay) and is reported in ClearResult.Converted, so no history is broken.
// dbscope.BeginTx inherits the request's app.tenant_id so RLS binds every
// statement, and the cleanup is atomic.
func (r *Repo) ClearDemo(ctx context.Context, tenantID uuid.UUID) (*appob.ClearResult, error) {
	tx, err := dbscope.BeginTx(ctx, r.db, nil)
	if err != nil {
		return nil, fmt.Errorf("onboarding repo: begin clear-demo tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Bills to remove: every demo bill, plus any draft (or soft-deleted) bill
	// that only holds demo products -- e.g. the draft purchase the replenish page
	// generates from a demo shortage -- plus, recursively, every bill that returns
	// (link_bill_id) or amends (amendment_of_id) one of those and holds only demo
	// products, so a returned demo sale leaves no orphan return behind. Approved
	// real bills are never touched.
	delBills := DemoBillsSQL()

	// A demo product still used by a bill the owner keeps must not be deleted:
	// the bill would lose its goods. Collect those products first (before any
	// delete changes what delBills sees); they are converted to real products at
	// the end. Drafts (0) and voided bills (9) never moved stock and cannot be
	// approved again, so neither counts as use. A product the owner already
	// soft-deleted stays soft-deleted as history, only its marker goes.
	const keepTable = `demo_keep`
	if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE `+keepTable+` ON COMMIT DROP AS `+
		`SELECT DISTINCT p.id, p.code, p.name, (p.deleted_at IS NOT NULL) AS gone `+
		`FROM tally.bill_head h JOIN tally.bill_item i ON i.head_id = h.id JOIN tally.product p ON p.id = i.product_id `+
		`WHERE h.tenant_id = $1 AND p.tenant_id = $1 AND p.remark = 'DEMO' AND h.status NOT IN (0, 9) AND h.id NOT IN `+delBills,
		tenantID); err != nil {
		return nil, fmt.Errorf("onboarding repo: collect demo in use: %w", err)
	}
	const keepIDs = `(SELECT id FROM demo_keep)`
	const demoProd = `(SELECT id FROM tally.product WHERE tenant_id = $1 AND remark = 'DEMO' AND id NOT IN ` + keepIDs + `)`

	// Rows that point at those bills (payment_head.related_bill_id is RESTRICT,
	// invoice_bill.bill_id and the bill self-references are plain FKs) go first.
	// A kept bill (not in the set) just loses its pointer; bills in the set keep
	// theirs until the final DELETE so the recursive walk above stays stable.
	billChildren := []string{
		`UPDATE tally.bill_item SET link_item_id = NULL WHERE tenant_id = $1 AND link_item_id IN ` +
			`(SELECT id FROM tally.bill_item WHERE head_id IN ` + delBills + `)`,
		`DELETE FROM tally.payment_item WHERE tenant_id = $1 AND head_id IN ` +
			`(SELECT id FROM tally.payment_head WHERE tenant_id = $1 AND related_bill_id IN ` + delBills + `)`,
		`DELETE FROM tally.payment_head WHERE tenant_id = $1 AND related_bill_id IN ` + delBills,
		`DELETE FROM tally.invoice_bill WHERE tenant_id = $1 AND bill_id IN ` + delBills,
		`UPDATE tally.bill_head SET link_bill_id = NULL WHERE tenant_id = $1 AND link_bill_id IN ` + delBills +
			` AND id NOT IN ` + delBills,
		`UPDATE tally.bill_head SET amendment_of_id = NULL WHERE tenant_id = $1 AND amendment_of_id IN ` + delBills +
			` AND id NOT IN ` + delBills,
		`UPDATE tally.replenish_suggestion_log SET adopted_bill_id = NULL WHERE tenant_id = $1 AND adopted_bill_id IN ` + delBills,
		// bill_item rows cascade with their head.
		`DELETE FROM tally.bill_head WHERE tenant_id = $1 AND id IN ` + delBills,
		// A demo line sitting in a surviving draft or voided bill (mixed with the owner's
		// own goods) is dropped; the draft keeps its other lines and gets its amounts
		// recomputed. Only approved real bills keep a demo product alive.
		// line_amount and amount_local are in the bill currency, total_amount is CNY
		// (amount_local x exchange_rate), subtotal is the sum of line amounts.
		`CREATE TEMP TABLE demo_touched_heads ON COMMIT DROP AS SELECT DISTINCT bi.head_id FROM tally.bill_item bi ` +
			`JOIN tally.bill_head h ON h.id = bi.head_id WHERE h.tenant_id = $1 AND h.status IN (0, 9) ` +
			`AND bi.product_id IN ` + demoProd,
		`UPDATE tally.bill_item SET link_item_id = NULL WHERE tenant_id = $1 AND link_item_id IN ` +
			`(SELECT bi.id FROM tally.bill_item bi WHERE bi.head_id IN (SELECT head_id FROM demo_touched_heads) ` +
			`AND bi.product_id IN ` + demoProd + `)`,
		`WITH del AS (DELETE FROM tally.bill_item WHERE tenant_id = $1 AND head_id IN (SELECT head_id FROM demo_touched_heads) ` +
			`AND product_id IN ` + demoProd + ` ` +
			`RETURNING head_id, line_amount), s AS (SELECT head_id, SUM(COALESCE(line_amount, 0)) AS amt FROM del GROUP BY head_id) ` +
			`UPDATE tally.bill_head h SET ` +
			`subtotal = GREATEST(h.subtotal - s.amt, 0), ` +
			`amount_local = GREATEST(h.amount_local - s.amt, 0), ` +
			`total_amount = CASE WHEN h.amount_local IS NULL THEN GREATEST(h.total_amount - s.amt, 0) ` +
			`ELSE ROUND(GREATEST(h.amount_local - s.amt, 0) * COALESCE(h.exchange_rate, 1), 4) END, ` +
			`discount_amount = CASE WHEN h.discount_amount IS NULL THEN NULL ELSE LEAST(h.discount_amount, ` +
			`CASE WHEN h.amount_local IS NULL THEN GREATEST(h.total_amount - s.amt, 0) ` +
			`ELSE ROUND(GREATEST(h.amount_local - s.amt, 0) * COALESCE(h.exchange_rate, 1), 4) END) END ` +
			`FROM s WHERE h.id = s.head_id`,
		// Close the gaps the removed lines left in line_no (only bills touched above).
		`UPDATE tally.bill_item bi SET line_no = r.rn FROM (SELECT x.id, ROW_NUMBER() OVER (PARTITION BY x.head_id ORDER BY x.line_no, x.id) AS rn ` +
			`FROM tally.bill_item x WHERE x.tenant_id = $1 AND x.head_id IN (SELECT head_id FROM demo_touched_heads)) r ` +
			`WHERE bi.id = r.id AND bi.line_no <> r.rn`,
	}
	const demoWhere = `tenant_id = $1 AND remark = 'DEMO' AND id NOT IN ` + keepIDs
	const demoSelect = `SELECT id FROM tally.product WHERE ` + demoWhere
	childDeletes := append(billChildren,
		`DELETE FROM tally.stock_movement WHERE tenant_id = $1 AND product_id IN (`+demoSelect+`)`,
		`DELETE FROM tally.stock_lot      WHERE tenant_id = $1 AND product_id IN (`+demoSelect+`)`,
		`DELETE FROM tally.stock_snapshot WHERE tenant_id = $1 AND product_id IN (`+demoSelect+`)`,
		`DELETE FROM tally.stock_initial   WHERE tenant_id = $1 AND product_id IN (`+demoSelect+`)`,
		`DELETE FROM tally.stock_serial    WHERE tenant_id = $1 AND product_id IN (`+demoSelect+`)`,
		`DELETE FROM tally.replenish_suggestion_log WHERE tenant_id = $1 AND product_id IN (`+demoSelect+`)`,
		`DELETE FROM tally.product_sku    WHERE tenant_id = $1 AND product_id IN (`+demoSelect+`)`,
	)
	for _, q := range childDeletes {
		if _, err := tx.ExecContext(ctx, q, tenantID); err != nil {
			return nil, fmt.Errorf("onboarding repo: clear demo child rows: %w", err)
		}
	}

	const delProducts = `DELETE FROM tally.product WHERE ` + demoWhere
	del, err := tx.ExecContext(ctx, delProducts, tenantID)
	if err != nil {
		return nil, fmt.Errorf("onboarding repo: delete demo products: %w", err)
	}
	cleared, _ := del.RowsAffected()

	// Drop the DEMO- code prefix where the bare code is free among live products.
	if _, err := tx.ExecContext(ctx, `UPDATE tally.product p SET code = substr(p.code, $2) `+
		`WHERE p.tenant_id = $1 AND p.remark = 'DEMO' AND p.id IN `+keepIDs+` AND p.deleted_at IS NULL `+
		`AND p.code LIKE 'DEMO-%' AND length(p.code) >= $2 `+
		`AND NOT EXISTS (SELECT 1 FROM tally.product q WHERE q.tenant_id = p.tenant_id AND q.deleted_at IS NULL `+
		`AND q.id <> p.id AND q.code = substr(p.code, $2))`, tenantID, len(demoCodePrefix)+1); err != nil {
		return nil, fmt.Errorf("onboarding repo: strip demo code prefix: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE tally.product SET remark = $2 WHERE tenant_id = $1 AND remark = 'DEMO' AND id IN `+keepIDs, tenantID, ConvertedRemark); err != nil {
		return nil, fmt.Errorf("onboarding repo: convert demo products: %w", err)
	}

	res := &appob.ClearResult{ClearedProducts: int(cleared), Converted: []appob.ConvertedProduct{}}
	rows, err := tx.QueryContext(ctx, `SELECT k.id, p.code, k.name FROM demo_keep k JOIN tally.product p ON p.id = k.id WHERE NOT k.gone ORDER BY k.name, p.code`)
	if err != nil {
		return nil, fmt.Errorf("onboarding repo: list converted demo: %w", err)
	}
	for rows.Next() {
		var cp appob.ConvertedProduct
		if err := rows.Scan(&cp.ID, &cp.Code, &cp.Name); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("onboarding repo: scan converted demo: %w", err)
		}
		res.Converted = append(res.Converted, cp)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("onboarding repo: iterate converted demo: %w", err)
	}
	_ = rows.Close()
	if len(res.Converted) > 0 {
		res.Hint = appob.ConvertedHint
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("onboarding repo: commit clear-demo: %w", err)
	}
	return res, nil
}
