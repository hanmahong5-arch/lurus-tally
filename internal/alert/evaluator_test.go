package alert_test

import (
	"testing"
	"time"

	"github.com/hanmahong5-arch/lurus-tally/internal/alert"
)

// baseDate is a fixed Monday used as the start of all test streaks.
var baseDate = time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

// days builds a slice of Snapshots with a single signal (name="s") that is
// breached on the days whose index is in `redIndices` and green on others.
// If redIndices is nil every day is red.
func days(n int, redIndices map[int]bool) []alert.Snapshot {
	snaps := make([]alert.Snapshot, n)
	for i := range snaps {
		v := 0.05 // below threshold → breached
		if redIndices != nil {
			if _, isRed := redIndices[i]; !isRed {
				v = 0.50 // above threshold → green
			}
		}
		snaps[i] = alert.Snapshot{
			Date: baseDate.AddDate(0, 0, i),
			Signals: []alert.Signal{
				{Name: "s", Value: v, Threshold: 0.40, Direction: alert.DirectionLT},
			},
		}
	}
	return snaps
}

// TestEvaluate_NoBreachShortStreak confirms that fewer than 14 consecutive
// red days does not produce a breach.
func TestEvaluate_NoBreachShortStreak(t *testing.T) {
	snaps := days(13, nil) // all 13 days red
	got := alert.Evaluate(snaps, alert.DefaultRequiredConsecutiveDays)
	if len(got) != 0 {
		t.Fatalf("expected no breach for 13-day streak, got %v", got)
	}
}

// TestEvaluate_BreachExactly14Days confirms that 14 consecutive red days
// triggers a breach.
func TestEvaluate_BreachExactly14Days(t *testing.T) {
	snaps := days(14, nil)
	got := alert.Evaluate(snaps, alert.DefaultRequiredConsecutiveDays)
	if len(got) != 1 {
		t.Fatalf("expected 1 breach for 14-day streak, got %d", len(got))
	}
	if got[0].ConsecutiveDays != 14 {
		t.Errorf("consecutive_days: want 14, got %d", got[0].ConsecutiveDays)
	}
	if !got[0].FirstRedDate.Equal(baseDate) {
		t.Errorf("first_red_date: want %v, got %v", baseDate, got[0].FirstRedDate)
	}
}

// TestEvaluate_StreakInterruptedByGreen confirms that a single green day
// inside a 14-day window resets the streak, preventing a breach.
func TestEvaluate_StreakInterruptedByGreen(t *testing.T) {
	// Days 0-6 red, day 7 green, days 8-14 red → longest streak = 7.
	redDays := map[int]bool{}
	for i := 0; i < 15; i++ {
		if i != 7 {
			redDays[i] = true
		}
	}
	snaps := days(15, redDays)
	got := alert.Evaluate(snaps, alert.DefaultRequiredConsecutiveDays)
	if len(got) != 0 {
		t.Fatalf("expected no breach when streak interrupted at day 7, got %v", got)
	}
}

// TestEvaluate_MultipleSignals confirms that independent signals are tracked
// separately and only those with 14+ consecutive red days are returned.
func TestEvaluate_MultipleSignals(t *testing.T) {
	n := 16
	snaps := make([]alert.Snapshot, n)
	for i := range snaps {
		// ks1: always breached.
		// ks2: breached only for first 10 days → max streak = 10.
		ks2val := 0.05
		if i >= 10 {
			ks2val = 0.50 // green
		}
		snaps[i] = alert.Snapshot{
			Date: baseDate.AddDate(0, 0, i),
			Signals: []alert.Signal{
				{Name: "ks1", Value: 0.05, Threshold: 0.40, Direction: alert.DirectionLT},
				{Name: "ks2", Value: ks2val, Threshold: 0.40, Direction: alert.DirectionLT},
			},
		}
	}
	got := alert.Evaluate(snaps, alert.DefaultRequiredConsecutiveDays)
	if len(got) != 1 {
		t.Fatalf("expected 1 breach (only ks1), got %v", got)
	}
	if got[0].SignalName != "ks1" {
		t.Errorf("expected ks1, got %s", got[0].SignalName)
	}
}

// TestEvaluate_EmptySnapshotsNoError confirms that an empty input returns
// nil without panicking.
func TestEvaluate_EmptySnapshotsNoError(t *testing.T) {
	got := alert.Evaluate(nil, alert.DefaultRequiredConsecutiveDays)
	if got != nil {
		t.Errorf("expected nil for empty input, got %v", got)
	}
}

// TestEvaluate_DirectionGT verifies that a "gt" direction correctly fires
// when Value exceeds Threshold.
func TestEvaluate_DirectionGT(t *testing.T) {
	snaps := make([]alert.Snapshot, 14)
	for i := range snaps {
		snaps[i] = alert.Snapshot{
			Date: baseDate.AddDate(0, 0, i),
			Signals: []alert.Signal{
				// Threshold 100 means "alert if more than 100 unresolved items".
				{Name: "error_backlog", Value: 150, Threshold: 100, Direction: alert.DirectionGT},
			},
		}
	}
	got := alert.Evaluate(snaps, alert.DefaultRequiredConsecutiveDays)
	if len(got) != 1 {
		t.Fatalf("expected 1 breach for gt-direction, got %v", got)
	}
}
