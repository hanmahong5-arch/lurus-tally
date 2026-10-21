package onboarding_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appob "github.com/hanmahong5-arch/lurus-tally/internal/app/onboarding"
)

type fakeSKUWriter struct{ calls []appob.DemoSKU }

func (f *fakeSKUWriter) InsertDemoSKU(_ context.Context, in appob.DemoSKU) error {
	f.calls = append(f.calls, in)
	return nil
}

func seedWith(t *testing.T, p appob.Persona) (*fakeProductCreator, *fakeSKUWriter) {
	t.Helper()
	products := &fakeProductCreator{}
	skus := &fakeSKUWriter{}
	uc := appob.NewSeedDemoUseCase(products, &fakeStockInitializer{}, &fakeSalesRecorder{}).WithSKUWriter(skus)
	if _, err := uc.Execute(context.Background(), appob.SeedInput{
		TenantID: uuid.New(), WarehouseID: uuid.New(), Persona: p,
	}); err != nil {
		t.Fatalf("seed %s: %v", p, err)
	}
	return products, skus
}

func TestSeedDemo_RetailIsHardwareWithPricesAndBarcodes(t *testing.T) {
	products, skus := seedWith(t, appob.PersonaRetail)
	var names []string
	for _, p := range products.calls {
		names = append(names, p.Name)
	}
	joined := strings.Join(names, "|")
	for _, want := range []string{"螺丝", "扳手", "PPR"} {
		if !strings.Contains(joined, want) {
			t.Errorf("retail demo should include %q, got %v", want, names)
		}
	}
	if len(skus.calls) != 3 {
		t.Fatalf("want 3 default SKUs, got %d", len(skus.calls))
	}
	for _, s := range skus.calls {
		if !s.Retail.IsPositive() || !s.Purchase.IsPositive() || s.Retail.LessThanOrEqual(s.Purchase) {
			t.Errorf("SKU prices must be set with retail above purchase: %+v", s)
		}
		if s.UnitName == "" {
			t.Errorf("SKU needs a unit name: %+v", s)
		}
	}
}

// The scanner, product form and import de-dup all read attributes.barcode.
func TestSeedDemo_RetailBarcodesLiveInAttributes(t *testing.T) {
	products, _ := seedWith(t, appob.PersonaRetail)
	seen := map[string]bool{}
	for _, p := range products.calls {
		var attrs map[string]string
		if err := json.Unmarshal(p.Attributes, &attrs); err != nil {
			t.Fatalf("%s: attributes: %v", p.Name, err)
		}
		bc := attrs["barcode"]
		if bc == "" || seen[bc] {
			t.Errorf("%s: need a unique attributes.barcode, got %q", p.Name, bc)
		}
		seen[bc] = true
	}
}

func TestSeedDemo_HorticultureCarriesSpecs(t *testing.T) {
	products, _ := seedWith(t, appob.PersonaHorticulture)
	for _, p := range products.calls {
		if p.Spec == "" {
			t.Errorf("%s: nursery stock is sold by spec (胸径/冠幅), Spec must be set", p.Name)
		}
	}
}

func TestSeedDemo_WithoutSKUWriterStillSeeds(t *testing.T) {
	products := &fakeProductCreator{}
	uc := appob.NewSeedDemoUseCase(products, &fakeStockInitializer{}, &fakeSalesRecorder{})
	res, err := uc.Execute(context.Background(), appob.SeedInput{
		TenantID: uuid.New(), WarehouseID: uuid.New(), Persona: appob.PersonaRetail,
	})
	if err != nil || res.ProductsCreated != 3 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestDemoSKU_PricesAreDecimal(t *testing.T) {
	_, skus := seedWith(t, appob.PersonaRetail)
	if skus.calls[0].Retail.Equal(decimal.Zero) {
		t.Fatal("retail price must not be zero")
	}
}
