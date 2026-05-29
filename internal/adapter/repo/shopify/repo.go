// Package shopify implements the persistence layer for tally.shopify_shop_map.
// GetByDomain performs a cross-tenant lookup (the webhook path has no tenant
// context), so callers must ensure the DB connection bypasses RLS — either by
// using BYPASSRLS role or the SET LOCAL ROLE postgres / RESET ROLE pattern
// inside the same transaction.
package shopify

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// ErrShopAlreadyBound is returned by Create when the shop_domain UNIQUE
// constraint fires — the domain is already registered to another tenant.
var ErrShopAlreadyBound = errors.New("shop domain already bound to another account")

// ShopMapping is the resolved record for a Shopify store.
type ShopMapping struct {
	ID          uuid.UUID
	ShopDomain  string
	TenantID    uuid.UUID
	WarehouseID uuid.UUID
	CreatorID   uuid.UUID
}

// DB is the minimal database/sql interface required by this repo.
type DB interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// ShopMapRepo queries tally.shopify_shop_map.
type ShopMapRepo struct {
	db DB
}

// New constructs a ShopMapRepo.
func New(db DB) *ShopMapRepo {
	return &ShopMapRepo{db: db}
}

// GetByDomain returns the mapping for shopDomain, or (nil, nil) when no row exists.
// An error is returned only on unexpected database failures.
//
// Cross-tenant note: this query deliberately omits a tenant_id predicate because
// the webhook endpoint uses the shop_domain as the lookup key to discover which
// tenant owns the store.  Caller must ensure RLS is bypassed on the connection.
func (r *ShopMapRepo) GetByDomain(ctx context.Context, domain string) (*ShopMapping, error) {
	const q = `
		SELECT id, shop_domain, tenant_id, warehouse_id, creator_id
		FROM tally.shopify_shop_map
		WHERE shop_domain = $1`

	var m ShopMapping
	err := r.db.QueryRowContext(ctx, q, domain).Scan(
		&m.ID, &m.ShopDomain, &m.TenantID, &m.WarehouseID, &m.CreatorID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("shopify repo: get by domain %q: %w", domain, err)
	}
	return &m, nil
}

// Create inserts a new shop mapping. Returns ErrShopAlreadyBound when the
// shop_domain UNIQUE constraint fires (the domain belongs to another tenant).
func (r *ShopMapRepo) Create(ctx context.Context, m *ShopMapping) error {
	const q = `
		INSERT INTO tally.shopify_shop_map (id, tenant_id, shop_domain, warehouse_id, creator_id)
		VALUES ($1, $2, $3, $4, $5)`

	_, err := r.db.ExecContext(ctx, q, m.ID, m.TenantID, m.ShopDomain, m.WarehouseID, m.CreatorID)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrShopAlreadyBound
		}
		return fmt.Errorf("shopify repo: create mapping for %q: %w", m.ShopDomain, err)
	}
	return nil
}

// ListByTenant returns all mappings that belong to tenantID.
func (r *ShopMapRepo) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]ShopMapping, error) {
	const q = `
		SELECT id, shop_domain, tenant_id, warehouse_id, creator_id
		FROM tally.shopify_shop_map
		WHERE tenant_id = $1
		ORDER BY created_at ASC`

	rows, err := r.db.QueryContext(ctx, q, tenantID)
	if err != nil {
		return nil, fmt.Errorf("shopify repo: list by tenant: %w", err)
	}
	defer rows.Close()

	var out []ShopMapping
	for rows.Next() {
		var m ShopMapping
		if err := rows.Scan(&m.ID, &m.ShopDomain, &m.TenantID, &m.WarehouseID, &m.CreatorID); err != nil {
			return nil, fmt.Errorf("shopify repo: scan row: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("shopify repo: list by tenant rows: %w", err)
	}
	return out, nil
}

// DeleteByID removes the mapping identified by id, scoped to tenantID to
// prevent cross-tenant deletion. Returns (nil) if the row does not exist —
// DELETE is idempotent.
func (r *ShopMapRepo) DeleteByID(ctx context.Context, tenantID, id uuid.UUID) error {
	const q = `
		DELETE FROM tally.shopify_shop_map
		WHERE id = $1 AND tenant_id = $2`

	_, err := r.db.ExecContext(ctx, q, id, tenantID)
	if err != nil {
		return fmt.Errorf("shopify repo: delete mapping %s: %w", id, err)
	}
	return nil
}

// isUniqueViolation detects PostgreSQL error code 23505 (unique_violation).
// We use string inspection to avoid importing a PG-specific driver package here.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	// pgx and pq both expose pgerrcode 23505 in the error string.
	s := err.Error()
	return contains(s, "23505") || contains(s, "unique_violation") || contains(s, "duplicate key")
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		indexString(s, sub) >= 0)
}

func indexString(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
