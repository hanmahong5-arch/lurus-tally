// Package sku implements the SKU price repository using PostgreSQL.
// All queries operate within the tally schema and rely on RLS being active
// (app.tenant_id set on the connection before calling these methods).
package sku

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dbscope"
	appsku "github.com/hanmahong5-arch/lurus-tally/internal/app/sku"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/decimalutil"
)

// Repo implements appsku.PriceRepo.
type Repo struct {
	db *sql.DB
}

// New creates a Repo backed by db.
func New(db *sql.DB) *Repo {
	return &Repo{db: db}
}

var (
	_ appsku.PriceRepo        = (*Repo)(nil)
	_ appsku.DefaultSKUWriter = (*Repo)(nil)
)

// InTx runs fn in one transaction; repositories called with the passed context
// (this one included) join it. Satisfies the product use cases' TxRunner.
func (r *Repo) InTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return dbscope.InTx(ctx, r.db, fn)
}

// UpsertDefaultSKU writes the provided prices onto the product's default SKU
// (the same row ListDefaultSKUs picks), creating an is_default SKU when none exists.
// Only non-nil prices are written; stock is not touched.
func (r *Repo) UpsertDefaultSKU(ctx context.Context, tenantID, productID uuid.UUID, p appsku.DefaultPrices) error {
	if p.IsEmpty() {
		return nil
	}
	arg := func(d *decimal.Decimal) any {
		if d == nil {
			return nil
		}
		return d.String()
	}
	dbh := dbscope.From(ctx, r.db)
	now := time.Now().UTC()

	const upd = `
		UPDATE tally.product_sku
		SET retail_price    = COALESCE($1::numeric, retail_price),
		    wholesale_price = COALESCE($2::numeric, wholesale_price),
		    purchase_price  = COALESCE($3::numeric, purchase_price),
		    updated_at      = $4
		WHERE id = (
			SELECT id FROM tally.product_sku
			WHERE tenant_id = $5 AND product_id = $6 AND deleted_at IS NULL
			ORDER BY is_default DESC, created_at ASC
			LIMIT 1)
		  AND tenant_id = $5`
	res, err := dbh.ExecContext(ctx, upd, arg(p.Retail), arg(p.Wholesale), arg(p.Purchase), now, tenantID, productID)
	if err != nil {
		return fmt.Errorf("sku repo upsert default: update: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sku repo upsert default: rows affected: %w", err)
	}
	if n > 0 {
		return nil
	}

	const ins = `
		INSERT INTO tally.product_sku
			(tenant_id, product_id, retail_price, wholesale_price, purchase_price, is_default, created_at, updated_at)
		VALUES ($1, $2, COALESCE($3::numeric, 0), COALESCE($4::numeric, 0), COALESCE($5::numeric, 0), true, $6, $6)`
	if _, err := dbh.ExecContext(ctx, ins, tenantID, productID, arg(p.Retail), arg(p.Wholesale), arg(p.Purchase), now); err != nil {
		return fmt.Errorf("sku repo upsert default: insert: %w", err)
	}
	return nil
}

// ListDefaultSKUs returns the default SKU (or earliest-created fallback) per product.
// Products with no SKU row are omitted from the result.
func (r *Repo) ListDefaultSKUs(ctx context.Context, tenantID uuid.UUID, productIDs []uuid.UUID) ([]appsku.DefaultSKU, error) {
	if len(productIDs) == 0 {
		return nil, nil
	}
	const q = `
		SELECT DISTINCT ON (product_id) id, product_id, COALESCE(retail_price, 0), COALESCE(purchase_price, 0), COALESCE(wholesale_price, 0), COALESCE(unit_name, '')
		FROM tally.product_sku
		WHERE tenant_id = $1
		  AND product_id = ANY($2::uuid[])
		  AND deleted_at IS NULL
		ORDER BY product_id, is_default DESC, created_at ASC`

	dbh := dbscope.From(ctx, r.db)
	rows, err := dbh.QueryContext(ctx, q, tenantID, uuidArrayLiteral(productIDs))
	if err != nil {
		return nil, fmt.Errorf("sku repo list default: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []appsku.DefaultSKU
	for rows.Next() {
		var s appsku.DefaultSKU
		var retailStr, purchaseStr, wholesaleStr string
		if err := rows.Scan(&s.SKUID, &s.ProductID, &retailStr, &purchaseStr, &wholesaleStr, &s.UnitName); err != nil {
			return nil, fmt.Errorf("sku repo scan: %w", err)
		}
		// TODO: these callers originally ignored parse errors; behaviour preserved.
		s.RetailPrice, _ = decimalutil.Parse(retailStr, "retail_price")
		s.PurchasePrice, _ = decimalutil.Parse(purchaseStr, "purchase_price")
		s.WholesalePrice, _ = decimalutil.Parse(wholesaleStr, "wholesale_price")
		out = append(out, s)
	}
	return out, rows.Err()
}

// UpdateRetailPrice sets retail_price for one SKU within the tenant scope.
func (r *Repo) UpdateRetailPrice(ctx context.Context, tenantID, skuID uuid.UUID, newPrice decimal.Decimal) error {
	const q = `
		UPDATE tally.product_sku
		SET retail_price = $1, updated_at = $2
		WHERE id = $3 AND tenant_id = $4 AND deleted_at IS NULL`

	dbh := dbscope.From(ctx, r.db)
	res, err := dbh.ExecContext(ctx, q, newPrice.String(), time.Now().UTC(), skuID, tenantID)
	if err != nil {
		return fmt.Errorf("sku repo update retail price: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sku repo update rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("sku repo update: sku %s not found for tenant", skuID)
	}
	return nil
}

// uuidArrayLiteral renders a UUID slice as a PostgreSQL array literal: {id1,id2,...}.
// UUIDs contain only hex + dashes so no escaping is required.
func uuidArrayLiteral(ids []uuid.UUID) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = id.String()
	}
	return "{" + strings.Join(parts, ",") + "}"
}
