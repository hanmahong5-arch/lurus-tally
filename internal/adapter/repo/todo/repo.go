// Package todo implements the two dashboard figures the todo summary needs that
// no bill/payment use case exposes: money collected on sales in a window, and
// the not-yet-invoiced amount of approved sales in a window.
package todo

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dbscope"
	appob "github.com/hanmahong5-arch/lurus-tally/internal/app/onboarding"
	apptodo "github.com/hanmahong5-arch/lurus-tally/internal/app/todo"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/decimalutil"
)

// Repo is the PostgreSQL-backed KPIReader.
type Repo struct {
	db *sql.DB
}

// New constructs a Repo.
func New(db *sql.DB) *Repo { return &Repo{db: db} }

var (
	_ apptodo.KPIReader           = (*Repo)(nil)
	_ apptodo.NegativeStockReader = (*Repo)(nil)
)

// NegativeStock lists live products whose on-hand quantity is below zero in a
// live warehouse, most negative first, at most limit rows. The second return is
// the real total (window count), so the badge is not capped by the preview.
func (r *Repo) NegativeStock(ctx context.Context, tenantID uuid.UUID, limit int) ([]apptodo.NegativeStockItem, int, error) {
	const q = `
		SELECT s.product_id, p.name, w.name, s.on_hand_qty::text, COUNT(*) OVER ()
		FROM tally.stock_snapshot s
		JOIN tally.product p ON p.id = s.product_id AND p.tenant_id = s.tenant_id
		JOIN tally.warehouse w ON w.id = s.warehouse_id AND w.tenant_id = s.tenant_id
		WHERE s.tenant_id = $1 AND s.on_hand_qty < 0
		  AND p.deleted_at IS NULL AND w.deleted_at IS NULL
		ORDER BY s.on_hand_qty ASC, p.name
		LIMIT $2`
	rows, err := dbscope.From(ctx, r.db).QueryContext(ctx, q, tenantID, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("todo repo: negative stock: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := []apptodo.NegativeStockItem{}
	total := 0
	for rows.Next() {
		var it apptodo.NegativeStockItem
		var onHand string
		if err := rows.Scan(&it.ProductID, &it.ProductName, &it.WarehouseName, &onHand, &total); err != nil {
			return nil, 0, fmt.Errorf("todo repo: negative stock scan: %w", err)
		}
		d, err := decimalutil.Parse(onHand, "on_hand_qty")
		if err != nil {
			return nil, 0, fmt.Errorf("todo repo: negative stock: %w", err)
		}
		it.OnHand = d.String()
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("todo repo: negative stock rows: %w", err)
	}
	return items, total, nil
}

// Collected sums non-credit payments recorded against live sale bills with
// pay_date in [from, to). "credit" is a promise to pay later, not money received.
func (r *Repo) Collected(ctx context.Context, tenantID uuid.UUID, from, to time.Time) (decimal.Decimal, error) {
	const q = `
		SELECT COALESCE(SUM(ph.amount), 0)::text
		FROM tally.payment_head ph
		JOIN tally.bill_head b ON b.id = ph.related_bill_id AND b.tenant_id = ph.tenant_id
		WHERE ph.tenant_id = $1 AND ph.deleted_at IS NULL
		  AND ph.pay_type <> 'credit'
		  AND b.sub_type = '销售' AND b.status <> 9 AND b.deleted_at IS NULL
		  AND ph.pay_date >= $2 AND ph.pay_date < $3`
	return r.sum(ctx, "collected", q, tenantID, from, to)
}

// Uninvoiced sums total_amount minus the amount covered by live invoices over
// approved sales dated in [from, to); fully invoiced bills contribute nothing.
func (r *Repo) Uninvoiced(ctx context.Context, tenantID uuid.UUID, from, to time.Time) (decimal.Decimal, error) {
	const q = `
		SELECT COALESCE(SUM(GREATEST(b.total_amount - COALESCE((
			SELECT SUM(ib.amount) FROM tally.invoice_bill ib
			JOIN tally.invoice iv ON iv.id = ib.invoice_id
			WHERE ib.bill_id = b.id AND iv.status = 'issued' AND iv.deleted_at IS NULL), 0), 0)), 0)::text
		FROM tally.bill_head b
		WHERE b.tenant_id = $1 AND b.deleted_at IS NULL AND b.sub_type = '销售' AND b.status = 2
		  AND b.bill_date >= $2 AND b.bill_date < $3`
	return r.sum(ctx, "uninvoiced", q, tenantID, from, to)
}

// HasData reports whether the tenant owns at least one live product or bill.
func (r *Repo) HasData(ctx context.Context, tenantID uuid.UUID) (bool, error) {
	const q = `
		SELECT EXISTS (SELECT 1 FROM tally.product WHERE tenant_id = $1 AND deleted_at IS NULL)
		    OR EXISTS (SELECT 1 FROM tally.bill_head WHERE tenant_id = $1 AND deleted_at IS NULL)`
	var has bool
	if err := dbscope.From(ctx, r.db).QueryRowContext(ctx, q, tenantID).Scan(&has); err != nil {
		return false, fmt.Errorf("todo repo: has data: %w", err)
	}
	return has, nil
}

// Onboarding reports the getting-started steps from the tenant's own records.
// Demo rows (remark 'DEMO') are ignored for the steps. A purchase / sale counts
// once approved (status 2): a saved draft has not moved stock or money yet. A
// shop that loaded opening stock from an import (reference_type init) has also
// "put its stock in", so that counts as the purchase step (a kept former sample
// product, marked 示例转正 or still coded DEMO-*, keeps its sample opening stock
// and counts for neither the product nor the purchase step).
// A payment counts when it is real money (not a credit promise) on a live,
// non-demo bill.
func (r *Repo) Onboarding(ctx context.Context, tenantID uuid.UUID) (apptodo.Onboarding, error) {
	const q = `
		SELECT
		  EXISTS (SELECT 1 FROM tally.product
		          WHERE tenant_id = $1 AND deleted_at IS NULL AND remark IS DISTINCT FROM 'DEMO'
		            AND remark IS DISTINCT FROM $2 AND code NOT LIKE 'DEMO-%'),
		  EXISTS (SELECT 1 FROM tally.bill_head
		          WHERE tenant_id = $1 AND deleted_at IS NULL AND sub_type = '采购' AND status = 2
		            AND remark IS DISTINCT FROM 'DEMO')
		  OR EXISTS (SELECT 1 FROM tally.stock_movement m
		          JOIN tally.product p ON p.id = m.product_id AND p.tenant_id = m.tenant_id
		          WHERE m.tenant_id = $1 AND m.reference_type = 'init' AND p.deleted_at IS NULL
		            AND p.remark IS DISTINCT FROM 'DEMO' AND p.remark IS DISTINCT FROM $2
		            AND p.code NOT LIKE 'DEMO-%'),
		  EXISTS (SELECT 1 FROM tally.bill_head
		          WHERE tenant_id = $1 AND deleted_at IS NULL AND sub_type = '销售' AND status = 2
		            AND remark IS DISTINCT FROM 'DEMO'),
		  EXISTS (SELECT 1 FROM tally.payment_head ph
		          JOIN tally.bill_head b ON b.id = ph.related_bill_id AND b.tenant_id = ph.tenant_id
		          WHERE ph.tenant_id = $1 AND ph.deleted_at IS NULL AND ph.pay_type <> 'credit'
		            AND b.deleted_at IS NULL AND b.status <> 9 AND b.remark IS DISTINCT FROM 'DEMO'),
		  EXISTS (SELECT 1 FROM tally.product
		          WHERE tenant_id = $1 AND deleted_at IS NULL AND remark = 'DEMO')`
	var o apptodo.Onboarding
	if err := dbscope.From(ctx, r.db).QueryRowContext(ctx, q, tenantID, appob.ConvertedRemark).
		Scan(&o.HasProduct, &o.HasPurchase, &o.HasSale, &o.HasPayment, &o.HasDemo); err != nil {
		return apptodo.Onboarding{}, fmt.Errorf("todo repo: onboarding: %w", err)
	}
	return o, nil
}

func (r *Repo) sum(ctx context.Context, what, q string, tenantID uuid.UUID, from, to time.Time) (decimal.Decimal, error) {
	var s string
	if err := dbscope.From(ctx, r.db).QueryRowContext(ctx, q, tenantID, from, to).Scan(&s); err != nil {
		return decimal.Zero, fmt.Errorf("todo repo: %s: %w", what, err)
	}
	return decimalutil.Parse(s, what)
}
