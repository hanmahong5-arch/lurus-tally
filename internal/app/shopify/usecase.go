// Package shopify contains application-layer use cases for Shopify shop
// bindings.  A tenant administrator can bind one or more Shopify stores to
// their account; each store can only belong to one tenant (DB UNIQUE on
// shop_domain).
package shopify

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/google/uuid"
)

// --- sentinel errors -------------------------------------------------------

// ErrInvalidDomain is returned when the supplied shop domain does not match
// the required *.myshopify.com pattern.
var ErrInvalidDomain = errors.New("shop domain must end with .myshopify.com")

// ErrShopAlreadyBound is surfaced when the DB UNIQUE constraint fires.
var ErrShopAlreadyBound = errors.New("该店铺已被其他账户绑定，请联系 Tally 客服")

// ErrWarehouseNotOwned is returned when the supplied warehouse_id does not
// belong to the requesting tenant.
var ErrWarehouseNotOwned = errors.New("warehouse not found or does not belong to this tenant")

// --- regexp ----------------------------------------------------------------

// shopifyDomainRE matches lowercase *.myshopify.com hostnames.
// The store slug must be 1-63 non-dot characters (Shopify's actual limit).
var shopifyDomainRE = regexp.MustCompile(`^[a-z0-9][a-z0-9\-]{0,61}[a-z0-9]?\.myshopify\.com$`)

// --- interfaces ------------------------------------------------------------

// ShopRepo abstracts persistence for shop mappings.
type ShopRepo interface {
	// Create inserts a new mapping. Returns ErrShopAlreadyBound on UNIQUE
	// violation (shop_domain already registered to another tenant).
	Create(ctx context.Context, m *ShopMapping) error
	// ListByTenant returns all mappings that belong to tenantID.
	ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]ShopMapping, error)
	// DeleteByID removes a mapping, tenant-scoped (no-op if not found).
	DeleteByID(ctx context.Context, tenantID, id uuid.UUID) error
}

// WarehouseChecker verifies that a warehouse ID belongs to a specific tenant.
// Implementors return (true, nil) when owned, (false, nil) when not found or
// not owned, and (false, err) on unexpected failures.
type WarehouseChecker interface {
	BelongsToTenant(ctx context.Context, tenantID, warehouseID uuid.UUID) (bool, error)
}

// --- domain model ----------------------------------------------------------

// ShopMapping is the application-layer representation of a bound Shopify store.
type ShopMapping struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	ShopDomain  string
	WarehouseID uuid.UUID
	CreatorID   uuid.UUID
}

// --- use cases -------------------------------------------------------------

// BindShopUseCase binds a Shopify store to the caller's tenant.
type BindShopUseCase struct {
	repo    ShopRepo
	checker WarehouseChecker
}

// NewBindShopUseCase constructs a BindShopUseCase.
func NewBindShopUseCase(repo ShopRepo, checker WarehouseChecker) *BindShopUseCase {
	return &BindShopUseCase{repo: repo, checker: checker}
}

// BindInput carries the validated input for a bind operation.
type BindInput struct {
	TenantID    uuid.UUID
	ShopDomain  string
	WarehouseID uuid.UUID
	CreatorID   uuid.UUID
}

// Execute validates and persists the shop binding.
//
// Validation order:
//  1. shop_domain format (*.myshopify.com)
//  2. warehouse belongs to the tenant
//  3. INSERT — UNIQUE violation → ErrShopAlreadyBound
func (uc *BindShopUseCase) Execute(ctx context.Context, in BindInput) (*ShopMapping, error) {
	if err := validateDomain(in.ShopDomain); err != nil {
		return nil, err
	}

	owned, err := uc.checker.BelongsToTenant(ctx, in.TenantID, in.WarehouseID)
	if err != nil {
		return nil, fmt.Errorf("shopify bind: warehouse check: %w", err)
	}
	if !owned {
		return nil, ErrWarehouseNotOwned
	}

	m := &ShopMapping{
		ID:          uuid.New(),
		TenantID:    in.TenantID,
		ShopDomain:  in.ShopDomain,
		WarehouseID: in.WarehouseID,
		CreatorID:   in.CreatorID,
	}
	if err := uc.repo.Create(ctx, m); err != nil {
		if errors.Is(err, ErrShopAlreadyBound) {
			return nil, ErrShopAlreadyBound
		}
		return nil, fmt.Errorf("shopify bind: persist: %w", err)
	}
	return m, nil
}

// ListShopsUseCase lists all Shopify shop bindings for a tenant.
type ListShopsUseCase struct {
	repo ShopRepo
}

// NewListShopsUseCase constructs a ListShopsUseCase.
func NewListShopsUseCase(repo ShopRepo) *ListShopsUseCase {
	return &ListShopsUseCase{repo: repo}
}

// Execute returns the list of shop mappings for tenantID.
func (uc *ListShopsUseCase) Execute(ctx context.Context, tenantID uuid.UUID) ([]ShopMapping, error) {
	items, err := uc.repo.ListByTenant(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("shopify list: %w", err)
	}
	return items, nil
}

// UnbindShopUseCase removes a Shopify shop binding.
type UnbindShopUseCase struct {
	repo ShopRepo
}

// NewUnbindShopUseCase constructs an UnbindShopUseCase.
func NewUnbindShopUseCase(repo ShopRepo) *UnbindShopUseCase {
	return &UnbindShopUseCase{repo: repo}
}

// Execute deletes the mapping. The operation is idempotent — deleting a
// non-existent or already-deleted mapping is not an error.
func (uc *UnbindShopUseCase) Execute(ctx context.Context, tenantID, id uuid.UUID) error {
	if err := uc.repo.DeleteByID(ctx, tenantID, id); err != nil {
		return fmt.Errorf("shopify unbind: %w", err)
	}
	return nil
}

// --- helpers ---------------------------------------------------------------

// validateDomain checks that domain matches *.myshopify.com.
func validateDomain(domain string) error {
	if !shopifyDomainRE.MatchString(domain) {
		return ErrInvalidDomain
	}
	return nil
}
