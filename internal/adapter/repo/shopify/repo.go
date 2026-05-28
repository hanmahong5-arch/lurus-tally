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

// ShopMapping is the resolved record for a Shopify store.
type ShopMapping struct {
	ShopDomain  string
	TenantID    uuid.UUID
	WarehouseID uuid.UUID
	CreatorID   uuid.UUID
}

// DB is the minimal database/sql interface required by this repo.
type DB interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
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
		SELECT shop_domain, tenant_id, warehouse_id, creator_id
		FROM tally.shopify_shop_map
		WHERE shop_domain = $1`

	var m ShopMapping
	err := r.db.QueryRowContext(ctx, q, domain).Scan(
		&m.ShopDomain, &m.TenantID, &m.WarehouseID, &m.CreatorID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("shopify repo: get by domain %q: %w", domain, err)
	}
	return &m, nil
}
