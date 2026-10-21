package bill

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dbscope"
	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/decimalutil"
)

// Ensure Repo satisfies the last-price read port at compile time.
var (
	_ appbill.LastPriceRepo      = (*Repo)(nil)
	_ appbill.BaseUnitNameReader = (*Repo)(nil)
)

// lastPricesQuery picks, per product, the newest approved line of one partner's bills.
// $1 tenant, $2 partner, $3 sub type (销售 / 采购), $4 product ids.
func lastPricesQuery() string {
	return `
	SELECT DISTINCT ON (i.product_id) i.product_id, i.unit_price::text, i.unit_id, COALESCE(i.unit_name, ''),
	       COALESCE(pu.conversion_factor, 1)::text, h.bill_date
	FROM tally.bill_item i
	JOIN tally.bill_head h ON h.id = i.head_id AND h.tenant_id = i.tenant_id
	LEFT JOIN tally.product_unit pu ON pu.product_id = i.product_id AND pu.unit_id = i.unit_id AND pu.tenant_id = i.tenant_id
	WHERE i.tenant_id = $1 AND h.partner_id = $2 AND h.sub_type = $3
	  AND h.status = 2 AND COALESCE(h.currency, 'CNY') = 'CNY' AND h.deleted_at IS NULL AND i.deleted_at IS NULL
	  AND i.unit_price IS NOT NULL AND i.product_id = ANY($4::uuid[])
	ORDER BY i.product_id, h.bill_date DESC, h.created_at DESC, i.created_at DESC`
}

// LastPrices implements appbill.LastPriceRepo.
func (r *Repo) LastPrices(ctx context.Context, q appbill.LastPriceQuery) ([]appbill.LastPrice, error) {
	ids := make([]string, len(q.ProductIDs))
	for i, id := range q.ProductIDs {
		ids[i] = id.String()
	}
	rows, err := dbscope.From(ctx, r.db).QueryContext(ctx, lastPricesQuery(),
		q.TenantID, q.PartnerID, q.SubType, pq.Array(ids))
	if err != nil {
		return nil, fmt.Errorf("bill repo: last prices: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := []appbill.LastPrice{}
	for rows.Next() {
		var lp appbill.LastPrice
		var pid uuid.UUID
		var price, factor string
		var unitID uuid.NullUUID
		var date time.Time
		if err := rows.Scan(&pid, &price, &unitID, &lp.UnitName, &factor, &date); err != nil {
			return nil, fmt.Errorf("bill repo: last prices scan: %w", err)
		}
		if lp.UnitPrice, err = decimalutil.Parse(price, "unit_price"); err != nil {
			return nil, fmt.Errorf("bill repo: last prices scan: %w", err)
		}
		if lp.Factor, err = decimalutil.Parse(factor, "conversion_factor"); err != nil {
			return nil, fmt.Errorf("bill repo: last prices scan: %w", err)
		}
		lp.UnitID = unitID.UUID
		lp.ProductID, lp.BillDate = pid, date
		out = append(out, lp)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("bill repo: last prices rows: %w", err)
	}
	return out, nil
}

// BaseUnitNames implements appbill.BaseUnitNameReader: product id -> name of its base unit.
func (r *Repo) BaseUnitNames(ctx context.Context, tenantID uuid.UUID, productIDs []uuid.UUID) (map[uuid.UUID]string, error) {
	ids := make([]string, len(productIDs))
	for i, id := range productIDs {
		ids[i] = id.String()
	}
	rows, err := dbscope.From(ctx, r.db).QueryContext(ctx, baseUnitNamesQuery(), tenantID, pq.Array(ids))
	if err != nil {
		return nil, fmt.Errorf("bill repo: base unit names: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make(map[uuid.UUID]string, len(productIDs))
	for rows.Next() {
		var pid uuid.UUID
		var name string
		if err := rows.Scan(&pid, &name); err != nil {
			return nil, fmt.Errorf("bill repo: base unit names scan: %w", err)
		}
		out[pid] = name
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("bill repo: base unit names rows: %w", err)
	}
	return out, nil
}

// baseUnitNamesQuery maps products to the name of their default (base) unit.
// $1 tenant, $2 product ids.
func baseUnitNamesQuery() string {
	return `
	SELECT p.id, ud.name
	FROM tally.product p
	JOIN tally.unit_def ud ON ud.id = p.default_unit_id
	WHERE p.tenant_id = $1 AND p.id = ANY($2::uuid[]) AND p.deleted_at IS NULL`
}
