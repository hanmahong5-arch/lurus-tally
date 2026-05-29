package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/hanmahong5-arch/lurus-tally/internal/alert"
)

// parseAssumptions reads the assumptions.md file and extracts kill-switch
// signal snapshots from the status history table.
//
// The expected table format (from assumptions.md):
//
//	| 日期       | H1 status | H1 current_value | H2 status | ... |
//	|---|---|---|---|---|
//	| 2026-05-18 | pending   | —                | pending   | ... |
//
// Column mapping → Signal:
//
//	H1 status → ks1_onboarding_rate    (threshold 0.40, lt)
//	H2 status → ks2_ai_po_order_rate   (threshold 0.20, lt)
//	H3 status → ks3_trial_conversion   (threshold 0.30, lt)
//
// A status value of "falsified" is treated as a breach (value = 0).
// "truthy" is treated as green (value = 1). "pending" / "inconclusive" /
// "—" / "n/a" yield 0 (breach) because they indicate data is not yet
// confirming the hypothesis.
//
// Returns an empty slice (not an error) when the file exists but contains
// no parseable rows — the caller falls back to mock data with a WARN log.
func parseAssumptions(path string) ([]alert.Snapshot, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	// Kill-switch thresholds (matching CLAUDE.md definitions).
	const (
		thresholdKS1 = 0.40
		thresholdKS2 = 0.20
		thresholdKS3 = 0.30
	)

	var snapshots []alert.Snapshot
	inTable := false
	headerParsed := false

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if !strings.HasPrefix(line, "|") {
			if inTable {
				// Table ended.
				inTable = false
				headerParsed = false
			}
			continue
		}

		// Detect the separator line (| --- | --- |).
		if strings.Contains(line, "---") {
			inTable = true
			headerParsed = true
			continue
		}

		if !inTable {
			// First | line of the table — header row.
			inTable = true
			continue
		}

		if !headerParsed {
			continue
		}

		// Data row: | 2026-05-18 | pending | — | pending | — | pending | — |
		cols := splitTableRow(line)
		if len(cols) < 7 {
			continue
		}

		dateStr := strings.TrimSpace(cols[0])
		date, err := time.Parse(time.DateOnly, dateStr)
		if err != nil {
			continue // not a data row
		}

		h1Status := strings.TrimSpace(cols[1])
		h2Status := strings.TrimSpace(cols[3])
		h3Status := strings.TrimSpace(cols[5])

		snap := alert.Snapshot{
			Date: date,
			Signals: []alert.Signal{
				{
					Name:      "ks1_onboarding_rate",
					Value:     statusToValue(h1Status),
					Threshold: thresholdKS1,
					Direction: alert.DirectionLT,
				},
				{
					Name:      "ks2_ai_po_order_rate",
					Value:     statusToValue(h2Status),
					Threshold: thresholdKS2,
					Direction: alert.DirectionLT,
				},
				{
					Name:      "ks3_trial_conversion",
					Value:     statusToValue(h3Status),
					Threshold: thresholdKS3,
					Direction: alert.DirectionLT,
				},
			},
		}
		snapshots = append(snapshots, snap)
	}

	if err := scanner.Err(); err != nil {
		return snapshots, fmt.Errorf("scan %s: %w", path, err)
	}
	return snapshots, nil
}

// splitTableRow splits a markdown table row by "|" and trims empty leading/
// trailing cells produced by the surrounding pipes.
func splitTableRow(line string) []string {
	parts := strings.Split(line, "|")
	// The first and last parts are empty because the line starts/ends with "|".
	if len(parts) >= 2 {
		parts = parts[1 : len(parts)-1]
	}
	return parts
}

// statusToValue converts an assumptions.md status string to a numeric signal
// value for breach detection.
//
// Mapping rationale:
//   - "truthy"      → 1.0  (green: hypothesis confirmed for this period)
//   - "falsified"   → 0.0  (red: hypothesis falsified)
//   - "pending"     → 0.0  (treated as breach: no data yet, assume worst-case
//     until evidence arrives)
//   - "inconclusive"→ 0.0  (per evaluation protocol: inconclusive extended
//     1 sprint is then treated as falsified)
//   - "—" / "n/a"  → 0.0  (no data — conservative default)
func statusToValue(status string) float64 {
	switch strings.ToLower(status) {
	case "truthy":
		return 1.0
	default:
		// falsified, pending, inconclusive, —, n/a, or anything unknown.
		return 0.0
	}
}

// mockSnapshots returns synthetic Snapshot data so the CLI can complete a
// dry-run when the assumptions file is absent or unreadable.
// All signals are set to 0 (breach) for 14 consecutive days, exercising the
// full alert pipeline without any real data dependency.
func mockSnapshots() []alert.Snapshot {
	base := time.Now().UTC().Truncate(24 * time.Hour).AddDate(0, 0, -13)
	snaps := make([]alert.Snapshot, 14)
	for i := range snaps {
		snaps[i] = alert.Snapshot{
			Date: base.AddDate(0, 0, i),
			Signals: []alert.Signal{
				{Name: "ks1_onboarding_rate", Value: 0, Threshold: 0.40, Direction: alert.DirectionLT},
				{Name: "ks2_ai_po_order_rate", Value: 0, Threshold: 0.20, Direction: alert.DirectionLT},
				{Name: "ks3_trial_conversion", Value: 0, Threshold: 0.30, Direction: alert.DirectionLT},
			},
		}
	}
	return snaps
}
