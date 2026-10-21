package product_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	handlerproduct "github.com/hanmahong5-arch/lurus-tally/internal/adapter/handler/product"
	appproduct "github.com/hanmahong5-arch/lurus-tally/internal/app/product"
	appsku "github.com/hanmahong5-arch/lurus-tally/internal/app/sku"
)

type recordingSKUWriter struct {
	calls  int
	prices appsku.DefaultPrices
}

func (w *recordingSKUWriter) UpsertDefaultSKU(_ context.Context, _, _ uuid.UUID, p appsku.DefaultPrices) error {
	w.calls++
	w.prices = p
	return nil
}

func newPricedHandler(repo *mockProductRepo, w appsku.DefaultSKUWriter) *handlerproduct.Handler {
	return handlerproduct.New(
		appproduct.NewCreateUseCase(repo).WithPriceWriter(w),
		appproduct.NewListUseCase(repo),
		appproduct.NewGetUseCase(repo),
		appproduct.NewUpdateUseCase(repo).WithPriceWriter(w),
		appproduct.NewDeleteUseCase(repo),
		appproduct.NewRestoreUseCase(repo),
	)
}

func sendJSON(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", testTenantID)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestProductHandler_Create_SavesPrices(t *testing.T) {
	repo := newMockProductRepo()
	w := &recordingSKUWriter{}
	r := newProductRouter(newPricedHandler(repo, w))

	rec := sendJSON(t, r, http.MethodPost, "/api/v1/products",
		`{"name":"螺丝","retail_price":"12.50","wholesale_price":"10","purchase_price":""}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if w.calls != 1 || w.prices.Retail == nil || w.prices.Retail.String() != "12.5" ||
		w.prices.Wholesale == nil || w.prices.Purchase != nil {
		t.Fatalf("calls=%d prices=%+v", w.calls, w.prices)
	}
}

func TestProductHandler_Create_BadPrice400(t *testing.T) {
	repo := newMockProductRepo()
	w := &recordingSKUWriter{}
	r := newProductRouter(newPricedHandler(repo, w))

	rec := sendJSON(t, r, http.MethodPost, "/api/v1/products", `{"name":"螺丝","retail_price":"-5"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if len(repo.products) != 0 || w.calls != 0 {
		t.Fatal("nothing may be persisted")
	}
	if got := rec.Body.String(); got != `{"error":"零售价不能为负数"}` {
		t.Fatalf("body must be the plain Chinese message, got %s", got)
	}
}

func TestProductHandler_Update_SavesPrices(t *testing.T) {
	repo := newMockProductRepo()
	id := seedProduct(repo)
	w := &recordingSKUWriter{}
	r := newProductRouter(newPricedHandler(repo, w))

	rec := sendJSON(t, r, http.MethodPut, "/api/v1/products/"+id.String(), `{"retail_price":"13.75"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if w.calls != 1 || w.prices.Retail.String() != "13.75" {
		t.Fatalf("calls=%d prices=%+v", w.calls, w.prices)
	}
}
