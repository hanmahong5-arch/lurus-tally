package product

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	appsku "github.com/hanmahong5-arch/lurus-tally/internal/app/sku"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/product"
)

type fakeSKUWriter struct {
	calls     int
	productID uuid.UUID
	prices    appsku.DefaultPrices
	err       error
}

func (w *fakeSKUWriter) UpsertDefaultSKU(_ context.Context, _, productID uuid.UUID, p appsku.DefaultPrices) error {
	w.calls++
	w.productID = productID
	w.prices = p
	return w.err
}

func str(s string) *string { return &s }

func TestCreate_WithPrices_UpsertsDefaultSKU(t *testing.T) {
	repo := &fakeRepo{}
	w := &fakeSKUWriter{}
	uc := NewCreateUseCase(repo).WithPriceWriter(w)

	p, err := uc.ExecuteWithPrices(context.Background(),
		domain.CreateInput{TenantID: uuid.New(), Name: "螺丝"},
		appsku.PriceInput{Retail: str("12.50"), Purchase: str("8")})
	if err != nil {
		t.Fatal(err)
	}
	if w.calls != 1 || w.productID != p.ID {
		t.Fatalf("writer calls=%d productID=%s want %s", w.calls, w.productID, p.ID)
	}
	if w.prices.Retail == nil || w.prices.Retail.String() != "12.5" || w.prices.Purchase.String() != "8" || w.prices.Wholesale != nil {
		t.Fatalf("prices = %+v", w.prices)
	}
}

func TestCreate_NoPrices_SkipsSKU(t *testing.T) {
	w := &fakeSKUWriter{}
	uc := NewCreateUseCase(&fakeRepo{}).WithPriceWriter(w)
	if _, err := uc.ExecuteWithPrices(context.Background(),
		domain.CreateInput{TenantID: uuid.New(), Name: "n"}, appsku.PriceInput{Retail: str("")}); err != nil {
		t.Fatal(err)
	}
	if w.calls != 0 {
		t.Fatalf("no price given must not create a SKU, calls=%d", w.calls)
	}
}

func TestCreate_InvalidPrice_NoProductCreated(t *testing.T) {
	repo := &fakeRepo{}
	w := &fakeSKUWriter{}
	uc := NewCreateUseCase(repo).WithPriceWriter(w)
	_, err := uc.ExecuteWithPrices(context.Background(),
		domain.CreateInput{TenantID: uuid.New(), Name: "n"}, appsku.PriceInput{Retail: str("-3")})
	if err == nil {
		t.Fatal("negative price must fail")
	}
	if repo.createdP != nil || w.calls != 0 {
		t.Fatal("nothing may be written when price is invalid")
	}
}

func TestCreate_SKUWriteError_Propagates(t *testing.T) {
	w := &fakeSKUWriter{err: errors.New("boom")}
	uc := NewCreateUseCase(&fakeRepo{}).WithPriceWriter(w)
	if _, err := uc.ExecuteWithPrices(context.Background(),
		domain.CreateInput{TenantID: uuid.New(), Name: "n"}, appsku.PriceInput{Retail: str("1")}); err == nil {
		t.Fatal("expected error")
	}
}

func TestUpdate_WithPrices_UpsertsAndKeepsUnsetFields(t *testing.T) {
	pid := uuid.New()
	repo := &fakeRepo{getByIDProduct: &domain.Product{ID: pid, Name: "old"}}
	w := &fakeSKUWriter{}
	uc := NewUpdateUseCase(repo).WithPriceWriter(w)

	_, err := uc.ExecuteWithPrices(context.Background(), uuid.New(), pid, domain.UpdateInput{},
		appsku.PriceInput{Wholesale: str("9.9")})
	if err != nil {
		t.Fatal(err)
	}
	if w.calls != 1 || w.productID != pid || w.prices.Wholesale == nil || w.prices.Retail != nil {
		t.Fatalf("calls=%d prices=%+v", w.calls, w.prices)
	}
}

func TestUpdate_InvalidPrice_NoWrite(t *testing.T) {
	pid := uuid.New()
	repo := &fakeRepo{getByIDProduct: &domain.Product{ID: pid, Name: "old"}}
	uc := NewUpdateUseCase(repo).WithPriceWriter(&fakeSKUWriter{})
	if _, err := uc.ExecuteWithPrices(context.Background(), uuid.New(), pid, domain.UpdateInput{},
		appsku.PriceInput{Retail: str("x")}); err == nil {
		t.Fatal("expected error")
	}
	if repo.updatedP != nil {
		t.Fatal("product must not be updated")
	}
}
