package unit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	repounit "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/unit"
)

type fakeProductUnits struct {
	rows      []repounit.ProductUnit
	err       error
	gotTenant uuid.UUID
	gotProd   uuid.UUID
}

func (f *fakeProductUnits) ProductUnits(_ context.Context, tenantID, productID uuid.UUID) ([]repounit.ProductUnit, error) {
	f.gotTenant, f.gotProd = tenantID, productID
	return f.rows, f.err
}

func productUnitsRouter(h *Handler, tenant uuid.UUID) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if tenant != uuid.Nil {
			c.Set(middleware.CtxKeyTenantID, tenant)
		}
	})
	r.GET("/products/:id/units", h.ProductUnits)
	return r
}

func doProductUnits(r *gin.Engine, id string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/products/"+id+"/units", nil))
	return w
}

func TestProductUnits_OK(t *testing.T) {
	tenant, prod, box := uuid.New(), uuid.New(), uuid.New()
	f := &fakeProductUnits{rows: []repounit.ProductUnit{
		{ID: uuid.New(), Code: "pcs", Name: "个", Factor: decimal.NewFromInt(1), IsBase: true},
		{ID: box, Code: "box", Name: "箱", Factor: decimal.RequireFromString("12")},
	}}
	h := New(nil, nil, nil).WithProductUnits(f)
	w := doProductUnits(productUnitsRouter(h, tenant), prod.String())
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	if f.gotTenant != tenant || f.gotProd != prod {
		t.Fatalf("got %v %v", f.gotTenant, f.gotProd)
	}
	var body struct {
		Items []struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Factor string `json:"conversion_factor"`
			IsBase bool   `json:"is_base"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 2 || !body.Items[0].IsBase || body.Items[1].ID != box.String() || body.Items[1].Factor != "12" {
		t.Fatalf("items = %+v", body.Items)
	}
}

func TestProductUnits_Errors(t *testing.T) {
	tenant := uuid.New()
	nf := New(nil, nil, nil).WithProductUnits(&fakeProductUnits{err: repounit.ErrNotFound})
	if w := doProductUnits(productUnitsRouter(nf, tenant), uuid.NewString()); w.Code != http.StatusNotFound {
		t.Errorf("not found: %d", w.Code)
	}
	boom := New(nil, nil, nil).WithProductUnits(&fakeProductUnits{err: errors.New("boom")})
	if w := doProductUnits(productUnitsRouter(boom, tenant), uuid.NewString()); w.Code != http.StatusInternalServerError {
		t.Errorf("internal: %d", w.Code)
	}
	ok := New(nil, nil, nil).WithProductUnits(&fakeProductUnits{})
	if w := doProductUnits(productUnitsRouter(ok, tenant), "not-a-uuid"); w.Code != http.StatusBadRequest {
		t.Errorf("bad id: %d", w.Code)
	}
	if w := doProductUnits(productUnitsRouter(ok, uuid.Nil), uuid.NewString()); w.Code != http.StatusUnauthorized {
		t.Errorf("no tenant: %d", w.Code)
	}
	none := New(nil, nil, nil)
	if w := doProductUnits(productUnitsRouter(none, tenant), uuid.NewString()); w.Code != http.StatusNotImplemented {
		t.Errorf("not configured: %d", w.Code)
	}
}

func TestProductUnits_EmptyListIsArray(t *testing.T) {
	h := New(nil, nil, nil).WithProductUnits(&fakeProductUnits{})
	w := doProductUnits(productUnitsRouter(h, uuid.New()), uuid.NewString())
	if w.Body.String() != `{"items":[]}` {
		t.Fatalf("body %s", w.Body.String())
	}
}
