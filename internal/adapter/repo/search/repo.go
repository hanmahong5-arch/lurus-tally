// Package search implements EntityRepo backed by PostgreSQL for the ⌘K entity search.
package search

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"

	appsearch "github.com/hanmahong5-arch/lurus-tally/internal/app/search"
)

// Repo implements appsearch.EntityRepo using PostgreSQL.
type Repo struct {
	db *sql.DB
}

// New creates a Repo.
func New(db *sql.DB) *Repo {
	return &Repo{db: db}
}

var _ appsearch.EntityRepo = (*Repo)(nil)

// SearchProducts queries tally.product by name / code / mnemonic (ILIKE).
func (r *Repo) SearchProducts(ctx context.Context, tenantID uuid.UUID, q string, limit int) ([]appsearch.EntityResult, error) {
	const stmt = `
		SELECT id::text, name, COALESCE(code, '')
		FROM tally.product
		WHERE tenant_id = $1
		  AND deleted_at IS NULL
		  AND (name ILIKE $2 OR code ILIKE $2 OR mnemonic ILIKE $2)
		ORDER BY name
		LIMIT $3`

	return r.scan(ctx, stmt, appsearch.EntityProduct, tenantID, "%"+q+"%", limit)
}

// SearchSuppliers queries tally.supplier by name / code (ILIKE).
func (r *Repo) SearchSuppliers(ctx context.Context, tenantID uuid.UUID, q string, limit int) ([]appsearch.EntityResult, error) {
	const stmt = `
		SELECT id::text, name, COALESCE(code, '')
		FROM tally.supplier
		WHERE tenant_id = $1
		  AND deleted_at IS NULL
		  AND (name ILIKE $2 OR code ILIKE $2)
		ORDER BY name
		LIMIT $3`

	return r.scan(ctx, stmt, appsearch.EntitySupplier, tenantID, "%"+q+"%", limit)
}

// SearchCustomers queries tally.partner where partner_type is customer or both (ILIKE on name / code).
func (r *Repo) SearchCustomers(ctx context.Context, tenantID uuid.UUID, q string, limit int) ([]appsearch.EntityResult, error) {
	const stmt = `
		SELECT id::text, name, COALESCE(code, '')
		FROM tally.partner
		WHERE tenant_id = $1
		  AND deleted_at IS NULL
		  AND partner_type IN ('customer', 'both', 'member')
		  AND (name ILIKE $2 OR code ILIKE $2)
		ORDER BY name
		LIMIT $3`

	return r.scan(ctx, stmt, appsearch.EntityCustomer, tenantID, "%"+q+"%", limit)
}

// SearchBills queries tally.bill_head by bill_no (ILIKE). Sublabel carries bill_type + sub_type.
func (r *Repo) SearchBills(ctx context.Context, tenantID uuid.UUID, q string, limit int) ([]appsearch.EntityResult, error) {
	const stmt = `
		SELECT id::text, bill_no, bill_type || ' · ' || sub_type
		FROM tally.bill_head
		WHERE tenant_id = $1
		  AND deleted_at IS NULL
		  AND bill_no ILIKE $2
		ORDER BY created_at DESC
		LIMIT $3`

	return r.scan(ctx, stmt, appsearch.EntityBill, tenantID, "%"+q+"%", limit)
}

// scan is a shared scanner for the standard (id, label, sublabel) projection.
func (r *Repo) scan(
	ctx context.Context,
	stmt string,
	entityType appsearch.EntityType,
	tenantID uuid.UUID,
	pattern string,
	limit int,
) ([]appsearch.EntityResult, error) {
	rows, err := r.db.QueryContext(ctx, stmt, tenantID, pattern, limit)
	if err != nil {
		return nil, fmt.Errorf("search %s: %w", entityType, err)
	}
	defer func() { _ = rows.Close() }()

	var out []appsearch.EntityResult
	for rows.Next() {
		var res appsearch.EntityResult
		res.Type = entityType
		if err := rows.Scan(&res.ID, &res.Label, &res.Sublabel); err != nil {
			return nil, fmt.Errorf("search %s scan: %w", entityType, err)
		}
		out = append(out, res)
	}
	return out, rows.Err()
}
