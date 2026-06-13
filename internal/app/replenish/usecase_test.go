package replenish_test

import (
	"context"
	"math"
	"strings"
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

// safetyStock computes the expected safety stock for assertions.
// z=1.65, σ=avgDaily*0.3, safetyStock = z × σ × √leadTime
func expectedSafetyStock(avgDaily float64, leadTime int) decimal.Decimal {
	sigma := avgDaily * 0.3
	ss := 1.65 * sigma * math.Sqrt(float64(leadTime))
	return decimal.NewFromFloat(ss)
}

// TestForecast_ROP verifies ROP = avgDailySales×leadTime + safetyStock.
func TestForecast_ROP(t *testing.T) {
	avgDaily := 5.0
	leadTime := 7

	rows := []replenish.RawRow{
		{
			ProductID:     uuid.New(),
			ProductName:   "Widget ROP",
			ProductCode:   "R-001",
			AvailableQty:  d("0"),
			UnitCost:      d("10"),
			AvgDailySales: decimal.NewFromFloat(avgDaily),
			LeadTimeDays:  leadTime,
		},
	}
	uc := replenish.NewListSuggestionsUseCase(&stubRepo{rows: rows})
	out, err := uc.Execute(context.Background(), uuid.New(), 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := out[0]

	wantSS := expectedSafetyStock(avgDaily, leadTime)
	wantROP := decimal.NewFromFloat(avgDaily * float64(leadTime)).Add(wantSS)

	// Safety stock tolerance: within 0.01 units.
	diff := r.SafetyStock.Sub(wantSS).Abs()
	if diff.GreaterThan(decimal.NewFromFloat(0.01)) {
		t.Errorf("SafetyStock = %s, want ~%s", r.SafetyStock, wantSS)
	}
	diff = r.ROP.Sub(wantROP).Abs()
	if diff.GreaterThan(decimal.NewFromFloat(0.01)) {
		t.Errorf("ROP = %s, want ~%s", r.ROP, wantROP)
	}
}

// TestForecast_SuggestedQty_NetOfInTransit verifies:
//
//	suggested = ceil(target + safetyStock − available − inTransit)
func TestForecast_SuggestedQty_NetOfInTransit(t *testing.T) {
	// avgDaily=5, leadTime=7, weeks=2
	// safetyStock = 1.65 × (5×0.3) × √7 = 1.65 × 1.5 × 2.6458 = ~6.543
	// target = 5 × 7 × 2 = 70
	// available = 30, inTransit = 10
	// suggested = ceil(70 + 6.543 − 30 − 10) = ceil(36.543) = 37
	avgDaily := 5.0
	leadTime := 7

	pid := uuid.New()
	rows := []replenish.RawRow{
		{
			ProductID:     pid,
			ProductName:   "Widget Net",
			ProductCode:   "N-001",
			AvailableQty:  d("30"),
			UnitCost:      d("25"),
			AvgDailySales: decimal.NewFromFloat(avgDaily),
			LeadTimeDays:  leadTime,
			InTransit:     d("10"),
		},
	}
	uc := replenish.NewListSuggestionsUseCase(&stubRepo{rows: rows})
	out, err := uc.Execute(context.Background(), uuid.New(), 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := out[0]

	ss := expectedSafetyStock(avgDaily, leadTime)
	target := decimal.NewFromFloat(avgDaily * 7 * 2)
	wantRaw := target.Add(ss).Sub(d("30")).Sub(d("10"))
	wantSuggested := wantRaw.Ceil()
	if wantSuggested.IsNegative() {
		wantSuggested = decimal.Zero
	}

	if !r.SuggestedQty.Equal(wantSuggested) {
		t.Errorf("SuggestedQty = %s, want %s (target=%s ss=%s)", r.SuggestedQty, wantSuggested, target, ss)
	}
	// Reason must be non-empty.
	if r.Reason == "" {
		t.Error("Reason must be non-empty")
	}
	// EstAmountCNY = SuggestedQty × unitCost.
	wantAmt := wantSuggested.Mul(d("25"))
	if !r.EstAmountCNY.Equal(wantAmt) {
		t.Errorf("EstAmountCNY = %s, want %s", r.EstAmountCNY, wantAmt)
	}
}

// TestForecast_SuggestedQty_FlooredAtZero ensures no negative suggestions when stock is ample.
func TestForecast_SuggestedQty_FlooredAtZero(t *testing.T) {
	// available=500 >> any reasonable target; suggested must be 0.
	rows := []replenish.RawRow{
		{
			ProductID:     uuid.New(),
			ProductName:   "Widget Full",
			ProductCode:   "F-001",
			AvailableQty:  d("500"),
			AvgDailySales: d("2"),
			UnitCost:      d("10"),
			LeadTimeDays:  7,
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

// TestForecast_ZeroLeadTimeFallsBackToSeven ensures a zero lead_time_days
// in the DB (should not happen after migration but is defensive) uses 7.
func TestForecast_ZeroLeadTimeFallsBackToSeven(t *testing.T) {
	rows := []replenish.RawRow{
		{
			ProductID:     uuid.New(),
			ProductName:   "Widget NoLT",
			ProductCode:   "LT-001",
			AvailableQty:  d("0"),
			AvgDailySales: d("5"),
			UnitCost:      d("10"),
			LeadTimeDays:  0, // DB returned 0; use case must default to 7
		},
	}
	uc := replenish.NewListSuggestionsUseCase(&stubRepo{rows: rows})
	out, err := uc.Execute(context.Background(), uuid.New(), 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := out[0]
	if r.LeadTimeDays != 7 {
		t.Errorf("expected LeadTimeDays=7 (fallback), got %d", r.LeadTimeDays)
	}
	// SafetyStock must be positive (using fallback lead time of 7).
	if !r.SafetyStock.IsPositive() {
		t.Errorf("expected positive SafetyStock, got %s", r.SafetyStock)
	}
}

// TestListSuggestions_UrgencySort_AscendingDaysOfSupply checks urgency ordering is preserved.
func TestListSuggestions_UrgencySort_AscendingDaysOfSupply(t *testing.T) {
	pA := uuid.New()
	pB := uuid.New()
	rows := []replenish.RawRow{
		// Insert B first to test sort.
		{ProductID: pB, ProductName: "B", ProductCode: "B", AvailableQty: d("30"), AvgDailySales: d("1"), UnitCost: d("1"), LeadTimeDays: 7},
		{ProductID: pA, ProductName: "A", ProductCode: "A", AvailableQty: d("3"), AvgDailySales: d("1"), UnitCost: d("1"), LeadTimeDays: 7},
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

// TestListSuggestions_ZeroVelocity_ScoredLast confirms zero-sales items sort last.
func TestListSuggestions_ZeroVelocity_ScoredLast(t *testing.T) {
	pZero := uuid.New()
	pActive := uuid.New()
	rows := []replenish.RawRow{
		{ProductID: pZero, ProductName: "Stale", ProductCode: "Z", AvailableQty: d("0"), AvgDailySales: d("0"), UnitCost: d("1"), LeadTimeDays: 7},
		{ProductID: pActive, ProductName: "Active", ProductCode: "A", AvailableQty: d("2"), AvgDailySales: d("5"), UnitCost: d("1"), LeadTimeDays: 7},
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

// TestListSuggestions_DefaultWeeks_UsedWhenZero checks weeks=0 falls back to 2.
func TestListSuggestions_DefaultWeeks_UsedWhenZero(t *testing.T) {
	avgDaily := 1.0
	leadTime := 7
	rows := []replenish.RawRow{
		{ProductID: uuid.New(), ProductName: "X", ProductCode: "X", AvailableQty: d("0"), AvgDailySales: d("1"), UnitCost: d("1"), LeadTimeDays: leadTime},
	}
	uc := replenish.NewListSuggestionsUseCase(&stubRepo{rows: rows})
	out, err := uc.Execute(context.Background(), uuid.New(), 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// target = 1 × 7 × 2 = 14; safetyStock = 1.65 × 0.3 × √7 ≈ 1.309
	// suggested = ceil(14 + 1.309 − 0 − 0) = ceil(15.309) = 16
	ss := expectedSafetyStock(avgDaily, leadTime)
	target := decimal.NewFromFloat(avgDaily * 7 * 2)
	wantSuggested := target.Add(ss).Ceil()
	if !out[0].SuggestedQty.Equal(wantSuggested) {
		t.Errorf("expected %s (target+ss=%s), got %s", wantSuggested, target.Add(ss), out[0].SuggestedQty)
	}
}

// TestForecast_Reason_NonEmpty verifies every row carries a non-empty Reason.
func TestForecast_Reason_NonEmpty(t *testing.T) {
	rows := []replenish.RawRow{
		{ProductID: uuid.New(), ProductName: "Y", ProductCode: "Y", AvailableQty: d("5"), AvgDailySales: d("3"), UnitCost: d("2"), LeadTimeDays: 14, InTransit: d("2")},
	}
	uc := replenish.NewListSuggestionsUseCase(&stubRepo{rows: rows})
	out, err := uc.Execute(context.Background(), uuid.New(), 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out[0].Reason == "" {
		t.Error("Reason must not be empty")
	}
}

// TestListSuggestions_Execute_LeadTimeThreeStates exercises the lead-time
// resolution matrix: learned (≥2 samples) > configured (≠7) > default (=7).
func TestListSuggestions_Execute_LeadTimeThreeStates(t *testing.T) {
	cases := []struct {
		name        string
		configured  int
		learnedDays float64
		samples     int
		wantDays    int
		wantSource  string
	}{
		{"learned_overrides_default", 7, 12.4, 3, 12, replenish.LeadTimeSourceLearned},
		{"learned_overrides_configured", 15, 4.6, 2, 5, replenish.LeadTimeSourceLearned},
		{"learned_subday_floors_at_one", 7, 0.6, 2, 1, replenish.LeadTimeSourceLearned},
		{"one_sample_not_enough_configured_wins", 10, 3.0, 1, 10, replenish.LeadTimeSourceConfigured},
		{"no_samples_configured", 10, 0, 0, 10, replenish.LeadTimeSourceConfigured},
		// 7 is the NOT NULL DEFAULT — a user-typed 7 is indistinguishable, so "default".
		{"no_samples_seven_is_default", 7, 0, 0, 7, replenish.LeadTimeSourceDefault},
		{"zero_configured_falls_back_to_default", 0, 0, 0, 7, replenish.LeadTimeSourceDefault},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows := []replenish.RawRow{
				{
					ProductID:       uuid.New(),
					ProductName:     "LT " + tc.name,
					ProductCode:     "LT",
					AvailableQty:    d("0"),
					AvgDailySales:   d("5"),
					UnitCost:        d("10"),
					LeadTimeDays:    tc.configured,
					LearnedLeadDays: tc.learnedDays,
					LeadTimeSamples: tc.samples,
				},
			}
			uc := replenish.NewListSuggestionsUseCase(&stubRepo{rows: rows})
			out, err := uc.Execute(context.Background(), uuid.New(), 2)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			r := out[0]
			if r.LeadTimeDays != tc.wantDays {
				t.Errorf("LeadTimeDays = %d, want %d", r.LeadTimeDays, tc.wantDays)
			}
			if r.LeadTimeSource != tc.wantSource {
				t.Errorf("LeadTimeSource = %q, want %q", r.LeadTimeSource, tc.wantSource)
			}
		})
	}
}

// TestListSuggestions_Execute_EstAmountPrefersLastPrice verifies EstAmountCNY
// uses the last approved purchase price when present and positive, otherwise
// the WAC unit cost.
func TestListSuggestions_Execute_EstAmountPrefersLastPrice(t *testing.T) {
	lastPrice := d("8")
	zeroPrice := d("0")
	cases := []struct {
		name      string
		lastPrice *decimal.Decimal
		wantUnit  decimal.Decimal
	}{
		{"nil_last_price_uses_wac", nil, d("10")},
		{"positive_last_price_preferred", &lastPrice, d("8")},
		{"zero_last_price_falls_back_to_wac", &zeroPrice, d("10")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows := []replenish.RawRow{
				{
					ProductID:         uuid.New(),
					ProductName:       "Price " + tc.name,
					ProductCode:       "PR",
					AvailableQty:      d("0"),
					AvgDailySales:     d("5"),
					UnitCost:          d("10"),
					LeadTimeDays:      7,
					LastPurchasePrice: tc.lastPrice,
				},
			}
			uc := replenish.NewListSuggestionsUseCase(&stubRepo{rows: rows})
			out, err := uc.Execute(context.Background(), uuid.New(), 2)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			r := out[0]
			want := r.SuggestedQty.Mul(tc.wantUnit)
			if !r.EstAmountCNY.Equal(want) {
				t.Errorf("EstAmountCNY = %s, want %s (qty=%s unit=%s)", r.EstAmountCNY, want, r.SuggestedQty, tc.wantUnit)
			}
		})
	}
}

// TestListSuggestions_Execute_LearnedReasonAppended verifies the learned
// lead-time explanation is appended to the reason string.
func TestListSuggestions_Execute_LearnedReasonAppended(t *testing.T) {
	rows := []replenish.RawRow{
		{
			ProductID:       uuid.New(),
			ProductName:     "Reason",
			ProductCode:     "RS",
			AvailableQty:    d("0"),
			AvgDailySales:   d("5"),
			UnitCost:        d("10"),
			LeadTimeDays:    7,
			LearnedLeadDays: 12.4,
			LeadTimeSamples: 3,
		},
	}
	uc := replenish.NewListSuggestionsUseCase(&stubRepo{rows: rows})
	out, err := uc.Execute(context.Background(), uuid.New(), 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "基于最近3次实际到货,中位交期12天"
	if !strings.Contains(out[0].Reason, want) {
		t.Errorf("Reason = %q, want it to contain %q", out[0].Reason, want)
	}
	if out[0].LeadTimeSamples != 3 {
		t.Errorf("LeadTimeSamples = %d, want 3", out[0].LeadTimeSamples)
	}
}
