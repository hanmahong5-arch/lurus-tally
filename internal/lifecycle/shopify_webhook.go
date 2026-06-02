package lifecycle

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/google/uuid"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/webhook"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dbscope"
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
// The shop→tenant resolution (GetByDomain) is inherently cross-tenant and runs
// before the tenant is known, on the shared pool; shopify_shop_map's RLS policy
// (migration 000043) keeps its empty-GUC arm permissive so that lookup works.
// Once the tenant IS resolved, the import itself runs through WithPinner below so
// its money/stock writes hit a connection scoped to that tenant (RLS backstop),
// instead of the unpinned pool the public webhook would otherwise use.
func BuildShopifyHandler(
	db *sql.DB,
	importUC *appimporting.ImportOrdersUseCase,
	secret string,
	log *slog.Logger,
) *webhook.Handler {
	resolver := &shopifyShopResolver{repo: reposhopify.New(db)}
	return webhook.New(secret, resolver, importUC, log).WithPinner(dbscopePinner{db: db})
}

// dbscopePinner adapts dbscope.WithPinnedConn to webhook.TenantPinner so the
// webhook handler stays free of a direct database/sql dependency.
type dbscopePinner struct{ db *sql.DB }

func (p dbscopePinner) WithPinnedConn(ctx context.Context, tenantID uuid.UUID, fn func(context.Context) error) error {
	return dbscope.WithPinnedConn(ctx, p.db, tenantID.String(), fn)
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
