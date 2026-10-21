package onboarding

import (
	"context"
	"fmt"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/dbscope"
	reposku "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/sku"
	appob "github.com/hanmahong5-arch/lurus-tally/internal/app/onboarding"
	appsku "github.com/hanmahong5-arch/lurus-tally/internal/app/sku"
)

var _ appob.DemoSKUWriter = (*Repo)(nil)

// InsertDemoSKU writes the demo product's default SKU through the same writer
// real products use (retail / purchase price), then stamps the shelf unit. The
// row dies with its product in DeleteDemoProducts.
func (r *Repo) InsertDemoSKU(ctx context.Context, in appob.DemoSKU) error {
	if err := reposku.New(r.db).UpsertDefaultSKU(ctx, in.TenantID, in.ProductID, appsku.DefaultPrices{
		Retail: &in.Retail, Purchase: &in.Purchase,
	}); err != nil {
		return fmt.Errorf("onboarding repo: demo sku prices: %w", err)
	}
	if in.UnitName == "" {
		return nil
	}
	const q = `
		UPDATE tally.product_sku SET unit_name = $3
		WHERE tenant_id = $1 AND product_id = $2 AND is_default AND deleted_at IS NULL`
	if _, err := dbscope.From(ctx, r.db).ExecContext(ctx, q, in.TenantID, in.ProductID, in.UnitName); err != nil {
		return fmt.Errorf("onboarding repo: demo sku unit: %w", err)
	}
	return nil
}
