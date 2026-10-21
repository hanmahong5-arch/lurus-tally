package product

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dbscope"
	appproduct "github.com/hanmahong5-arch/lurus-tally/internal/app/product"
)

// The bulk import checks identifiers for a whole file at once. Values travel as one
// JSON array parameter (unnest via jsonb_array_elements_text), which sidesteps array
// literal escaping for arbitrary user text.

var _ appproduct.ImportCatalog = (*Repo)(nil)

func jsonArray(values []string) (string, error) {
	if values == nil {
		values = []string{}
	}
	b, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (r *Repo) existingSet(ctx context.Context, q string, tenantID uuid.UUID, values []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(values) == 0 {
		return out, nil
	}
	arr, err := jsonArray(values)
	if err != nil {
		return nil, fmt.Errorf("product repo existing: %w", err)
	}
	rows, err := dbscope.From(ctx, r.db).QueryContext(ctx, q, tenantID, arr)
	if err != nil {
		return nil, fmt.Errorf("product repo existing: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var a, b string
		if err := rows.Scan(&a, &b); err != nil {
			return nil, fmt.Errorf("product repo existing scan: %w", err)
		}
		if b == "" {
			out[a] = true
		} else {
			out[appproduct.NameSpecKey(a, b)] = true
		}
	}
	return out, rows.Err()
}

// ExistingCodes returns which of codes are already used by an active product of the tenant.
func (r *Repo) ExistingCodes(ctx context.Context, tenantID uuid.UUID, codes []string) (map[string]bool, error) {
	const q = `
		SELECT code, '' FROM tally.product
		WHERE tenant_id = $1 AND deleted_at IS NULL
		  AND code IN (SELECT jsonb_array_elements_text($2::jsonb))`
	return r.existingSet(ctx, q, tenantID, codes)
}

// ExistingBarcodes returns which barcodes (attributes.barcode) are already used by an active product.
func (r *Repo) ExistingBarcodes(ctx context.Context, tenantID uuid.UUID, barcodes []string) (map[string]bool, error) {
	const q = `
		SELECT attributes->>'barcode', '' FROM tally.product
		WHERE tenant_id = $1 AND deleted_at IS NULL
		  AND attributes->>'barcode' IN (SELECT jsonb_array_elements_text($2::jsonb))`
	return r.existingSet(ctx, q, tenantID, barcodes)
}

// ExistingNameSpecs returns NameSpecKey(name, spec) for active products whose name is in names.
// Products without a spec are keyed with the empty spec.
func (r *Repo) ExistingNameSpecs(ctx context.Context, tenantID uuid.UUID, names []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(names) == 0 {
		return out, nil
	}
	arr, err := jsonArray(names)
	if err != nil {
		return nil, fmt.Errorf("product repo existing names: %w", err)
	}
	const q = `
		SELECT name, COALESCE(spec, '') FROM tally.product
		WHERE tenant_id = $1 AND deleted_at IS NULL
		  AND name IN (SELECT jsonb_array_elements_text($2::jsonb))`
	rows, err := dbscope.From(ctx, r.db).QueryContext(ctx, q, tenantID, arr)
	if err != nil {
		return nil, fmt.Errorf("product repo existing names: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var name, spec string
		if err := rows.Scan(&name, &spec); err != nil {
			return nil, fmt.Errorf("product repo existing names scan: %w", err)
		}
		out[appproduct.NameSpecKey(name, spec)] = true
	}
	return out, rows.Err()
}

var _ appproduct.ImportLocker = (*Repo)(nil)

// LockImport takes a session-level advisory lock keyed by the tenant on a dedicated
// connection, so concurrent real imports of one tenant run one after another.
func (r *Repo) LockImport(ctx context.Context, tenantID uuid.UUID) (func(), error) {
	conn, err := r.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("product repo import lock: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock(hashtextextended($1, 0))`, "product-import:"+tenantID.String()); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("product repo import lock: %w", err)
	}
	return func() {
		// Fresh context: the request may already be cancelled, the lock must still be released.
		_, _ = conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock(hashtextextended($1, 0))`, "product-import:"+tenantID.String())
		_ = conn.Close()
	}, nil
}
