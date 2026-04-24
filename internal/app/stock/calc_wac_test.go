package stock_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appstock "github.com/hanmahong5-arch/lurus-tally/internal/app/stock"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/stock"
)

var (
	testTenantID    = uuid.New()
	testProductID   = uuid.New()
	testWarehouseID = uuid.New()
)

// d is a test helper that parses a decimal string, panicking on invalid input.
func d(s string) decimal.Decimal {
	v, err := decimal.NewFromString(s)
	if err != nil {
		panic("invalid decimal: " + s)
	}
	return v
}

// TestWAC_ApplyMovement_Inbound_UpdatesAvgCost verifies AC-2:
// initial 100@10 + in 50@12 → avg = (100*10 + 50*12)/150 = 10.666667
func TestWAC_ApplyMovement_Inbound_UpdatesAvgCost(t *testing.T) {
	snap := &domain.Snapshot{
		ID: uuid.New(), TenantID: testTenantID, ProductID: testProductID, WarehouseID: testWarehouseID,
		OnHandQty: d("100"), UnitCost: d("10"),
	}
	calc := appstock.NewCalculator(stubProfile{"wac"}, newMockRepo(snap))

	m := newMovement(testTenantID, testProductID, testWarehouseID, domain.DirectionIn, d("50"), d("12"), domain.RefPurchase)
	result, err := calc.ApplyMovement(context.Background(), nil, m)
	if err != nil {
		t.Fatalf("ApplyMovement error: %v", err)
	}

	if !result.OnHandQty.Equal(d("150")) {
		t.Errorf("OnHandQty = %s, want 150", result.OnHandQty)
	}
	// (100*10 + 50*12) / 150 = 1600/150 = 10.666667 (rounded to 6dp)
	want := d("10.666667")
	if !result.UnitCost.Equal(want) {
		t.Errorf("UnitCost = %s, want %s", result.UnitCost, want)
	}
}

// TestWAC_ApplyMovement_Inbound_ZeroInitial: starting from zero, avg = inbound cost.
func TestWAC_ApplyMovement_Inbound_ZeroInitial(t *testing.T) {
	calc := appstock.NewCalculator(stubProfile{"wac"}, newMockRepo(nil))

	m := newMovement(testTenantID, testProductID, testWarehouseID, domain.DirectionIn, d("50"), d("15"), domain.RefPurchase)
	result, err := calc.ApplyMovement(context.Background(), nil, m)
	if err != nil {
		t.Fatalf("ApplyMovement error: %v", err)
	}

	if !result.OnHandQty.Equal(d("50")) {
		t.Errorf("OnHandQty = %s, want 50", result.OnHandQty)
	}
	if !result.UnitCost.Equal(d("15")) {
		t.Errorf("UnitCost = %s, want 15", result.UnitCost)
	}
}

// TestWAC_ApplyMovement_Outbound_DecreasesQty: outbound does not change unit_cost.
func TestWAC_ApplyMovement_Outbound_DecreasesQty(t *testing.T) {
	snap := &domain.Snapshot{
		ID: uuid.New(), TenantID: testTenantID, ProductID: testProductID, WarehouseID: testWarehouseID,
		OnHandQty: d("100"), UnitCost: d("10"),
	}
	calc := appstock.NewCalculator(stubProfile{"wac"}, newMockRepo(snap))

	m := newMovement(testTenantID, testProductID, testWarehouseID, domain.DirectionOut, d("30"), d("0"), domain.RefSale)
	result, err := calc.ApplyMovement(context.Background(), nil, m)
	if err != nil {
		t.Fatalf("ApplyMovement error: %v", err)
	}

	if !result.OnHandQty.Equal(d("70")) {
		t.Errorf("OnHandQty = %s, want 70", result.OnHandQty)
	}
	if !result.UnitCost.Equal(d("10")) {
		t.Errorf("UnitCost = %s, want 10 (unchanged)", result.UnitCost)
	}
}

// TestWAC_ApplyMovement_Adjust_NoCostChange: adjust (±qty) leaves unit_cost unchanged.
func TestWAC_ApplyMovement_Adjust_NoCostChange(t *testing.T) {
	snap := &domain.Snapshot{
		ID: uuid.New(), TenantID: testTenantID, ProductID: testProductID, WarehouseID: testWarehouseID,
		OnHandQty: d("100"), UnitCost: d("10"),
	}
	repo := newMockRepo(snap)
	calc := appstock.NewCalculator(stubProfile{"wac"}, repo)

	// Shrinkage: -10
	m1 := newMovement(testTenantID, testProductID, testWarehouseID, domain.DirectionAdjust, d("-10"), d("0"), domain.RefAdjust)
	r1, err := calc.ApplyMovement(context.Background(), nil, m1)
	if err != nil {
		t.Fatalf("adjust(-10) error: %v", err)
	}
	if !r1.OnHandQty.Equal(d("90")) {
		t.Errorf("OnHandQty = %s, want 90", r1.OnHandQty)
	}
	if !r1.UnitCost.Equal(d("10")) {
		t.Errorf("UnitCost = %s, want 10", r1.UnitCost)
	}

	// Overage: +5
	m2 := newMovement(testTenantID, testProductID, testWarehouseID, domain.DirectionAdjust, d("5"), d("0"), domain.RefAdjust)
	r2, err := calc.ApplyMovement(context.Background(), nil, m2)
	if err != nil {
		t.Fatalf("adjust(+5) error: %v", err)
	}
	if !r2.OnHandQty.Equal(d("95")) {
		t.Errorf("OnHandQty = %s, want 95", r2.OnHandQty)
	}
	if !r2.UnitCost.Equal(d("10")) {
		t.Errorf("UnitCost = %s, want 10", r2.UnitCost)
	}
}

// TestWAC_ValidateMovement_Oversell_Returns422 verifies AC-6.
func TestWAC_ValidateMovement_Oversell_Returns422(t *testing.T) {
	snap := &domain.Snapshot{
		ID: uuid.New(), TenantID: testTenantID, ProductID: testProductID, WarehouseID: testWarehouseID,
		OnHandQty: d("50"), UnitCost: d("10"),
	}
	calc := appstock.NewCalculator(stubProfile{"wac"}, newMockRepo(snap))

	m := newMovement(testTenantID, testProductID, testWarehouseID, domain.DirectionOut, d("100"), d("0"), domain.RefSale)
	err := calc.ValidateMovement(context.Background(), nil, m)
	if err == nil {
		t.Fatal("expected InsufficientStockError, got nil")
	}
	if !appstock.IsInsufficientStock(err) {
		t.Errorf("expected InsufficientStockError, got %T: %v", err, err)
	}
	ise := err.(*appstock.InsufficientStockError)
	if !ise.Available.Equal(d("50")) {
		t.Errorf("Available = %s, want 50", ise.Available)
	}
	if !ise.Requested.Equal(d("100")) {
		t.Errorf("Requested = %s, want 100", ise.Requested)
	}
}
