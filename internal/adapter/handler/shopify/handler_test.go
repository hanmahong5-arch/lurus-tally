package shopify_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	handlershopify "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/shopify"
	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appshopify "github.com/hanmahong5-arch/lurus-tally/internal/app/shopify"
)

func init() {
	gin.SetMode(gin.TestMode)
}

const testTenantID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"

// ---- fakes ----------------------------------------------------------------

type fakeBinder struct {
	out *appshopify.ShopMapping
	err error
}

func (f *fakeBinder) Execute(_ context.Context, _ appshopify.BindInput) (*appshopify.ShopMapping, error) {
	return f.out, f.err
}

type fakeLister struct {
	items []appshopify.ShopMapping
	err   error
}

func (f *fakeLister) Execute(_ context.Context, _ uuid.UUID) ([]appshopify.ShopMapping, error) {
	return f.items, f.err
}

type fakeUnbinder struct {
	err error
}

func (f *fakeUnbinder) Execute(_ context.Context, _, _ uuid.UUID) error {
	return f.err
}

// ---- helpers --------------------------------------------------------------

func newRouter(h *handlershopify.Handler) *gin.Engine {
	r := gin.New()
	rg := r.Group("/api/v1")
	rg.Use(func(c *gin.Context) {
		c.Set(middleware.CtxKeyTenantID, uuid.MustParse(testTenantID))
		c.Next()
	})
	h.RegisterRoutes(rg)
	return r
}

func newRouterNoTenant(h *handlershopify.Handler) *gin.Engine {
	r := gin.New()
	rg := r.Group("/api/v1")
	h.RegisterRoutes(rg)
	return r
}

func marshal(t *testing.T, v any) *bytes.Buffer {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.NewBuffer(b)
}

// ---- POST /shopify/shops --------------------------------------------------

func TestBind_Happy(t *testing.T) {
	warehouseID := uuid.New()
	want := &appshopify.ShopMapping{
		ID:          uuid.New(),
		TenantID:    uuid.MustParse(testTenantID),
		ShopDomain:  "myshop.myshopify.com",
		WarehouseID: warehouseID,
		CreatorID:   uuid.MustParse(testTenantID),
	}
	h := handlershopify.New(&fakeBinder{out: want}, &fakeLister{}, &fakeUnbinder{})
	r := newRouter(h)

	body := marshal(t, map[string]string{
		"shop_domain":  "myshop.myshopify.com",
		"warehouse_id": warehouseID.String(),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/shopify/shops", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d; want 201: body=%s", w.Code, w.Body.String())
	}
}

func TestBind_InvalidDomain_Returns422(t *testing.T) {
	h := handlershopify.New(
		&fakeBinder{err: appshopify.ErrInvalidDomain},
		&fakeLister{},
		&fakeUnbinder{},
	)
	r := newRouter(h)

	body := marshal(t, map[string]string{
		"shop_domain":  "notshopify.com",
		"warehouse_id": uuid.New().String(),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/shopify/shops", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d; want 422", w.Code)
	}
}

func TestBind_Duplicate_Returns409(t *testing.T) {
	h := handlershopify.New(
		&fakeBinder{err: appshopify.ErrShopAlreadyBound},
		&fakeLister{},
		&fakeUnbinder{},
	)
	r := newRouter(h)

	body := marshal(t, map[string]string{
		"shop_domain":  "taken.myshopify.com",
		"warehouse_id": uuid.New().String(),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/shopify/shops", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d; want 409", w.Code)
	}
}

func TestBind_WarehouseNotOwned_Returns422(t *testing.T) {
	h := handlershopify.New(
		&fakeBinder{err: appshopify.ErrWarehouseNotOwned},
		&fakeLister{},
		&fakeUnbinder{},
	)
	r := newRouter(h)

	body := marshal(t, map[string]string{
		"shop_domain":  "abc.myshopify.com",
		"warehouse_id": uuid.New().String(),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/shopify/shops", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d; want 422", w.Code)
	}
}

func TestBind_NoTenant_Returns401(t *testing.T) {
	h := handlershopify.New(&fakeBinder{}, &fakeLister{}, &fakeUnbinder{})
	r := newRouterNoTenant(h)

	body := marshal(t, map[string]string{
		"shop_domain":  "abc.myshopify.com",
		"warehouse_id": uuid.New().String(),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/shopify/shops", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d; want 401", w.Code)
	}
}

// ---- GET /shopify/shops ---------------------------------------------------

func TestList_Happy(t *testing.T) {
	items := []appshopify.ShopMapping{
		{ID: uuid.New(), ShopDomain: "a.myshopify.com", WarehouseID: uuid.New(), CreatorID: uuid.New()},
	}
	h := handlershopify.New(&fakeBinder{}, &fakeLister{items: items}, &fakeUnbinder{})
	r := newRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/shopify/shops", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", w.Code)
	}
	var resp struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Items) != 1 {
		t.Errorf("len(items) = %d; want 1", len(resp.Items))
	}
}

// ---- DELETE /shopify/shops/:id --------------------------------------------

func TestUnbind_Happy(t *testing.T) {
	h := handlershopify.New(&fakeBinder{}, &fakeLister{}, &fakeUnbinder{})
	r := newRouter(h)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/shopify/shops/"+uuid.New().String(), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d; want 204", w.Code)
	}
}

func TestUnbind_InternalError(t *testing.T) {
	h := handlershopify.New(&fakeBinder{}, &fakeLister{}, &fakeUnbinder{err: errors.New("db down")})
	r := newRouter(h)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/shopify/shops/"+uuid.New().String(), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d; want 500", w.Code)
	}
}
