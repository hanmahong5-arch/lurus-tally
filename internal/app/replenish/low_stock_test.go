package replenish_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/app/replenish"
	"github.com/shopspring/decimal"
)

// TestForecast_Pure_MatchesFormula locks the extracted pure Forecast against the
// documented v2 formula (SafetyStock, ROP, UrgencyScore) independently of
// Execute — the regression guard for the A1 extraction.
func TestForecast_Pure_MatchesFormula(t *testing.T) {
	avgDaily := 5.0
	leadTime := 7
	raw := replenish.RawRow{
		ProductID:     uuid.New(),
		ProductName:   "Pure",
		ProductCode:   "P-1",
		AvailableQty:  d("20"),
		AvgDailySales: decimal.NewFromFloat(avgDaily),
		UnitCost:      d("10"),
		LeadTimeDays:  leadTime,
	}
	f := replenish.Forecast(raw, 2)

	wantSS := expectedSafetyStock(avgDaily, leadTime)
	wantROP := decimal.NewFromFloat(avgDaily * float64(leadTime)).Add(wantSS)
	if f.SafetyStock.Sub(wantSS).Abs().GreaterThan(decimal.NewFromFloat(0.01)) {
		t.Errorf("SafetyStock = %s, want ~%s", f.SafetyStock, wantSS)
	}
	if f.ROP.Sub(wantROP).Abs().GreaterThan(decimal.NewFromFloat(0.01)) {
		t.Errorf("ROP = %s, want ~%s", f.ROP, wantROP)
	}
	// UrgencyScore = available / avgDaily = 20 / 5 = 4.
	if !f.UrgencyScore.Equal(d("4")) {
		t.Errorf("UrgencyScore = %s, want 4", f.UrgencyScore)
	}
}

// TestForecast_Pure_AgreesWithExecute proves Execute (which now loops Forecast)
// returns the same per-row result Forecast computes — extraction is byte-stable.
func TestForecast_Pure_AgreesWithExecute(t *testing.T) {
	raw := replenish.RawRow{
		ProductID:     uuid.New(),
		ProductName:   "Agree",
		ProductCode:   "AG-1",
		AvailableQty:  d("30"),
		AvgDailySales: d("5"),
		UnitCost:      d("25"),
		LeadTimeDays:  7,
		InTransit:     d("10"),
	}
	f := replenish.Forecast(raw, 2)

	uc := replenish.NewListSuggestionsUseCase(&stubRepo{rows: []replenish.RawRow{raw}})
	out, err := uc.Execute(context.Background(), uuid.New(), 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := out[0]
	if !got.ROP.Equal(f.ROP) || !got.SafetyStock.Equal(f.SafetyStock) ||
		!got.SuggestedQty.Equal(f.SuggestedQty) || !got.UrgencyScore.Equal(f.UrgencyScore) ||
		!got.EstAmountCNY.Equal(f.EstAmountCNY) {
		t.Errorf("Execute row != Forecast row:\n execute=%+v\n forecast=%+v", got, f)
	}
}

// lowStockRaw is a compact RawRow builder for the filter tests.
func lowStockRaw(code string, available, avgDaily, safety string, leadTime int) replenish.RawRow {
	return replenish.RawRow{
		ProductID:     uuid.New(),
		ProductName:   "P " + code,
		ProductCode:   code,
		AvailableQty:  d(available),
		AvgDailySales: d(avgDaily),
		SafetyQty:     d(safety),
		UnitCost:      d("10"),
		LeadTimeDays:  leadTime,
	}
}

// TestListLowStock_AlertsBelowROP_ExcludesNoSignalAndAmple verifies the core
// filter: a product below its learned ROP alerts; a no-velocity product (ROP 0)
// and an amply-stocked product are excluded.
func TestListLowStock_AlertsBelowROP_ExcludesNoSignalAndAmple(t *testing.T) {
	// avgDaily=2, leadTime=7 → ROP ≈ 2×7 + safety(≈2.62) ≈ 16.62.
	alert := lowStockRaw("A", "5", "2", "0", 7)      // 5 < 16.62 → alert
	noSignal := lowStockRaw("B", "100", "0", "0", 7) // ROP 0 → excluded
	ample := lowStockRaw("C", "500", "2", "0", 7)    // 500 > 16.62 → excluded

	uc := replenish.NewListLowStockUseCase(&stubRepo{rows: []replenish.RawRow{noSignal, ample, alert}})
	rows, err := uc.Execute(context.Background(), uuid.New(), 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 alert row, got %d: %+v", len(rows), rows)
	}
	r := rows[0]
	if r.ProductCode != "A" {
		t.Errorf("expected product A to alert, got %s", r.ProductCode)
	}
	rop := d(r.ReorderPoint)
	avail := d(r.AvailableQty)
	if !rop.GreaterThan(avail) {
		t.Errorf("reorder_point (%s) must exceed available (%s) for an alert", r.ReorderPoint, r.AvailableQty)
	}
	// days_of_supply = available / avgDaily = 5 / 2 = 2.5.
	if r.DaysOfSupply != "2.5" {
		t.Errorf("days_of_supply = %s, want 2.5", r.DaysOfSupply)
	}
}

// TestListLowStock_ExplicitOverrideWins verifies a set low_safe_qty (SafetyQty)
// overrides the learned ROP — alerting even with zero velocity (the forward-
// compatible manual-override path).
func TestListLowStock_ExplicitOverrideWins(t *testing.T) {
	// Zero velocity → learned ROP would be 0; SafetyQty=50 forces threshold 50.
	override := lowStockRaw("OV", "10", "0", "50", 7) // 10 < 50 → alert
	uc := replenish.NewListLowStockUseCase(&stubRepo{rows: []replenish.RawRow{override}})
	rows, err := uc.Execute(context.Background(), uuid.New(), 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 alert row via override, got %d", len(rows))
	}
	if rows[0].ReorderPoint != "50" {
		t.Errorf("reorder_point = %s, want 50 (explicit override, not learned ROP)", rows[0].ReorderPoint)
	}
}

// TestListLowStock_SkipsZeroThreshold confirms a no-demand SKU is silent even at
// zero stock (no demand signal = nothing to predict; dead stock is separate).
func TestListLowStock_SkipsZeroThreshold(t *testing.T) {
	dead := lowStockRaw("Z", "0", "0", "0", 7) // ROP 0, no override → skip
	uc := replenish.NewListLowStockUseCase(&stubRepo{rows: []replenish.RawRow{dead}})
	rows, err := uc.Execute(context.Background(), uuid.New(), 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected 0 rows for zero-threshold SKU, got %d: %+v", len(rows), rows)
	}
}

// TestListLowStock_SortedByDaysOfSupply checks most-urgent-first ordering.
func TestListLowStock_SortedByDaysOfSupply(t *testing.T) {
	urgent := lowStockRaw("U", "2", "2", "0", 7) // DoS = 1
	less := lowStockRaw("L", "10", "2", "0", 7)  // DoS = 5 (both < ROP≈16.62)
	uc := replenish.NewListLowStockUseCase(&stubRepo{rows: []replenish.RawRow{less, urgent}})
	rows, err := uc.Execute(context.Background(), uuid.New(), 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 alert rows, got %d", len(rows))
	}
	if rows[0].ProductCode != "U" {
		t.Errorf("expected most-urgent product U first, got %s", rows[0].ProductCode)
	}
}
