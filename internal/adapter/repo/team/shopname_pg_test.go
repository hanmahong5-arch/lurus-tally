package team

import (
	"context"
	"testing"
)

func TestPrintShopName(t *testing.T) {
	f := newPGFixture(t)
	ctx := context.Background()
	if n, err := f.store.PrintShopName(ctx, f.tenant); err != nil || n != "" {
		t.Fatalf("unset: %q %v", n, err)
	}
	f.exec(t, `UPDATE tally.tenant SET settings = jsonb_set(COALESCE(settings,'{}'::jsonb), '{print}', '{"shop_name":"老张五金"}'::jsonb, true) WHERE id=$1`, f.tenant)
	if n, err := f.store.PrintShopName(ctx, f.tenant); err != nil || n != "老张五金" {
		t.Fatalf("set: %q %v", n, err)
	}
}
