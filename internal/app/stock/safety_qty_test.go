package stock_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
)

type fakeSafetyStore struct {
	lastQty  *decimal.Decimal
	lastProd uuid.UUID
	lastWH   uuid.UUID
	calls    int
	err      error
}

func (f *fakeSafetyStore) SetProductSafetyQty(_ context.Context, _, productID uuid.UUID, qty *decimal.Decimal, wh uuid.UUID) error {
	f.calls++
	f.lastProd = productID
	f.lastQty = qty
	f.lastWH = wh
	return f.err
}

type fakeWH struct {
	id  uuid.UUID
	err error
}

func (f fakeWH) DefaultWarehouseID(context.Context, uuid.UUID) (uuid.UUID, error) {
	return f.id, f.err
}

func TestSetSafetyQty_SetsPositiveAndClearsZero(t *testing.T) {
	store := &fakeSafetyStore{}
	whID := uuid.New()
	uc := appstock.NewSetSafetyQtyUseCase(store, fakeWH{id: whID})
	tenant, product := uuid.New(), uuid.New()

	if err := uc.Execute(context.Background(), tenant, product, decimal.NewFromInt(12)); err != nil {
		t.Fatalf("set: %v", err)
	}
	if store.calls != 1 || store.lastProd != product || store.lastWH != whID || store.lastQty == nil || !store.lastQty.Equal(decimal.NewFromInt(12)) {
		t.Fatalf("store got product=%s wh=%s qty=%v calls=%d", store.lastProd, store.lastWH, store.lastQty, store.calls)
	}

	if err := uc.Execute(context.Background(), tenant, product, decimal.Zero); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if store.lastQty != nil || store.lastWH != uuid.Nil {
		t.Fatalf("clear should pass nil qty and skip warehouse lookup, got qty=%v wh=%s", store.lastQty, store.lastWH)
	}
}

func TestSetSafetyQty_RejectsNegativeAndMissingProduct(t *testing.T) {
	uc := appstock.NewSetSafetyQtyUseCase(&fakeSafetyStore{}, fakeWH{id: uuid.New()})
	if err := uc.Execute(context.Background(), uuid.New(), uuid.Nil, decimal.NewFromInt(1)); !errors.Is(err, appstock.ErrInvalidSafetyQty) {
		t.Fatalf("nil product: %v", err)
	}
	if err := uc.Execute(context.Background(), uuid.New(), uuid.New(), decimal.NewFromInt(-1)); !errors.Is(err, appstock.ErrInvalidSafetyQty) {
		t.Fatalf("negative: %v", err)
	}
}

func TestSetSafetyQty_NoWarehouse(t *testing.T) {
	uc := appstock.NewSetSafetyQtyUseCase(&fakeSafetyStore{}, fakeWH{err: errors.New("none")})
	if err := uc.Execute(context.Background(), uuid.New(), uuid.New(), decimal.NewFromInt(5)); !errors.Is(err, appstock.ErrNoWarehouse) {
		t.Fatalf("err = %v, want ErrNoWarehouse", err)
	}
}
