package replenish_test

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/app/replenish"
)

// stubScorecardRepo is a test double for ScorecardRepo.
type stubScorecardRepo struct {
	raw        replenish.ScorecardRaw
	err        error
	gotWindow  int
	gotTenant  uuid.UUID
	timesAsked int
}

func (s *stubScorecardRepo) Scorecard(_ context.Context, tenantID uuid.UUID, windowDays int) (replenish.ScorecardRaw, error) {
	s.timesAsked++
	s.gotTenant = tenantID
	s.gotWindow = windowDays
	return s.raw, s.err
}

// TestGetScorecard_Execute_AdoptionRate verifies the rate derivation,
// including the 0/0 → 0 (not NaN) contract.
func TestGetScorecard_Execute_AdoptionRate(t *testing.T) {
	cases := []struct {
		name     string
		raw      replenish.ScorecardRaw
		wantRate float64
	}{
		{"zero_over_zero_is_zero", replenish.ScorecardRaw{SuggestionsCount: 0, AdoptedCount: 0}, 0},
		{"half_adopted", replenish.ScorecardRaw{SuggestionsCount: 8, AdoptedCount: 4, StockoutMisses: 1}, 0.5},
		{"all_adopted", replenish.ScorecardRaw{SuggestionsCount: 3, AdoptedCount: 3}, 1},
		{"none_adopted", replenish.ScorecardRaw{SuggestionsCount: 5, AdoptedCount: 0, StockoutMisses: 2}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &stubScorecardRepo{raw: tc.raw}
			uc := replenish.NewGetScorecardUseCase(repo)

			out, err := uc.Execute(context.Background(), uuid.New())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if math.Abs(out.AdoptionRate-tc.wantRate) > 1e-9 {
				t.Errorf("AdoptionRate = %f, want %f", out.AdoptionRate, tc.wantRate)
			}
			if out.SuggestionsCount != tc.raw.SuggestionsCount {
				t.Errorf("SuggestionsCount = %d, want %d", out.SuggestionsCount, tc.raw.SuggestionsCount)
			}
			if out.AdoptedCount != tc.raw.AdoptedCount {
				t.Errorf("AdoptedCount = %d, want %d", out.AdoptedCount, tc.raw.AdoptedCount)
			}
			if out.StockoutMisses != tc.raw.StockoutMisses {
				t.Errorf("StockoutMisses = %d, want %d", out.StockoutMisses, tc.raw.StockoutMisses)
			}
			// The 28-day window constant must reach the repo and the output.
			if repo.gotWindow != out.WindowDays || out.WindowDays != 28 {
				t.Errorf("window = repo %d / out %d, want 28", repo.gotWindow, out.WindowDays)
			}
		})
	}
}

// TestGetScorecard_Execute_RepoError_Propagated verifies repo failures wrap up.
func TestGetScorecard_Execute_RepoError_Propagated(t *testing.T) {
	repo := &stubScorecardRepo{err: errors.New("db down")}
	uc := replenish.NewGetScorecardUseCase(repo)

	if _, err := uc.Execute(context.Background(), uuid.New()); err == nil {
		t.Fatal("expected error from repo, got nil")
	}
}

// TestGetScorecard_Execute_NilTenant_Rejected verifies the tenant guard.
func TestGetScorecard_Execute_NilTenant_Rejected(t *testing.T) {
	repo := &stubScorecardRepo{}
	uc := replenish.NewGetScorecardUseCase(repo)

	if _, err := uc.Execute(context.Background(), uuid.Nil); err == nil {
		t.Fatal("expected error for nil tenant, got nil")
	}
	if repo.timesAsked != 0 {
		t.Errorf("repo must not be queried for nil tenant, got %d calls", repo.timesAsked)
	}
}
