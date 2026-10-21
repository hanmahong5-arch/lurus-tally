package product_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appsku "github.com/hanmahong5-arch/lurus-tally/internal/app/sku"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/product"
)

// fakeRetailPrices serves canned default-SKU rows keyed by product id.
type fakeRetailPrices struct {
	rows  map[uuid.UUID]decimal.Decimal
	units map[uuid.UUID]string
	err   error
}

func (f *fakeRetailPrices) ListDefaultSKUs(_ context.Context, _ uuid.UUID, ids []uuid.UUID) ([]appsku.DefaultSKU, error) {
	if f.err != nil {
		return nil, f.err
	}
	var out []appsku.DefaultSKU
	for _, id := range ids {
		if p, ok := f.rows[id]; ok {
			out = append(out, appsku.DefaultSKU{ProductID: id, RetailPrice: p, UnitName: f.units[id]})
		}
	}
	return out, nil
}

// listingRepo returns every seeded product from List (the base mock returns none).
type listingRepo struct{ *mockProductRepo }

func (l listingRepo) List(_ context.Context, _ domain.ListFilter) ([]*domain.Product, int, error) {
	out := make([]*domain.Product, 0, len(l.products))
	for _, p := range l.products {
		out = append(out, p)
	}
	return out, len(out), nil
}

func getJSON(t *testing.T, path string, handlerSetup func() http.Handler) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("X-Tenant-ID", testTenantID)
	w := httptest.NewRecorder()
	handlerSetup().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp
}

func TestProductHandler_List_IncludesDefaultSKURetailPrice(t *testing.T) {
	repo := newMockProductRepo()
	priced := seedProduct(repo)
	unpriced := seedProduct(repo)
	h := newTestProductHandler(listingRepo{repo})
	h.WithPriceLookup(&fakeRetailPrices{rows: map[uuid.UUID]decimal.Decimal{priced: decimal.RequireFromString("12.5")}})
	r := newProductRouter(h)

	resp := getJSON(t, "/api/v1/products", func() http.Handler { return r })
	items := resp["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	for _, it := range items {
		m := it.(map[string]any)
		switch m["id"] {
		case priced.String():
			if m["retail_price"] != "12.5" {
				t.Errorf("priced retail_price = %v, want \"12.5\"", m["retail_price"])
			}
		case unpriced.String():
			if _, ok := m["retail_price"]; ok {
				t.Errorf("unpriced product must omit retail_price, got %v", m["retail_price"])
			}
		}
		if m["name"] == nil {
			t.Error("product fields must stay flattened next to retail_price")
		}
	}
}

func TestProductHandler_GetByID_IncludesRetailPrice(t *testing.T) {
	repo := newMockProductRepo()
	id := seedProduct(repo)
	h := newTestProductHandler(repo)
	h.WithPriceLookup(&fakeRetailPrices{rows: map[uuid.UUID]decimal.Decimal{id: decimal.RequireFromString("88")}})
	r := newProductRouter(h)

	resp := getJSON(t, "/api/v1/products/"+id.String(), func() http.Handler { return r })
	if resp["retail_price"] != "88" {
		t.Errorf("retail_price = %v, want \"88\"", resp["retail_price"])
	}
	if resp["id"] != id.String() {
		t.Errorf("id = %v", resp["id"])
	}
}

func TestProductHandler_List_PriceLookupError_DegradesWithoutPrice(t *testing.T) {
	repo := newMockProductRepo()
	seedProduct(repo)
	h := newTestProductHandler(listingRepo{repo})
	h.WithPriceLookup(&fakeRetailPrices{err: errors.New("db down")})
	r := newProductRouter(h)

	resp := getJSON(t, "/api/v1/products", func() http.Handler { return r })
	items := resp["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	if _, ok := items[0].(map[string]any)["retail_price"]; ok {
		t.Error("retail_price must be omitted when lookup fails")
	}
}

func TestProductHandler_GetByID_IncludesRetailPriceUnit(t *testing.T) {
	repo := newMockProductRepo()
	id := seedProduct(repo)
	h := newTestProductHandler(repo)
	h.WithPriceLookup(&fakeRetailPrices{
		rows:  map[uuid.UUID]decimal.Decimal{id: decimal.RequireFromString("9")},
		units: map[uuid.UUID]string{id: "个"},
	})
	r := newProductRouter(h)

	resp := getJSON(t, "/api/v1/products/"+id.String(), func() http.Handler { return r })
	if resp["retail_price_unit"] != "个" {
		t.Errorf("retail_price_unit = %v, want \"个\"", resp["retail_price_unit"])
	}
}
