package digest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	appdigest "github.com/hanmahong5-arch/lurus-tally/internal/app/digest"
	"github.com/shopspring/decimal"
)

// stubDigestRepo is a test double for DigestRepo.
type stubDigestRepo struct {
	replenishRows []appdigest.ReplenishRow
	replenishErr  error
	oversellCount int
	oversellErr   error
	deadCount     int
	deadErr       error
}

func (s *stubDigestRepo) ListReplenishCandidates(_ context.Context, _ uuid.UUID) ([]appdigest.ReplenishRow, error) {
	return s.replenishRows, s.replenishErr
}

func (s *stubDigestRepo) CountOversell(_ context.Context, _ uuid.UUID) (int, error) {
	return s.oversellCount, s.oversellErr
}

func (s *stubDigestRepo) CountDeadStock(_ context.Context, _ uuid.UUID) (int, error) {
	return s.deadCount, s.deadErr
}

// TestWeeklySummary_HappyPath_ComputesAmountCorrectly verifies the
// suggested-amount formula: Σ(max(avgDaily×14 − available, 0) × unitCost).
func TestWeeklySummary_HappyPath_ComputesAmountCorrectly(t *testing.T) {
	// Product A: avgDaily=5, available=10, coverage=14 → suggested=5*14-10=60, cost=20 → 1200
	// Product B: avgDaily=2, available=25, coverage=14 → suggested=max(2*14-25=3,0), cost=100 → 300
	rows := []appdigest.ReplenishRow{
		{
			ProductID:     uuid.New(),
			AvailableQty:  decimal.NewFromInt(10),
			SafetyQty:     decimal.NewFromInt(5),
			AvgDailySales: decimal.NewFromInt(5),
			UnitCost:      decimal.NewFromInt(20),
		},
		{
			ProductID:     uuid.New(),
			AvailableQty:  decimal.NewFromInt(25),
			SafetyQty:     decimal.NewFromInt(30),
			AvgDailySales: decimal.NewFromInt(2),
			UnitCost:      decimal.NewFromInt(100),
		},
	}

	uc := appdigest.NewWeeklySummaryUseCase(&stubDigestRepo{
		replenishRows: rows,
		oversellCount: 1,
		deadCount:     3,
	})

	s, err := uc.Execute(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if s.ReplenishCount != 2 {
		t.Errorf("ReplenishCount: want 2 got %d", s.ReplenishCount)
	}
	wantAmount := decimal.NewFromInt(1200 + 300) // 1500
	if !s.ReplenishAmountCNY.Equal(wantAmount) {
		t.Errorf("ReplenishAmountCNY: want %s got %s", wantAmount, s.ReplenishAmountCNY)
	}
	if s.OversellCount != 1 {
		t.Errorf("OversellCount: want 1 got %d", s.OversellCount)
	}
	if s.DeadStockCount != 3 {
		t.Errorf("DeadStockCount: want 3 got %d", s.DeadStockCount)
	}
}

// TestWeeklySummary_SuggestedQtyFlooredAtZero verifies that when available > 14*daily
// the suggested qty is clamped to zero and does not contribute negative amount.
func TestWeeklySummary_SuggestedQtyFlooredAtZero(t *testing.T) {
	// available=200, avgDaily=5, coverage=14 → 5*14=70 < 200 → suggested=0
	rows := []appdigest.ReplenishRow{
		{
			ProductID:     uuid.New(),
			AvailableQty:  decimal.NewFromInt(200),
			SafetyQty:     decimal.NewFromInt(10),
			AvgDailySales: decimal.NewFromInt(5),
			UnitCost:      decimal.NewFromInt(50),
		},
	}

	uc := appdigest.NewWeeklySummaryUseCase(&stubDigestRepo{replenishRows: rows})
	s, err := uc.Execute(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !s.ReplenishAmountCNY.IsZero() {
		t.Errorf("expected zero amount when available > coverage target, got %s", s.ReplenishAmountCNY)
	}
}

// TestWeeklySummary_RepoError_Propagates verifies that a repo error is surfaced.
func TestWeeklySummary_RepoError_Propagates(t *testing.T) {
	uc := appdigest.NewWeeklySummaryUseCase(&stubDigestRepo{
		replenishErr: errors.New("connection reset"),
	})
	_, err := uc.Execute(context.Background(), uuid.New())
	if err == nil {
		t.Error("expected error, got nil")
	}
}

// TestWeeklySummary_EmptyRepo_ReturnsAllZeros verifies no-data case.
func TestWeeklySummary_EmptyRepo_ReturnsAllZeros(t *testing.T) {
	uc := appdigest.NewWeeklySummaryUseCase(&stubDigestRepo{})
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
