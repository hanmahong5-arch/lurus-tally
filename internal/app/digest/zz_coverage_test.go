package digest_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appdigest "github.com/hanmahong5-arch/lurus-tally/internal/app/digest"
	"github.com/hanmahong5-arch/lurus-tally/internal/app/replenish"
)

// seqDigestRepo is a DigestRepo test double that records which of its methods
// were actually invoked, so tests can assert the sequential-reads contract:
// Execute must return on the FIRST error and must never call a later repo
// method after an earlier one has failed (the RLS single-pinned-conn reason
// documented on Execute).
type seqDigestRepo struct {
	oversellCount  int
	oversellErr    error
	oversellCalled bool

	deadCount  int
	deadErr    error
	deadCalled bool

	scorecard       appdigest.ScorecardCounts
	scorecardErr    error
	scorecardCalled bool
}

func (s *seqDigestRepo) CountOversell(_ context.Context, _ uuid.UUID) (int, error) {
	s.oversellCalled = true
	return s.oversellCount, s.oversellErr
}

func (s *seqDigestRepo) CountDeadStock(_ context.Context, _ uuid.UUID) (int, error) {
	s.deadCalled = true
	return s.deadCount, s.deadErr
}

func (s *seqDigestRepo) SuggestionScorecard(_ context.Context, _ uuid.UUID) (appdigest.ScorecardCounts, error) {
	s.scorecardCalled = true
	return s.scorecard, s.scorecardErr
}

// seqSuggestionRepo is a replenish.SuggestionRepo test double that records
// whether ListSuggestions was invoked.
type seqSuggestionRepo struct {
	rows       []replenish.RawRow
	err        error
	listCalled bool
}

func (s *seqSuggestionRepo) ListSuggestions(_ context.Context, _ uuid.UUID) ([]replenish.RawRow, error) {
	s.listCalled = true
	return s.rows, s.err
}

// zzRawRow is a compact RawRow builder mirroring the one in usecase_test.go
// (kept private to this file to avoid touching the existing test file).
func zzRawRow(avail, avgDaily, unitCost float64, lead int) replenish.RawRow {
	return replenish.RawRow{
		ProductID:     uuid.New(),
		AvailableQty:  decimal.NewFromFloat(avail),
		AvgDailySales: decimal.NewFromFloat(avgDaily),
		UnitCost:      decimal.NewFromFloat(unitCost),
		LeadTimeDays:  lead,
	}
}

// TestZZ_Execute_SequentialErrorShortCircuit drives all four early-return
// error branches (ListSuggestions, CountOversell, CountDeadStock,
// SuggestionScorecard) and asserts that every read STRICTLY AFTER the failing
// one is never invoked — the RLS single-pinned-conn sequential-reads
// contract documented on Execute.
func TestZZ_Execute_SequentialErrorShortCircuit(t *testing.T) {
	sentinel := errors.New("connection reset")

	t.Run("ListSuggestions error stops before any repo read", func(t *testing.T) {
		repl := &seqSuggestionRepo{err: sentinel}
		repo := &seqDigestRepo{}

		uc := appdigest.NewWeeklySummaryUseCase(repo, repl)
		got, err := uc.Execute(context.Background(), uuid.New())

		if !errors.Is(err, sentinel) {
			t.Fatalf("err: want sentinel, got %v", err)
		}
		if got != (appdigest.Summary{}) {
			t.Errorf("expected zero-value Summary on error, got %+v", got)
		}
		if repo.oversellCalled || repo.deadCalled || repo.scorecardCalled {
			t.Errorf("no digest repo method should run after ListSuggestions fails: %+v", repo)
		}
	})

	t.Run("CountOversell error stops before dead-stock and scorecard", func(t *testing.T) {
		repl := &seqSuggestionRepo{}
		repo := &seqDigestRepo{oversellErr: sentinel}

		uc := appdigest.NewWeeklySummaryUseCase(repo, repl)
		_, err := uc.Execute(context.Background(), uuid.New())

		if !errors.Is(err, sentinel) {
			t.Fatalf("err: want sentinel, got %v", err)
		}
		if !repl.listCalled {
			t.Error("ListSuggestions should have run before CountOversell")
		}
		if !repo.oversellCalled {
			t.Error("CountOversell should have been called")
		}
		if repo.deadCalled {
			t.Error("CountDeadStock must NOT be called once CountOversell fails")
		}
		if repo.scorecardCalled {
			t.Error("SuggestionScorecard must NOT be called once CountOversell fails")
		}
	})

	t.Run("CountDeadStock error stops before scorecard", func(t *testing.T) {
		repl := &seqSuggestionRepo{}
		repo := &seqDigestRepo{deadErr: sentinel}

		uc := appdigest.NewWeeklySummaryUseCase(repo, repl)
		_, err := uc.Execute(context.Background(), uuid.New())

		if !errors.Is(err, sentinel) {
			t.Fatalf("err: want sentinel, got %v", err)
		}
		if !repo.oversellCalled {
			t.Error("CountOversell should have run before CountDeadStock")
		}
		if !repo.deadCalled {
			t.Error("CountDeadStock should have been called")
		}
		if repo.scorecardCalled {
			t.Error("SuggestionScorecard must NOT be called once CountDeadStock fails")
		}
	})

	t.Run("SuggestionScorecard error propagates as the final read", func(t *testing.T) {
		repl := &seqSuggestionRepo{}
		repo := &seqDigestRepo{scorecardErr: sentinel}

		uc := appdigest.NewWeeklySummaryUseCase(repo, repl)
		_, err := uc.Execute(context.Background(), uuid.New())

		if !errors.Is(err, sentinel) {
			t.Fatalf("err: want sentinel, got %v", err)
		}
		if !repo.oversellCalled || !repo.deadCalled || !repo.scorecardCalled {
			t.Errorf("all three digest repo reads should have run: %+v", repo)
		}
	})
}

