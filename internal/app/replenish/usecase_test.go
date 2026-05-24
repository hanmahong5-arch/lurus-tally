package replenish_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/app/replenish"
	"github.com/shopspring/decimal"
)

// stubRepo implements SuggestionRepo for tests.
type stubRepo struct {
	rows []replenish.RawRow
	err  error
}

func (s *stubRepo) ListSuggestions(_ context.Context, _ uuid.UUID) ([]replenish.RawRow, error) {
	return s.rows, s.err
}

func d(s string) decimal.Decimal {
	v, _ := decimal.NewFromString(s)
	return v
}

func TestListSuggestions_SuggestedQty_Formula(t *testing.T) {
	// avgDailySales = 5, available = 30, weeks = 2
	// weeklyDemand = 5 × 7 × 2 = 70; suggested = 70 - 30 = 40
	pid := uuid.New()
	rows := []replenish.RawRow{
		{
			ProductID:     pid,
			ProductName:   "Widget A",
			ProductCode:   "W-001",
			AvailableQty:  d("30"),
			SafetyQty:     d("10"),
			UnitCost:      d("25"),
			AvgDailySales: d("5"),
		},
	}
	uc := replenish.NewListSuggestionsUseCase(&stubRepo{rows: rows})
	out, err := uc.Execute(context.Background(), uuid.New(), 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 row, got %d", len(out))
	}
	r := out[0]
	if !r.SuggestedQty.Equal(d("40")) {
		t.Errorf("SuggestedQty = %s, want 40", r.SuggestedQty)
	}
	// estAmount = 40 × 25 = 1000
	if !r.EstAmountCNY.Equal(d("1000")) {
		t.Errorf("EstAmountCNY = %s, want 1000", r.EstAmountCNY)
	}
}

func TestListSuggestions_SuggestedQty_FlooredAtZero(t *testing.T) {
	// available > weeklyDemand → suggested = 0
	rows := []replenish.RawRow{
		{
			ProductID:     uuid.New(),
			ProductName:   "Widget B",
			ProductCode:   "W-002",
			AvailableQty:  d("500"),
			AvgDailySales: d("2"),
			UnitCost:      d("10"),
		},
	}
	uc := replenish.NewListSuggestionsUseCase(&stubRepo{rows: rows})
	out, err := uc.Execute(context.Background(), uuid.New(), 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out[0].SuggestedQty.IsZero() {
		t.Errorf("expected 0, got %s", out[0].SuggestedQty)
	}
}

func TestListSuggestions_UrgencySort_AscendingDaysOfSupply(t *testing.T) {
	// Product A: 3 days of supply (urgent)
	// Product B: 30 days of supply (not urgent)
	// Expected order: A first
	pA := uuid.New()
	pB := uuid.New()
	rows := []replenish.RawRow{
		// Insert B first to test sort
		{ProductID: pB, ProductName: "B", ProductCode: "B", AvailableQty: d("30"), AvgDailySales: d("1"), UnitCost: d("1")},
		{ProductID: pA, ProductName: "A", ProductCode: "A", AvailableQty: d("3"), AvgDailySales: d("1"), UnitCost: d("1")},
	}
	uc := replenish.NewListSuggestionsUseCase(&stubRepo{rows: rows})
	out, err := uc.Execute(context.Background(), uuid.New(), 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out[0].ProductID != pA {
		t.Errorf("expected product A first (most urgent), got %s", out[0].ProductCode)
	}
}

func TestListSuggestions_ZeroVelocity_ScoredLast(t *testing.T) {
	// Zero velocity product should sort after products with any sales
	pZero := uuid.New()
	pActive := uuid.New()
	rows := []replenish.RawRow{
		{ProductID: pZero, ProductName: "Stale", ProductCode: "Z", AvailableQty: d("0"), AvgDailySales: d("0"), UnitCost: d("1")},
		{ProductID: pActive, ProductName: "Active", ProductCode: "A", AvailableQty: d("2"), AvgDailySales: d("5"), UnitCost: d("1")},
	}
	uc := replenish.NewListSuggestionsUseCase(&stubRepo{rows: rows})
	out, err := uc.Execute(context.Background(), uuid.New(), 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out[0].ProductID != pActive {
		t.Errorf("expected active product first, got %s", out[0].ProductCode)
	}
	if out[1].ProductID != pZero {
		t.Errorf("expected zero-velocity product last, got %s", out[1].ProductCode)
	}
}

func TestListSuggestions_DefaultWeeks_UsedWhenZero(t *testing.T) {
	// weeks=0 → falls back to 2; check suggested qty matches 2-week calc
	rows := []replenish.RawRow{
		{ProductID: uuid.New(), ProductName: "X", ProductCode: "X", AvailableQty: d("0"), AvgDailySales: d("1"), UnitCost: d("1")},
	}
	uc := replenish.NewListSuggestionsUseCase(&stubRepo{rows: rows})
	out, err := uc.Execute(context.Background(), uuid.New(), 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 1 × 7 × 2 = 14 - 0 = 14
	if !out[0].SuggestedQty.Equal(d("14")) {
		t.Errorf("expected 14, got %s", out[0].SuggestedQty)
	}
}
