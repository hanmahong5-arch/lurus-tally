package alert

import (
	"sort"
	"time"
)

const (
	// DefaultRequiredConsecutiveDays is the consecutive red-day count that triggers
	// a pivot meeting — two full weeks per the kill-switch contract.
	DefaultRequiredConsecutiveDays = 14
)

// Breach records that a named signal has been red for consecutiveDays in a row,
// starting at FirstRedDate.
type Breach struct {
	// SignalName matches Signal.Name.
	SignalName string
	// ConsecutiveDays is how many consecutive days the signal was breached.
	ConsecutiveDays int
	// FirstRedDate is the date the current consecutive red streak began.
	FirstRedDate time.Time
}

// Evaluate examines a history of Snapshots and returns every signal that has
// been continuously breached for at least requiredConsecutiveDays days.
//
// Snapshots need not be sorted; the function orders them by Date internally.
// Missing signal names on some days do not reset a streak — only an explicit
// non-breached reading resets it.
//
// Pass DefaultRequiredConsecutiveDays (14) for the standard two-week window.
func Evaluate(snapshots []Snapshot, requiredConsecutiveDays int) []Breach {
	if len(snapshots) == 0 || requiredConsecutiveDays <= 0 {
		return nil
	}

	// Sort ascending by date so streaks accumulate in chronological order.
	sorted := make([]Snapshot, len(snapshots))
	copy(sorted, snapshots)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Date.Before(sorted[j].Date)
	})

	// streak tracks, per signal name: (current consecutive red days, first red date).
	type streakEntry struct {
		count     int
		firstDate time.Time
	}
	streaks := map[string]*streakEntry{}

	for _, snap := range sorted {
		// Track which signal names appeared in this snapshot.
		seen := map[string]bool{}
		for _, sig := range snap.Signals {
			seen[sig.Name] = true
			entry, ok := streaks[sig.Name]
			if !ok {
				entry = &streakEntry{}
				streaks[sig.Name] = entry
			}
			if sig.IsBreached() {
				if entry.count == 0 {
					entry.firstDate = snap.Date
				}
				entry.count++
			} else {
				// Explicit green reading resets the streak.
				entry.count = 0
				entry.firstDate = time.Time{}
			}
		}
		// Signals absent from this snapshot are left untouched (no reset).
		_ = seen
	}

	var breaches []Breach
	for name, entry := range streaks {
		if entry.count >= requiredConsecutiveDays {
			breaches = append(breaches, Breach{
				SignalName:      name,
				ConsecutiveDays: entry.count,
				FirstRedDate:    entry.firstDate,
			})
		}
	}

	// Deterministic output order for tests and log readability.
	sort.Slice(breaches, func(i, j int) bool {
		return breaches[i].SignalName < breaches[j].SignalName
	})
	return breaches
}
