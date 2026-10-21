package stock_test

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/stock"
	appreplenish "github.com/hanmahong5-arch/lurus-tally/internal/app/replenish"
	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
)

func adjustHandler(repo *fakeStockRepo) *stock.Handler {
	calc := appstock.NewCalculator(nil, repo) // real WAC: the count must flow through ApplyMovement
	record := appstock.NewRecordMovementUseCase(repo, calc, nil, nil)
	return stock.New(record, appstock.NewGetSnapshotUseCase(repo), appstock.NewListSnapshotsUseCase(repo),
		appstock.NewListMovementsUseCase(repo), appreplenish.NewListLowStockUseCase(&fakeSuggestionRepo{})).
		WithAdjust(appstock.NewAdjustUseCase(repo, record))
}

func TestPostAdjustment_NoTenant_401(t *testing.T) {
	c, w := newCtx(http.MethodPost, "/stock/adjustments", []byte(`{}`), nil, nil)
	adjustHandler(&fakeStockRepo{}).PostAdjustment(c)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestPostAdjustment_BadBodiesAreChinese400(t *testing.T) {
	tid := uuid.New()
	wh, p := uuid.NewString(), uuid.NewString()
	cases := map[string]string{
		"nowarehouse": `{"items":[{"product_id":"` + p + `","counted_qty":"1"}]}`,
		"empty":       `{"warehouse_id":"` + wh + `","items":[]}`,
		"notnumber":   `{"warehouse_id":"` + wh + `","items":[{"product_id":"` + p + `","counted_qty":"abc"}]}`,
		"negative":    `{"warehouse_id":"` + wh + `","items":[{"product_id":"` + p + `","counted_qty":"-2"}]}`,
	}
	for name, body := range cases {
		c, w := newCtx(http.MethodPost, "/stock/adjustments", []byte(body), &tid, nil)
		adjustHandler(&fakeStockRepo{}).PostAdjustment(c)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, w.Code)
			continue
		}
		msg, _ := decodeBody(t, w)["error"].(string)
		if msg == "" || msg[0] < 0x80 {
			t.Errorf("%s: error must be plain Chinese, got %q", name, msg)
		}
	}
}

func TestPostAdjustment_BlankCountedQty_400AndNothingWritten(t *testing.T) {
	tid := uuid.New()
	wh, p := uuid.NewString(), uuid.NewString()
	for name, item := range map[string]string{
		"empty":   `{"product_id":"` + p + `","counted_qty":""}`,
		"blanks":  `{"product_id":"` + p + `","counted_qty":"  "}`,
		"missing": `{"product_id":"` + p + `"}`,
	} {
		repo := &fakeStockRepo{}
		c, w := newCtx(http.MethodPost, "/stock/adjustments", []byte(`{"warehouse_id":"`+wh+`","items":[`+item+`]}`), &tid, nil)
		adjustHandler(repo).PostAdjustment(c)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400", name, w.Code)
		}
		if msg, _ := decodeBody(t, w)["error"].(string); msg != "请填写盘点数量" {
			t.Errorf("%s: error = %q", name, msg)
		}
		if len(repo.movements) != 0 {
			t.Errorf("%s: a blank count must not clear the stock", name)
		}
	}
}

func TestPostAdjustment_EmptyBookWithoutPrice_AsksForCost(t *testing.T) {
	tid, p, wh := uuid.New(), uuid.New(), uuid.New()
	repo := &fakeStockRepo{}
	body := `{"warehouse_id":"` + wh.String() + `","items":[{"product_id":"` + p.String() + `","counted_qty":"5"}]}`
	c, w := newCtx(http.MethodPost, "/stock/adjustments", []byte(body), &tid, nil)
	adjustHandler(repo).PostAdjustment(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	got := decodeBody(t, w)
	ids, _ := got["product_ids"].([]any)
	if got["code"] != "cost_required" || len(ids) != 1 || ids[0] != p.String() {
		t.Fatalf("body = %v", got)
	}
	if len(repo.movements) != 0 {
		t.Fatal("nothing may be written")
	}
}

func TestPostAdjustment_EmptyBookWithPrice_ValuesAtThatPrice(t *testing.T) {
	tid, p, wh := uuid.New(), uuid.New(), uuid.New()
	repo := &fakeStockRepo{}
	body := `{"warehouse_id":"` + wh.String() + `","items":[{"product_id":"` + p.String() + `","counted_qty":"5","unit_cost":"8.3"}]}`
	c, w := newCtx(http.MethodPost, "/stock/adjustments", []byte(body), &tid, nil)
	adjustHandler(repo).PostAdjustment(c)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if len(repo.movements) != 1 || !repo.movements[0].TotalCost.Equal(decimal.RequireFromString("41.5")) {
		t.Fatalf("movements = %+v", repo.movements)
	}
}

func TestPostAdjustment_BadUnitCost_400(t *testing.T) {
	tid := uuid.New()
	body := `{"warehouse_id":"` + uuid.NewString() + `","items":[{"product_id":"` + uuid.NewString() + `","counted_qty":"5","unit_cost":"abc"}]}`
	c, w := newCtx(http.MethodPost, "/stock/adjustments", []byte(body), &tid, nil)
	adjustHandler(&fakeStockRepo{}).PostAdjustment(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestPostAdjustment_TenToSeven_Created(t *testing.T) {
	tid, p, wh := uuid.New(), uuid.New(), uuid.New()
	repo := &fakeStockRepo{selectForUpdate: &domain.Snapshot{
		ID: uuid.New(), TenantID: tid, ProductID: p, WarehouseID: wh,
		OnHandQty: decimal.NewFromInt(10), UnitCost: decimal.NewFromInt(4),
	}}
	body := `{"warehouse_id":"` + wh.String() + `","remark":"盘点","items":[{"product_id":"` + p.String() + `","counted_qty":"7"}]}`
	c, w := newCtx(http.MethodPost, "/stock/adjustments", []byte(body), &tid, nil)
	adjustHandler(repo).PostAdjustment(c)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	got := decodeBody(t, w)
	if got["adjusted"].(float64) != 1 {
		t.Fatalf("body = %v", got)
	}
	if len(repo.movements) != 1 || repo.movements[0].ReferenceType != domain.RefAdjust || !repo.movements[0].QtyBase.Equal(decimal.NewFromInt(-3)) {
		t.Fatalf("movements = %+v", repo.movements)
	}
}

func TestRegisterRoutes_AdjustOnlyWhenEnabled_AndExtraRoutesMounted(t *testing.T) {
	has := func(r *gin.Engine, method, path string) bool {
		for _, ri := range r.Routes() {
			if ri.Method == method && ri.Path == path {
				return true
			}
		}
		return false
	}
	plain := buildHandler(nil, nil, nil)
	r1 := gin.New()
	plain.RegisterRoutes(r1.Group("/api/v1"))
	if has(r1, http.MethodPost, "/api/v1/stock/adjustments") {
		t.Fatal("adjust route must be absent without WithAdjust")
	}

	r2 := gin.New()
	adjustHandler(&fakeStockRepo{}).
		WithExtraRoutes(func(rg *gin.RouterGroup) { rg.POST("/products/import", func(*gin.Context) {}) }).
		RegisterRoutes(r2.Group("/api/v1"))
	if !has(r2, http.MethodPost, "/api/v1/stock/adjustments") || !has(r2, http.MethodPost, "/api/v1/products/import") {
		t.Fatal("adjust and extra routes must be mounted")
	}
}
