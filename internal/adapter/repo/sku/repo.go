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

	appsku "github.com/hanmahong5-arch/lurus-tally/internal/app/sku"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/decimalutil"
)

// DB abstracts the minimal database/sql surface needed by this repo.
// Both *sql.DB and *sql.Tx satisfy this interface.
type DB interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Repo implements appsku.PriceRepo.
type Repo struct {
	db DB
}

// New creates a Repo backed by db.
func New(db DB) *Repo {
	return &Repo{db: db}
}

var _ appsku.PriceRepo = (*Repo)(nil)

// ListDefaultSKUs returns the default SKU (or earliest-created fallback) per product.
// Products with no SKU row are omitted from the result.
func (r *Repo) ListDefaultSKUs(ctx context.Context, tenantID uuid.UUID, productIDs []uuid.UUID) ([]appsku.DefaultSKU, error) {
	if len(productIDs) == 0 {
		return nil, nil
	}
	const q = `
		SELECT DISTINCT ON (product_id) id, product_id, COALESCE(retail_price, 0), COALESCE(purchase_price, 0)
		FROM tally.product_sku
		WHERE tenant_id = $1
		  AND product_id = ANY($2::uuid[])
		  AND deleted_at IS NULL
		ORDER BY product_id, is_default DESC, created_at ASC`

	rows, err := r.db.QueryContext(ctx, q, tenantID, uuidArrayLiteral(productIDs))
	if err != nil {
		return nil, fmt.Errorf("sku repo list default: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []appsku.DefaultSKU
	for rows.Next() {
		var s appsku.DefaultSKU
		var retailStr, purchaseStr string
		if err := rows.Scan(&s.SKUID, &s.ProductID, &retailStr, &purchaseStr); err != nil {
			return nil, fmt.Errorf("sku repo scan: %w", err)
		}
		// TODO: these callers originally ignored parse errors; behaviour preserved.
		s.RetailPrice, _ = decimalutil.Parse(retailStr, "retail_price")
		s.PurchasePrice, _ = decimalutil.Parse(purchaseStr, "purchase_price")
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

	res, err := r.db.ExecContext(ctx, q, newPrice.String(), time.Now().UTC(), skuID, tenantID)
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
