package digest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appdigest "github.com/hanmahong5-arch/lurus-tally/internal/app/digest"
	"github.com/hanmahong5-arch/lurus-tally/internal/app/replenish"
)

// stubDigestRepo is a test double for the (now replenish-free) DigestRepo —
// oversell / dead-stock / scorecard only.
type stubDigestRepo struct {
	oversellCount int
	oversellErr   error
	deadCount     int
	deadErr       error
	scorecard     appdigest.ScorecardCounts
	scorecardErr  error
}

func (s *stubDigestRepo) CountOversell(_ context.Context, _ uuid.UUID) (int, error) {
	return s.oversellCount, s.oversellErr
}

func (s *stubDigestRepo) CountDeadStock(_ context.Context, _ uuid.UUID) (int, error) {
	return s.deadCount, s.deadErr
}

func (s *stubDigestRepo) SuggestionScorecard(_ context.Context, _ uuid.UUID) (appdigest.ScorecardCounts, error) {
	return s.scorecard, s.scorecardErr
}

// stubSuggestionRepo is a test double for replenish.SuggestionRepo.
type stubSuggestionRepo struct {
	rows []replenish.RawRow
	err  error
}

func (s *stubSuggestionRepo) ListSuggestions(_ context.Context, _ uuid.UUID) ([]replenish.RawRow, error) {
	return s.rows, s.err
}

// rawRow is a compact RawRow builder for the digest filter tests.
func rawRow(avail, avgDaily, unitCost float64, lead int) replenish.RawRow {
	return replenish.RawRow{
		ProductID:     uuid.New(),
		AvailableQty:  decimal.NewFromFloat(avail),
		AvgDailySales: decimal.NewFromFloat(avgDaily),
		UnitCost:      decimal.NewFromFloat(unitCost),
		LeadTimeDays:  lead,
	}
}

// TestWeeklySummary_ReplenishMatchesROP verifies the Monday card counts exactly
// the SKUs below their learned reorder point (the SAME filter the dashboard
// low-stock alert uses) and sums their EstAmountCNY.
func TestWeeklySummary_ReplenishMatchesROP(t *testing.T) {
	// avgDaily=2, lead 7 → ROP ≈ 16.62.
	below := rawRow(5, 2, 10, 7)    // 5 < 16.62 → counted, SuggestedQty>0 → amount>0
	above := rawRow(500, 2, 10, 7)  // 500 > 16.62 → excluded
	noSignal := rawRow(0, 0, 10, 7) // ROP 0 (no velocity) → skipped

	uc := appdigest.NewWeeklySummaryUseCase(
		&stubDigestRepo{oversellCount: 1, deadCount: 3},
		&stubSuggestionRepo{rows: []replenish.RawRow{above, noSignal, below}},
	)

	s, err := uc.Execute(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if s.ReplenishCount != 1 {
		t.Errorf("ReplenishCount: want 1 (only the below-ROP SKU), got %d", s.ReplenishCount)
	}
	// Amount must equal Σ EstAmountCNY over the counted rows — computed through
	// the same Forecast the use case calls, so it can't drift if constants change.
	want := replenish.Forecast(below, replenish.DefaultWeeks).EstAmountCNY
	if !s.ReplenishAmountCNY.Equal(want) {
		t.Errorf("ReplenishAmountCNY: want %s got %s", want, s.ReplenishAmountCNY)
	}
	if !s.ReplenishAmountCNY.IsPositive() {
		t.Errorf("ReplenishAmountCNY must be positive for a below-ROP SKU, got %s", s.ReplenishAmountCNY)
	}
	if s.OversellCount != 1 {
		t.Errorf("OversellCount: want 1 got %d", s.OversellCount)
	}
	if s.DeadStockCount != 3 {
		t.Errorf("DeadStockCount: want 3 got %d", s.DeadStockCount)
	}
}

// TestWeeklySummary_RepoError_Propagates verifies a suggestion-repo error is surfaced.
func TestWeeklySummary_RepoError_Propagates(t *testing.T) {
	uc := appdigest.NewWeeklySummaryUseCase(
		&stubDigestRepo{},
		&stubSuggestionRepo{err: errors.New("connection reset")},
	)
	_, err := uc.Execute(context.Background(), uuid.New())
	if err == nil {
		t.Error("expected error, got nil")
	}
}

// TestWeeklySummary_Execute_ScorecardPassthrough verifies the repo's scorecard
// counts land unchanged in the Summary (including the empty-ledger zero path).
func TestWeeklySummary_Execute_ScorecardPassthrough(t *testing.T) {
	cases := []struct {
		name      string
		scorecard appdigest.ScorecardCounts
	}{
		{
			name:      "values pass through",
			scorecard: appdigest.ScorecardCounts{Suggested: 12, Adopted: 5, MissedStockout: 2},
		},
		{
			name:      "empty ledger yields zeros",
			scorecard: appdigest.ScorecardCounts{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			uc := appdigest.NewWeeklySummaryUseCase(
				&stubDigestRepo{scorecard: tc.scorecard},
				&stubSuggestionRepo{},
			)
			s, err := uc.Execute(context.Background(), uuid.New())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if s.Suggested != tc.scorecard.Suggested {
				t.Errorf("Suggested: want %d got %d", tc.scorecard.Suggested, s.Suggested)
			}
			if s.Adopted != tc.scorecard.Adopted {
				t.Errorf("Adopted: want %d got %d", tc.scorecard.Adopted, s.Adopted)
			}
			if s.MissedStockout != tc.scorecard.MissedStockout {
				t.Errorf("MissedStockout: want %d got %d", tc.scorecard.MissedStockout, s.MissedStockout)
			}
		})
	}
}

// TestWeeklySummary_Execute_ScorecardError_Propagates verifies a scorecard
// repo error surfaces like the other aggregate errors.
func TestWeeklySummary_Execute_ScorecardError_Propagates(t *testing.T) {
	uc := appdigest.NewWeeklySummaryUseCase(
		&stubDigestRepo{scorecardErr: errors.New("connection reset")},
		&stubSuggestionRepo{},
	)
	_, err := uc.Execute(context.Background(), uuid.New())
	if err == nil {
		t.Error("expected error, got nil")
	}
}

// TestWeeklySummary_EmptyRepo_ReturnsAllZeros verifies the no-data case.
func TestWeeklySummary_EmptyRepo_ReturnsAllZeros(t *testing.T) {
	uc := appdigest.NewWeeklySummaryUseCase(&stubDigestRepo{}, &stubSuggestionRepo{})
	s, err := uc.Execute(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.ReplenishCount != 0 || s.OversellCount != 0 || s.DeadStockCount != 0 {
		t.Errorf("expected all zeros, got %+v", s)
	}
	if !s.ReplenishAmountCNY.IsZero() {
		t.Errorf("expected zero amount, got %s", s.ReplenishAmountCNY)
	}
}
