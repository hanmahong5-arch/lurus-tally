// Package importing implements the repository for platform-order import tables:
// tally.import_sku_map and tally.import_order_seen.
// All queries rely on RLS being active (app.tenant_id session variable).
package importing

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	appimporting "github.com/hanmahong5-arch/lurus-tally/internal/app/importing"
)

const pgUniqueViolation = "23505"

// DB abstracts the minimal database/sql surface required by this repo.
type DB interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Repo implements appimporting.ImportRepo.
type Repo struct {
	db DB
}

// New creates a Repo backed by db.
func New(db DB) *Repo {
	return &Repo{db: db}
}

// Ensure Repo satisfies the interface at compile time.
var _ appimporting.ImportRepo = (*Repo)(nil)

// GetMapping looks up the product_id for a (tenant, platform, platform_sku) tuple.
// Returns nil, nil when no mapping exists.
func (r *Repo) GetMapping(ctx context.Context, tenantID uuid.UUID, platform, platformSKU string) (*appimporting.SKUMapping, error) {
	const q = `
		SELECT id, tenant_id, platform, platform_sku, product_id, created_at, updated_at
		FROM tally.import_sku_map
		WHERE tenant_id = $1 AND platform = $2 AND platform_sku = $3`

	row := r.db.QueryRowContext(ctx, q, tenantID, platform, platformSKU)
	m, err := scanMapping(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("import repo: get mapping: %w", err)
	}
	return m, nil
}

// UpsertMapping inserts or updates a SKU mapping.
// On conflict (tenant_id, platform, platform_sku) the product_id and updated_at are refreshed.
func (r *Repo) UpsertMapping(ctx context.Context, m *appimporting.SKUMapping) error {
	now := time.Now().UTC()
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	const q = `
		INSERT INTO tally.import_sku_map
			(id, tenant_id, platform, platform_sku, product_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (tenant_id, platform, platform_sku)
		DO UPDATE SET product_id = EXCLUDED.product_id, updated_at = EXCLUDED.updated_at`

	_, err := r.db.ExecContext(ctx, q,
		m.ID, m.TenantID, m.Platform, m.PlatformSKU, m.ProductID, now, now,
	)
	if err != nil {
		return fmt.Errorf("import repo: upsert mapping: %w", err)
	}
	return nil
}

// ListMappings returns all SKU mappings for a tenant, optionally filtered by platform.
func (r *Repo) ListMappings(ctx context.Context, tenantID uuid.UUID, platform string) ([]appimporting.SKUMapping, error) {
	var args []any
	args = append(args, tenantID)

	q := `SELECT id, tenant_id, platform, platform_sku, product_id, created_at, updated_at
		FROM tally.import_sku_map WHERE tenant_id = $1`
	if platform != "" {
		q += " AND platform = $2"
		args = append(args, platform)
	}
	q += " ORDER BY platform, platform_sku"

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("import repo: list mappings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result []appimporting.SKUMapping
	for rows.Next() {
		m, err := scanMappingRow(rows)
		if err != nil {
			return nil, fmt.Errorf("import repo: scan mapping: %w", err)
		}
		result = append(result, *m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("import repo: list mappings rows: %w", err)
	}
	return result, nil
}

// IsOrderSeen returns true when (tenant, platform, platform_order_no) has already
// been imported. Returns the existing bill_id when true.
//
// Two-stage detection prevents duplicate bills when MarkOrderSeen previously
// failed but bill creation had already committed:
//  1. Canonical dedup via tally.import_order_seen.
//  2. Fallback via tally.bill_head WHERE remark = 'import:{platform}:{orderNo}'.
//     A hit triggers a best-effort self-heal MarkOrderSeen so the next call
//     short-circuits on stage 1.
func (r *Repo) IsOrderSeen(ctx context.Context, tenantID uuid.UUID, platform, orderNo string) (bool, uuid.UUID, error) {
	const seenQuery = `
		SELECT bill_id FROM tally.import_order_seen
		WHERE tenant_id = $1 AND platform = $2 AND platform_order_no = $3`

	var billID uuid.UUID
	err := r.db.QueryRowContext(ctx, seenQuery, tenantID, platform, orderNo).Scan(&billID)
	if err == nil {
		return true, billID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, uuid.Nil, fmt.Errorf("import repo: is order seen: %w", err)
	}

	// Stage 2: orphan recovery. The seen-table row is missing — check bill_head
	// for an import-marked bill that already exists for this order.
	const remarkQuery = `
		SELECT id FROM tally.bill_head
		WHERE tenant_id = $1 AND remark = $2 AND deleted_at IS NULL
		ORDER BY created_at DESC LIMIT 1`
	remark := fmt.Sprintf("import:%s:%s", platform, orderNo)
	err = r.db.QueryRowContext(ctx, remarkQuery, tenantID, remark).Scan(&billID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, uuid.Nil, nil
	}
	if err != nil {
		return false, uuid.Nil, fmt.Errorf("import repo: orphan bill lookup: %w", err)
	}

	// Self-heal: insert the missing import_order_seen row so future lookups
	// short-circuit on stage 1. Failure is non-fatal because the caller already
	// has the correct seen=true answer.
	_ = r.MarkOrderSeen(ctx, tenantID, platform, orderNo, billID)
	return true, billID, nil
}

// MarkOrderSeen records that a platform order has been imported as bill billID.
// Duplicate inserts are silently ignored (idempotent).
func (r *Repo) MarkOrderSeen(ctx context.Context, tenantID uuid.UUID, platform, orderNo string, billID uuid.UUID) error {
	const q = `
		INSERT INTO tally.import_order_seen (id, tenant_id, platform, platform_order_no, bill_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (tenant_id, platform, platform_order_no) DO NOTHING`

	_, err := r.db.ExecContext(ctx, q,
		uuid.New(), tenantID, platform, orderNo, billID, time.Now().UTC(),
	)
	if err != nil && !isPgUniqueViolation(err) {
		return fmt.Errorf("import repo: mark order seen: %w", err)
	}
	return nil
}

// ----- helpers -----

type rowScanner interface {
	Scan(dest ...any) error
}

func scanMapping(s rowScanner) (*appimporting.SKUMapping, error) {
	var m appimporting.SKUMapping
	err := s.Scan(&m.ID, &m.TenantID, &m.Platform, &m.PlatformSKU, &m.ProductID, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func scanMappingRow(rows *sql.Rows) (*appimporting.SKUMapping, error) {
	var m appimporting.SKUMapping
	err := rows.Scan(&m.ID, &m.TenantID, &m.Platform, &m.PlatformSKU, &m.ProductID, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func isPgUniqueViolation(err error) bool {
	type pgErr interface {
		SQLState() string
	}
	var pe pgErr
	if errors.As(err, &pe) {
		return pe.SQLState() == pgUniqueViolation
	}
	return strings.Contains(err.Error(), pgUniqueViolation) ||
		strings.Contains(err.Error(), "unique constraint") ||
		strings.Contains(err.Error(), "duplicate key")
}
