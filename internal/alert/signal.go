// Package alert provides kill-switch signal evaluation and breach notification
// for the Tally V1.5 falsifiable hypothesis tracker.
//
// Three signals are monitored (see CLAUDE.md kill-switch section):
//   - KS1: onboarding completion rate for the first 10 customers (threshold ≥ 40%)
//   - KS2: day-45 AI-suggested PO actual order rate (threshold ≥ 20%)
//   - KS3: 90-day trial-to-paid conversion rate (threshold ≥ 30%)
//
// Any signal staying below its threshold for 14 consecutive days triggers a breach,
// which in turn fires all configured Sender implementations.
package alert

import "time"

// Direction indicates whether a value breach means the metric fell below ("lt")
// or exceeded ("gt") its threshold.
type Direction string

const (
	// DirectionLT marks a breach when Value < Threshold.
	DirectionLT Direction = "lt"
	// DirectionGT marks a breach when Value > Threshold.
	DirectionGT Direction = "gt"
)

// Signal represents a single kill-switch metric observation.
type Signal struct {
	// Name is a short identifier, e.g. "ks1_onboarding_rate".
	Name string
	// Value is the measured value at the time of snapshot, in the range [0, 1]
	// for percentages (0.40 = 40%).
	Value float64
	// Threshold is the boundary that, if crossed in the Direction indicated,
	// marks the day as red.
	Threshold float64
	// Direction is "lt" (red when Value < Threshold) or "gt" (red when Value > Threshold).
	Direction Direction
}

// IsBreached reports whether this Signal's Value crossed its Threshold in the
// configured Direction — i.e. whether today counts as a red day.
func (s Signal) IsBreached() bool {
	switch s.Direction {
	case DirectionLT:
		return s.Value < s.Threshold
	case DirectionGT:
		return s.Value > s.Threshold
	default:
		return false
	}
}

// Snapshot is the full observation record for one calendar day.
type Snapshot struct {
	// Date is the UTC date this observation was recorded.
	Date    time.Time
	Signals []Signal
}
