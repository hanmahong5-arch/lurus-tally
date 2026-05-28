package lifecycle

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/webhook"
	reposhopify "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/shopify"
	appimporting "github.com/hanmahong5-arch/lurus-tally/internal/app/importing"
)

// BuildShopifyHandler wires the Shopify webhook handler for registration by
// the caller (router/main.go).
//
// Caller steps (main.go / app.go):
//  1. Read SHOPIFY_WEBHOOK_SECRET from cfg.
//  2. h := BuildShopifyHandler(db, importUC, cfg.ShopifyWebhookSecret, l)
//  3. h.RegisterRoutes(r)   — must happen BEFORE router.New so the path lives
//     on the root gin.Engine, not inside the auth-gated /api/v1 group.
//
// The shopify_shop_map table has no RLS policy (see migration 000039) so the
// regular application DB pool can read it for cross-tenant domain lookups.
func BuildShopifyHandler(
	db *sql.DB,
	importUC *appimporting.ImportOrdersUseCase,
	secret string,
	log *slog.Logger,
) *webhook.Handler {
	resolver := &shopifyShopResolver{repo: reposhopify.New(db)}
	return webhook.New(secret, resolver, importUC, log)
}

// shopifyShopResolver satisfies webhook.ShopResolver.
// It translates reposhopify.ShopMapping → webhook.ShopMapping, keeping the
// handler package free of a direct dependency on the repo package.
type shopifyShopResolver struct {
	repo *reposhopify.ShopMapRepo
}

func (s *shopifyShopResolver) GetByDomain(ctx context.Context, domain string) (*webhook.ShopMapping, error) {
	m, err := s.repo.GetByDomain(ctx, domain)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, nil
	}
	return &webhook.ShopMapping{
		ShopDomain:  m.ShopDomain,
		TenantID:    m.TenantID,
		WarehouseID: m.WarehouseID,
		CreatorID:   m.CreatorID,
	}, nil
}
