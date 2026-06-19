package onboarding_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appob "github.com/hanmahong5-arch/lurus-tally/internal/app/onboarding"
	domainproduct "github.com/hanmahong5-arch/lurus-tally/internal/domain/product"
	domainstock "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
)

// --- fakes ---

type fakeProductCreator struct {
	calls  []domainproduct.CreateInput
	errOn  string // code to fail on
	errMsg string
}

func (f *fakeProductCreator) Execute(_ context.Context, in domainproduct.CreateInput) (*domainproduct.Product, error) {
	f.calls = append(f.calls, in)
	if f.errOn != "" && in.Code == f.errOn {
		return nil, errors.New(f.errMsg)
	}
	return &domainproduct.Product{
		ID:       uuid.New(),
		TenantID: in.TenantID,
		Code:     in.Code,
		Name:     in.Name,
		Remark:   in.Remark,
	}, nil
}

type fakeStockInitializer struct {
	calls []appob.StockInitRequest
	err   error
}

func (f *fakeStockInitializer) Execute(_ context.Context, req appob.StockInitRequest) (*domainstock.Snapshot, error) {
	f.calls = append(f.calls, req)
	if f.err != nil {
		return nil, f.err
	}
	return &domainstock.Snapshot{
		TenantID:    req.TenantID,
		ProductID:   req.ProductID,
		WarehouseID: req.WarehouseID,
		OnHandQty:   req.Qty,
	}, nil
}

// fakeSalesRecorder records (no-op) the backdated demo sales the seed emits.
type fakeSalesRecorder struct {
	calls []appob.DemoSaleRequest
	err   error
}

func (f *fakeSalesRecorder) RecordSale(_ context.Context, req appob.DemoSaleRequest) error {
	f.calls = append(f.calls, req)
	return f.err
}

type fakeDemoDeleter struct {
	called   bool
	tenantID uuid.UUID
	err      error
}

func (f *fakeDemoDeleter) DeleteDemoProducts(_ context.Context, tenantID uuid.UUID) error {
	f.called = true
	f.tenantID = tenantID
	return f.err
}

// --- SeedDemoUseCase tests ---

func TestSeedDemoUseCase_Execute_CrossBorder(t *testing.T) {
	products := &fakeProductCreator{}
	stock := &fakeStockInitializer{}
	sales := &fakeSalesRecorder{}
	uc := appob.NewSeedDemoUseCase(products, stock, sales)

	tenantID := uuid.New()
	warehouseID := uuid.New()

	result, err := uc.Execute(context.Background(), appob.SeedInput{
		TenantID:    tenantID,
		WarehouseID: warehouseID,
		Persona:     appob.PersonaCrossBorder,
	})
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}
	if result.ProductsCreated != 3 {
		t.Errorf("want 3 products created, got %d", result.ProductsCreated)
	}
	if len(products.calls) != 3 {
		t.Errorf("want 3 product.Create calls, got %d", len(products.calls))
	}
	for _, p := range products.calls {
		if p.Remark != "DEMO" {
			t.Errorf("product %s: want remark=DEMO, got %q", p.Code, p.Remark)
		}
		if p.TenantID != tenantID {
			t.Errorf("product %s: want tenantID=%s, got %s", p.Code, tenantID, p.TenantID)
		}
	}
	// One opening receipt per SKU (the over-received 'in').
	if len(stock.calls) != 3 {
		t.Errorf("want 3 stock.Execute calls, got %d", len(stock.calls))
	}
	for _, s := range stock.calls {
		if s.WarehouseID != warehouseID {
			t.Errorf("stock call: want warehouseID=%s, got %s", warehouseID, s.WarehouseID)
		}
		if s.Qty.LessThanOrEqual(decimal.Zero) {
			t.Errorf("stock call: qty must be positive, got %s", s.Qty)
		}
		if s.OccurredAt.IsZero() {
			t.Errorf("stock call: opening receipt must be backdated, got zero OccurredAt")
		}
	}
	// Sales are recorded per SKU (3 SKUs × K parts). All must be backdated.
	if len(sales.calls) == 0 {
		t.Errorf("want backdated demo sales recorded, got none")
	}
	for _, s := range sales.calls {
		if s.OccurredAt.IsZero() {
			t.Errorf("sale call: must be backdated, got zero OccurredAt")
		}
		if s.Qty.LessThanOrEqual(decimal.Zero) {
			t.Errorf("sale call: qty must be positive, got %s", s.Qty)
		}
		// Sale price must exceed cost so the gross-margin report shows positive margin.
		if !s.UnitPrice.GreaterThan(s.UnitCost) {
			t.Errorf("sale call: unit_price (%s) must exceed unit_cost (%s)", s.UnitPrice, s.UnitCost)
		}
	}
}