// TestZZ_Execute_EmptyRaws_ZeroReplenish verifies the empty-raws technical
// case with an EXACT decimal equality (not just IsZero), per the outline.
func TestZZ_Execute_EmptyRaws_ZeroReplenish(t *testing.T) {
	repo := &seqDigestRepo{}
	repl := &seqSuggestionRepo{rows: []replenish.RawRow{}}

	uc := appdigest.NewWeeklySummaryUseCase(repo, repl)
	s, err := uc.Execute(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.ReplenishCount != 0 {
		t.Errorf("ReplenishCount: want 0 got %d", s.ReplenishCount)
	}
	if !s.ReplenishAmountCNY.Equal(decimal.Zero) {
		t.Errorf("ReplenishAmountCNY: want decimal.Zero got %s", s.ReplenishAmountCNY)
	}
}

// TestZZ_Execute_ROP_Boundary hand-derives the reorder-point threshold via the
// REAL replenish.Forecast + replenish.ReorderPoint (the same shared formula
// Execute uses internally, and the same one the dashboard low-stock alert
// uses) from a raw row whose AvailableQty does not influence the threshold
// itself (ROP depends on AvgDailySales/LeadTimeDays, not AvailableQty). It
// then builds two sibling rows differing only in AvailableQty:
//   - exactly AT the threshold           → must be SKIPPED (>= is exclusive)
//   - one cent BELOW the threshold       → must be COUNTED
//
// This is not self-validating: the expected threshold is derived from the
// shared production formula on a row whose AvailableQty is irrelevant to that
// formula, then used to construct independent inputs whose classification is
// asserted through Execute's real output.
func TestZZ_Execute_ROP_Boundary(t *testing.T) {
	probe := replenish.RawRow{
		ProductID:     uuid.New(),
		AvailableQty:  decimal.Zero, // irrelevant to ROP/threshold computation
		AvgDailySales: decimal.NewFromFloat(2),
		UnitCost:      decimal.NewFromFloat(5),
		LeadTimeDays:  10,
	}
	threshold := replenish.ReorderPoint(replenish.Forecast(probe, replenish.DefaultWeeks))
	if threshold.IsZero() {
		t.Fatalf("test setup invalid: threshold must be non-zero, got %s", threshold)
	}

	atThreshold := probe
	atThreshold.ProductID = uuid.New()
	atThreshold.AvailableQty = threshold

	belowThreshold := probe
	belowThreshold.ProductID = uuid.New()
	belowThreshold.AvailableQty = threshold.Sub(decimal.NewFromFloat(0.01))

	repo := &seqDigestRepo{}
	repl := &seqSuggestionRepo{rows: []replenish.RawRow{atThreshold, belowThreshold}}

	uc := appdigest.NewWeeklySummaryUseCase(repo, repl)
	s, err := uc.Execute(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if s.ReplenishCount != 1 {
		t.Fatalf("ReplenishCount: want 1 (only belowThreshold counted), got %d", s.ReplenishCount)
	}
	wantAmount := replenish.Forecast(belowThreshold, replenish.DefaultWeeks).EstAmountCNY
	if !s.ReplenishAmountCNY.Equal(wantAmount) {
		t.Errorf("ReplenishAmountCNY: want %s got %s", wantAmount, s.ReplenishAmountCNY)
	}
}

// TestZZ_Execute_ReplenishAmountZero_StillCounted covers the "shortfall
// already covered so EstAmountCNY legitimately comes out to 0" business
// invariant: a SKU below its ROP still increments ReplenishCount even though
// its own contribution to ReplenishAmountCNY is zero.
func TestZZ_Execute_ReplenishAmountZero_StillCounted(t *testing.T) {
	// avgDaily=1, leadTime=30 days pushes ROP (~32.7) comfortably above the
	// 2-week target+safety coverage (~16.7), so an AvailableQty in between
	// (20) is below ROP (counted) yet the suggested-qty formula floors to 0
	// (target+safety-available goes negative), making EstAmountCNY exactly 0.
	raw := replenish.RawRow{
		ProductID:     uuid.New(),
		AvailableQty:  decimal.NewFromFloat(20),
		AvgDailySales: decimal.NewFromFloat(1),
		UnitCost:      decimal.NewFromFloat(9),
		LeadTimeDays:  30,
	}
	f := replenish.Forecast(raw, replenish.DefaultWeeks)
	threshold := replenish.ReorderPoint(f)
	if !f.AvailableQty.LessThan(threshold) {
		t.Fatalf("test setup invalid: AvailableQty %s must be below threshold %s", f.AvailableQty, threshold)
	}
	if !f.EstAmountCNY.Equal(decimal.Zero) {
		t.Fatalf("test setup invalid: expected EstAmountCNY 0, got %s (adjust fixture)", f.EstAmountCNY)
	}

	repo := &seqDigestRepo{}
	repl := &seqSuggestionRepo{rows: []replenish.RawRow{raw}}

	uc := appdigest.NewWeeklySummaryUseCase(repo, repl)
	s, err := uc.Execute(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.ReplenishCount != 1 {
		t.Errorf("ReplenishCount: want 1 (below-ROP SKU still counted), got %d", s.ReplenishCount)
	}
	if !s.ReplenishAmountCNY.Equal(decimal.Zero) {
		t.Errorf("ReplenishAmountCNY: want 0 (covered shortfall), got %s", s.ReplenishAmountCNY)
	}
}

// TestZZ_Execute_GeneratedAt_IsUTCNow verifies GeneratedAt is stamped with a
// non-zero UTC timestamp taken at call time (bounded by a before/after window
// around the Execute call, not read back from itself).
func TestZZ_Execute_GeneratedAt_IsUTCNow(t *testing.T) {
	repo := &seqDigestRepo{}
	repl := &seqSuggestionRepo{}

	uc := appdigest.NewWeeklySummaryUseCase(repo, repl)

	before := time.Now().UTC()
	s, err := uc.Execute(context.Background(), uuid.New())
	after := time.Now().UTC()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if s.GeneratedAt.IsZero() {
		t.Fatal("GeneratedAt must be non-zero")
	}
	if s.GeneratedAt.Location() != time.UTC {
		t.Errorf("GeneratedAt location: want UTC got %s", s.GeneratedAt.Location())
	}
	if s.GeneratedAt.Before(before) || s.GeneratedAt.After(after) {
		t.Errorf("GeneratedAt %s not within [%s, %s]", s.GeneratedAt, before, after)
	}
}

// TestZZ_Execute_MultipleRawsWithMixedFates exercises a mixed batch (3 raws:
// clearly below ROP, at/above ROP, and zero-ROP no-signal) matching the
// business-invariant outline, using a distinct fixture from the sibling
// existing test to hit independent code paths for the loop's `continue`
// branches under -race.
func TestZZ_Execute_MultipleRawsWithMixedFates(t *testing.T) {
	below := zzRawRow(1, 5, 20, 3)    // small ROP but AvailableQty even smaller → counted
	above := zzRawRow(9999, 5, 20, 3) // way above ROP → skipped
	zeroROP := zzRawRow(0, 0, 20, 3)  // no velocity → ROP 0 → skipped regardless of AvailableQty

	repo := &seqDigestRepo{oversellCount: 2, deadCount: 4, scorecard: appdigest.ScorecardCounts{Suggested: 7, Adopted: 3, MissedStockout: 1}}
	repl := &seqSuggestionRepo{rows: []replenish.RawRow{below, above, zeroROP}}

	uc := appdigest.NewWeeklySummaryUseCase(repo, repl)
	s, err := uc.Execute(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if s.ReplenishCount != 1 {
		t.Errorf("ReplenishCount: want 1 got %d", s.ReplenishCount)
	}
	wantAmount := replenish.Forecast(below, replenish.DefaultWeeks).EstAmountCNY
	if !s.ReplenishAmountCNY.Equal(wantAmount) {
		t.Errorf("ReplenishAmountCNY: want %s got %s", wantAmount, s.ReplenishAmountCNY)
	}
	if s.OversellCount != 2 {
		t.Errorf("OversellCount: want 2 got %d", s.OversellCount)
	}
	if s.DeadStockCount != 4 {
		t.Errorf("DeadStockCount: want 4 got %d", s.DeadStockCount)
	}
	if s.Suggested != 7 || s.Adopted != 3 || s.MissedStockout != 1 {
		t.Errorf("scorecard passthrough mismatch: %+v", s)
	}
}
