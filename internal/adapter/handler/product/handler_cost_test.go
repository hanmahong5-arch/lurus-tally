package product_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/hanmahong5-arch/lurus-tally/internal/adapter/middleware"
	appsku "github.com/hanmahong5-arch/lurus-tally/internal/app/sku"
	"github.com/hanmahong5-arch/lurus-tally/internal/domain/team"
)

type costLookup struct{ id uuid.UUID }

func (f costLookup) ListDefaultSKUs(_ context.Context, _ uuid.UUID, _ []uuid.UUID) ([]appsku.DefaultSKU, error) {
	return []appsku.DefaultSKU{{
		ProductID: f.id, RetailPrice: decimal.RequireFromString("10"),
		WholesalePrice: decimal.RequireFromString("8"), PurchasePrice: decimal.RequireFromString("5"),
	}}, nil
}

func listAs(t *testing.T, role team.Role) map[string]any {
	t.Helper()
	repo := newMockProductRepo()
	id := seedProduct(repo)
	h := newTestProductHandler(listingRepo{repo})
	h.WithPriceLookup(costLookup{id})
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		tid, _ := uuid.Parse(testTenantID)
		c.Set(middleware.CtxKeyTenantID, tid)
		c.Set(middleware.CtxKeyShopRole, role)
	})
	r.GET("/api/v1/products", h.List)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/products", nil))
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	resp := getJSONBody(t, w.Body.Bytes())
	return resp["items"].([]any)[0].(map[string]any)
}

func TestList_PurchasePriceOnlyForCostViewers(t *testing.T) {
	for _, role := range []team.Role{team.RoleClerk, team.RoleCashier} {
		it := listAs(t, role)
		if _, ok := it["purchase_price"]; ok {
			t.Errorf("%s must not receive purchase_price", role)
		}
		if it["retail_price"] != "10" {
			t.Errorf("%s still needs retail_price, got %v", role, it["retail_price"])
		}
	}
	for _, role := range []team.Role{team.RoleOwner, team.RoleManager, team.RoleFinance} {
		if it := listAs(t, role); it["purchase_price"] != "5" {
			t.Errorf("%s should see purchase_price, got %v", role, it["purchase_price"])
		}
	}
}

func getJSONBody(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}
