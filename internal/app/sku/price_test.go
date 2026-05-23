package sku_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appsku "github.com/hanmahong5-arch/lurus-tally/internal/app/sku"
)

func TestApplyAction_Percentage_Increase(t *testing.T) {
	got, err := appsku.ApplyAction(decimal.NewFromInt(100), "+5%")
	if err != nil {
		t.Fatalf("ApplyAction: %v", err)
	}
	if !got.Equal(decimal.NewFromInt(105)) {
		t.Errorf("got %s, want 105", got)
	}
}

func TestApplyAction_Percentage_Decrease(t *testing.T) {
	got, err := appsku.ApplyAction(decimal.NewFromInt(200), "-10%")
	if err != nil {
		t.Fatalf("ApplyAction: %v", err)
	}
	if !got.Equal(decimal.NewFromInt(180)) {
		t.Errorf("got %s, want 180", got)
	}
}

func TestApplyAction_AbsoluteSet(t *testing.T) {
	got, err := appsku.ApplyAction(decimal.NewFromInt(100), "=199.00")
	if err != nil {
		t.Fatalf("ApplyAction: %v", err)
	}
	if !got.Equal(decimal.NewFromFloat(199)) {
		t.Errorf("got %s, want 199", got)
	}
}

func TestApplyAction_BareNumber_TreatedAsAbsolute(t *testing.T) {
	got, err := appsku.ApplyAction(decimal.NewFromInt(100), "50")
	if err != nil {
		t.Fatalf("ApplyAction: %v", err)
	}
	if !got.Equal(decimal.NewFromInt(50)) {
		t.Errorf("got %s, want 50", got)
	}
}

func TestApplyAction_ClampsNegativeToZero(t *testing.T) {
	got, err := appsku.ApplyAction(decimal.NewFromInt(100), "-150%")
	if err != nil {
		t.Fatalf("ApplyAction: %v", err)
	}
	if !got.IsZero() {
		t.Errorf("got %s, want 0 (clamped)", got)
	}
}

func TestApplyAction_InvalidAction_Errors(t *testing.T) {
	if _, err := appsku.ApplyAction(decimal.NewFromInt(100), "cheaper"); err == nil {
		t.Error("expected error for non-numeric action")
	}
	if _, err := appsku.ApplyAction(decimal.NewFromInt(100), ""); err == nil {
		t.Error("expected error for empty action")
	}
}

// --- use case with mock repo ---

type mockPriceRepo struct {
	skus    []appsku.DefaultSKU
	updates map[uuid.UUID]decimal.Decimal
	listErr error
}

func (m *mockPriceRepo) ListDefaultSKUs(_ context.Context, _ uuid.UUID, _ []uuid.UUID) ([]appsku.DefaultSKU, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.skus, nil
}

func (m *mockPriceRepo) UpdateRetailPrice(_ context.Context, _, skuID uuid.UUID, newPrice decimal.Decimal) error {
	if m.updates == nil {
		m.updates = make(map[uuid.UUID]decimal.Decimal)
	}
	m.updates[skuID] = newPrice
	return nil
}

func TestUpdatePrice_AppliesToEachDefaultSKU(t *testing.T) {
	tenantID := uuid.New()
	sku1, sku2 := uuid.New(), uuid.New()
	repo := &mockPriceRepo{
		skus: []appsku.DefaultSKU{
			{SKUID: sku1, ProductID: uuid.New(), RetailPrice: decimal.NewFromInt(100)},
			{SKUID: sku2, ProductID: uuid.New(), RetailPrice: decimal.NewFromInt(200)},
		},
	}
	uc := appsku.NewUpdatePriceUseCase(repo)

	affected, err := uc.Execute(context.Background(), tenantID, []uuid.UUID{uuid.New(), uuid.New()}, "+10%")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if affected != 2 {
		t.Errorf("affected=%d, want 2", affected)
	}
	if got := repo.updates[sku1]; !got.Equal(decimal.NewFromInt(110)) {
		t.Errorf("sku1 price=%s, want 110", got)
	}
	if got := repo.updates[sku2]; !got.Equal(decimal.NewFromInt(220)) {
		t.Errorf("sku2 price=%s, want 220", got)
	}
}

func TestUpdatePrice_NoProducts_ReturnsZero(t *testing.T) {
	uc := appsku.NewUpdatePriceUseCase(&mockPriceRepo{})
	affected, err := uc.Execute(context.Background(), uuid.New(), nil, "+10%")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if affected != 0 {
		t.Errorf("affected=%d, want 0", affected)
	}
}

func TestUpdatePrice_InvalidAction_FailsBeforeWrite(t *testing.T) {
	repo := &mockPriceRepo{skus: []appsku.DefaultSKU{{SKUID: uuid.New(), RetailPrice: decimal.NewFromInt(100)}}}
	uc := appsku.NewUpdatePriceUseCase(repo)
	_, err := uc.Execute(context.Background(), uuid.New(), []uuid.UUID{uuid.New()}, "bogus")
	if err == nil {
		t.Fatal("expected error for invalid action")
	}
	if len(repo.updates) != 0 {
		t.Errorf("expected no writes on invalid action, got %d", len(repo.updates))
	}
}