func TestSeedDemoUseCase_Execute_Retail(t *testing.T) {
	products := &fakeProductCreator{}
	stock := &fakeStockInitializer{}
	uc := appob.NewSeedDemoUseCase(products, stock, &fakeSalesRecorder{})

	result, err := uc.Execute(context.Background(), appob.SeedInput{
		TenantID:    uuid.New(),
		WarehouseID: uuid.New(),
		Persona:     appob.PersonaRetail,
	})
	if err != nil {
		t.Fatalf("Execute retail: unexpected error: %v", err)
	}
	if result.ProductsCreated != 3 {
		t.Errorf("want 3 products created, got %d", result.ProductsCreated)
	}
}

func TestSeedDemoUseCase_Execute_MissingTenantID(t *testing.T) {
	uc := appob.NewSeedDemoUseCase(&fakeProductCreator{}, &fakeStockInitializer{}, &fakeSalesRecorder{})
	_, err := uc.Execute(context.Background(), appob.SeedInput{
		TenantID:    uuid.Nil,
		WarehouseID: uuid.New(),
		Persona:     appob.PersonaRetail,
	})
	if err == nil {
		t.Fatal("want error for nil tenant_id, got nil")
	}
}

func TestSeedDemoUseCase_Execute_MissingWarehouseID(t *testing.T) {
	uc := appob.NewSeedDemoUseCase(&fakeProductCreator{}, &fakeStockInitializer{}, &fakeSalesRecorder{})
	_, err := uc.Execute(context.Background(), appob.SeedInput{
		TenantID:    uuid.New(),
		WarehouseID: uuid.Nil,
		Persona:     appob.PersonaRetail,
	})
	if err == nil {
		t.Fatal("want error for nil warehouse_id, got nil")
	}
}

func TestSeedDemoUseCase_Execute_SkipsDuplicateCode(t *testing.T) {
	products := &fakeProductCreator{
		errOn:  "DEMO-RT-001",
		errMsg: "duplicate key value violates unique constraint",
	}
	stock := &fakeStockInitializer{}
	uc := appob.NewSeedDemoUseCase(products, stock, &fakeSalesRecorder{})

	result, err := uc.Execute(context.Background(), appob.SeedInput{
		TenantID:    uuid.New(),
		WarehouseID: uuid.New(),
		Persona:     appob.PersonaRetail,
	})
	if err != nil {
		t.Fatalf("Execute with duplicate: unexpected error: %v", err)
	}
	// Duplicate was skipped — 2 instead of 3 products created.
	if result.ProductsCreated != 2 {
		t.Errorf("want 2 products created (1 duplicate skipped), got %d", result.ProductsCreated)
	}
	// Stock should only be called for the 2 non-duplicate products.
	if len(stock.calls) != 2 {
		t.Errorf("want 2 stock calls, got %d", len(stock.calls))
	}
}

func TestSeedDemoUseCase_Execute_StockError(t *testing.T) {
	products := &fakeProductCreator{}
	stock := &fakeStockInitializer{err: errors.New("db down")}
	uc := appob.NewSeedDemoUseCase(products, stock, &fakeSalesRecorder{})

	_, err := uc.Execute(context.Background(), appob.SeedInput{
		TenantID:    uuid.New(),
		WarehouseID: uuid.New(),
		Persona:     appob.PersonaRetail,
	})
	if err == nil {
		t.Fatal("want error when stock returns error, got nil")
	}
}

// --- ClearDemoUseCase tests ---

func TestClearDemoUseCase_Execute_CallsRepo(t *testing.T) {
	del := &fakeDemoDeleter{}
	uc := appob.NewClearDemoUseCase(del)

	tenantID := uuid.New()
	if err := uc.Execute(context.Background(), tenantID); err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}
	if !del.called {
		t.Error("want DeleteDemoProducts to be called")
	}
	if del.tenantID != tenantID {
		t.Errorf("want tenantID=%s, got %s", tenantID, del.tenantID)
	}
}

func TestClearDemoUseCase_Execute_MissingTenantID(t *testing.T) {
	del := &fakeDemoDeleter{}
	uc := appob.NewClearDemoUseCase(del)

	if err := uc.Execute(context.Background(), uuid.Nil); err == nil {
		t.Fatal("want error for nil tenant_id, got nil")
	}
}

func TestClearDemoUseCase_Execute_PropagatesRepoError(t *testing.T) {
	del := &fakeDemoDeleter{err: errors.New("db down")}
	uc := appob.NewClearDemoUseCase(del)

	if err := uc.Execute(context.Background(), uuid.New()); err == nil {
		t.Fatal("want error when repo returns error, got nil")
	}
}
