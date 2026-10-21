package stock_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
	"github.com/hanmahong5-arch/lurus-tally/internal/domain/team"
)

// A person without cost.view cannot write a purchase price through a count:
// the unit_cost they send is ignored, so an empty book stays "no price yet".
func TestPostAdjustment_ClerkCannotSeedCost(t *testing.T) {
	tid, p, wh := uuid.New(), uuid.New(), uuid.New()
	repo := &fakeStockRepo{}
	body := `{"warehouse_id":"` + wh.String() + `","items":[{"product_id":"` + p.String() + `","counted_qty":"5","unit_cost":"8.3"}]}`
	c, w := newCtx(http.MethodPost, "/stock/adjustments", []byte(body), &tid, nil)
	c.Set(middleware.CtxKeyShopRole, team.RoleClerk)
	adjustHandler(repo).PostAdjustment(c)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	got := decodeBody(t, w)
	if got["code"] != "cost_required" || got["error"] != "这些商品还没有进价，请老板或店长先填进价，或由他们来盘点" {
		t.Fatalf("body = %v", got)
	}
	if len(repo.movements) != 0 {
		t.Fatalf("nothing may be written: %+v", repo.movements)
	}
}

func TestPostAdjustment_ClerkUnitCostIgnoredWhenBookHasPrice(t *testing.T) {
	tid, p, wh := uuid.New(), uuid.New(), uuid.New()
	repo := &fakeStockRepo{selectForUpdate: &domain.Snapshot{
		ID: uuid.New(), TenantID: tid, ProductID: p, WarehouseID: wh,
		OnHandQty: decimal.NewFromInt(10), UnitCost: decimal.NewFromInt(4),
	}}
	body := `{"warehouse_id":"` + wh.String() + `","items":[{"product_id":"` + p.String() + `","counted_qty":"12","unit_cost":"99"}]}`
	c, w := newCtx(http.MethodPost, "/stock/adjustments", []byte(body), &tid, nil)
	c.Set(middleware.CtxKeyShopRole, team.RoleClerk)
	adjustHandler(repo).PostAdjustment(c)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if len(repo.movements) != 1 || !repo.movements[0].TotalCost.Equal(decimal.NewFromInt(8)) {
		t.Fatalf("the book price 4 must value the 2 extra units, movements = %+v", repo.movements)
	}
}

// An unreadable unit_cost from a clerk is ignored like any other, not a 400.
func TestPostAdjustment_ClerkGarbledUnitCostIsIgnored(t *testing.T) {
	tid, p, wh := uuid.New(), uuid.New(), uuid.New()
	repo := &fakeStockRepo{selectForUpdate: &domain.Snapshot{
		ID: uuid.New(), TenantID: tid, ProductID: p, WarehouseID: wh,
		OnHandQty: decimal.NewFromInt(10), UnitCost: decimal.NewFromInt(4),
	}}
	body := `{"warehouse_id":"` + wh.String() + `","items":[{"product_id":"` + p.String() + `","counted_qty":"7","unit_cost":"abc"}]}`
	c, w := newCtx(http.MethodPost, "/stock/adjustments", []byte(body), &tid, nil)
	c.Set(middleware.CtxKeyShopRole, team.RoleClerk)
	adjustHandler(repo).PostAdjustment(c)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
}

func TestPostAdjustment_ManagerStillSeedsCost(t *testing.T) {
	tid, p, wh := uuid.New(), uuid.New(), uuid.New()
	repo := &fakeStockRepo{}
	body := `{"warehouse_id":"` + wh.String() + `","items":[{"product_id":"` + p.String() + `","counted_qty":"5","unit_cost":"8.3"}]}`
	c, w := newCtx(http.MethodPost, "/stock/adjustments", []byte(body), &tid, nil)
	c.Set(middleware.CtxKeyShopRole, team.RoleManager)
	adjustHandler(repo).PostAdjustment(c)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if len(repo.movements) != 1 || !repo.movements[0].TotalCost.Equal(decimal.RequireFromString("41.5")) {
		t.Fatalf("movements = %+v", repo.movements)
	}
}
