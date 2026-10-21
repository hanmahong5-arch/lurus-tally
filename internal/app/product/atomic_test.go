package product

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	appsku "github.com/hanmahong5-arch/lurus-tally/internal/app/sku"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/product"
)

// fakeTx records every transaction the use case opens and its outcome.
type fakeTx struct {
	opened int
	errs   []error
}

type fakeTxKey struct{}

// InTx joins an already open transaction, like dbscope.InTx does.
func (f *fakeTx) InTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if ctx.Value(fakeTxKey{}) != nil {
		return fn(ctx)
	}
	f.opened++
	err := fn(context.WithValue(ctx, fakeTxKey{}, true))
	f.errs = append(f.errs, err)
	return err
}

func TestCreate_ProductAndSKUShareOneTransaction(t *testing.T) {
	tx := &fakeTx{}
	w := &fakeSKUWriter{err: errors.New("sku boom")}
	uc := NewCreateUseCase(&fakeRepo{}).WithPriceWriter(w).WithTx(tx)

	_, err := uc.ExecuteWithPrices(context.Background(),
		domain.CreateInput{TenantID: uuid.New(), Name: "n"}, appsku.PriceInput{Retail: str("1")})
	if err == nil {
		t.Fatal("expected error")
	}
	if tx.opened != 1 || tx.errs[0] == nil {
		t.Fatalf("product+sku must run in one tx that fails as a whole: opened=%d errs=%v", tx.opened, tx.errs)
	}
}

func TestCreate_CodeConflictRetriesInFreshTransaction(t *testing.T) {
	tx := &fakeTx{}
	repo := &conflictRepo{conflicts: 1}
	uc := NewCreateUseCase(repo).WithCodeSource(fixedCodes(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16)).WithTx(tx)
	if _, err := uc.ExecuteWithPrices(context.Background(),
		domain.CreateInput{TenantID: uuid.New(), Name: "n"}, appsku.PriceInput{}); err != nil {
		t.Fatal(err)
	}
	if tx.opened != 2 {
		t.Fatalf("each attempt needs its own tx (an aborted tx cannot be reused), opened=%d", tx.opened)
	}
}

func TestUpdate_ProductAndSKUShareOneTransaction(t *testing.T) {
	pid := uuid.New()
	tx := &fakeTx{}
	w := &fakeSKUWriter{err: errors.New("sku boom")}
	repo := &fakeRepo{getByIDProduct: &domain.Product{ID: pid, Name: "old"}}
	uc := NewUpdateUseCase(repo).WithPriceWriter(w).WithTx(tx)
	if _, err := uc.ExecuteWithPrices(context.Background(), uuid.New(), pid, domain.UpdateInput{},
		appsku.PriceInput{Retail: str("2")}); err == nil {
		t.Fatal("expected error")
	}
	if tx.opened != 1 || tx.errs[0] == nil {
		t.Fatalf("opened=%d errs=%v", tx.opened, tx.errs)
	}
}

func TestUpdate_BlankPriceClears_NilPriceKept(t *testing.T) {
	pid := uuid.New()
	repo := &fakeRepo{getByIDProduct: &domain.Product{ID: pid, Name: "old"}}
	w := &fakeSKUWriter{}
	uc := NewUpdateUseCase(repo).WithPriceWriter(w)
	if _, err := uc.ExecuteWithPrices(context.Background(), uuid.New(), pid, domain.UpdateInput{},
		appsku.PriceInput{Wholesale: str("")}); err != nil {
		t.Fatal(err)
	}
	if w.calls != 1 || w.prices.Wholesale == nil || !w.prices.Wholesale.IsZero() || w.prices.Retail != nil {
		t.Fatalf("blank must clear wholesale only: calls=%d %+v", w.calls, w.prices)
	}
}

func TestUpdate_InvalidPrice_IsSentinel(t *testing.T) {
	pid := uuid.New()
	uc := NewUpdateUseCase(&fakeRepo{getByIDProduct: &domain.Product{ID: pid}})
	_, err := uc.ExecuteWithPrices(context.Background(), uuid.New(), pid, domain.UpdateInput{}, appsku.PriceInput{Retail: str("x")})
	if !errors.Is(err, appsku.ErrInvalidPrice) {
		t.Fatalf("err = %v", err)
	}
}

func TestUpdate_AttributesMergeKeepsBarcode(t *testing.T) {
	pid := uuid.New()
	repo := &fakeRepo{getByIDProduct: &domain.Product{ID: pid, Attributes: json.RawMessage(`{"barcode":"690123","is_bulk":false}`)}}
	uc := NewUpdateUseCase(repo)
	p, err := uc.Execute(context.Background(), uuid.New(), pid, domain.UpdateInput{Attributes: json.RawMessage(`{"is_bulk":true,"allow_credit":true}`)})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(p.Attributes, &got); err != nil {
		t.Fatal(err)
	}
	if got["barcode"] != "690123" || got["is_bulk"] != true || got["allow_credit"] != true {
		t.Fatalf("attributes = %s", p.Attributes)
	}
}

func TestUpdate_AttributesNullOrBlankBarcodeRemovesKey(t *testing.T) {
	for _, patch := range []string{`{"barcode":null}`, `{"barcode":""}`} {
		pid := uuid.New()
		repo := &fakeRepo{getByIDProduct: &domain.Product{ID: pid, Attributes: json.RawMessage(`{"barcode":"690123","hs_code":"1"}`)}}
		p, err := NewUpdateUseCase(repo).Execute(context.Background(), uuid.New(), pid, domain.UpdateInput{Attributes: json.RawMessage(patch)})
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		_ = json.Unmarshal(p.Attributes, &got)
		if _, ok := got["barcode"]; ok || got["hs_code"] != "1" {
			t.Fatalf("patch %s -> %s", patch, p.Attributes)
		}
	}
}

func TestUpdate_AttributesInvalidJSON(t *testing.T) {
	pid := uuid.New()
	repo := &fakeRepo{getByIDProduct: &domain.Product{ID: pid}}
	_, err := NewUpdateUseCase(repo).Execute(context.Background(), uuid.New(), pid, domain.UpdateInput{Attributes: json.RawMessage(`[1]`)})
	if err == nil {
		t.Fatal("non-object attributes must be rejected")
	}
}
