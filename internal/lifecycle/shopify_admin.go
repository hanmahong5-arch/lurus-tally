package lifecycle

import (
	"context"

	"github.com/google/uuid"
	reposhopify "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/shopify"
	appshopify "github.com/hanmahong5-arch/lurus-tally/internal/app/shopify"
)

// shopRepoAdapter implements appshopify.ShopRepo by wrapping the repo layer's
// ShopMapRepo and translating the two ShopMapping struct types (identical fields,
// different packages — kept separate so the app layer carries no repo import).
type shopRepoAdapter struct {
	r *reposhopify.ShopMapRepo
}

func newShopRepoAdapter(r *reposhopify.ShopMapRepo) *shopRepoAdapter {
	return &shopRepoAdapter{r: r}
}

func (a *shopRepoAdapter) Create(ctx context.Context, m *appshopify.ShopMapping) error {
	return a.r.Create(ctx, &reposhopify.ShopMapping{
		ID:          m.ID,
		ShopDomain:  m.ShopDomain,
		TenantID:    m.TenantID,
		WarehouseID: m.WarehouseID,
		CreatorID:   m.CreatorID,
	})
}

func (a *shopRepoAdapter) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]appshopify.ShopMapping, error) {
	rows, err := a.r.ListByTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]appshopify.ShopMapping, len(rows))
	for i, m := range rows {
		out[i] = appshopify.ShopMapping{
			ID:          m.ID,
			TenantID:    m.TenantID,
			ShopDomain:  m.ShopDomain,
			WarehouseID: m.WarehouseID,
			CreatorID:   m.CreatorID,
		}
	}
	return out, nil
}

func (a *shopRepoAdapter) DeleteByID(ctx context.Context, tenantID, id uuid.UUID) error {
	return a.r.DeleteByID(ctx, tenantID, id)
}
