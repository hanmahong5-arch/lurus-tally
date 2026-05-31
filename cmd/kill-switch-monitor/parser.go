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
// signal snapshots from the "状态历史" table.
//
// The table header (assumptions.md § 状态历史) declares 11 columns:
//
//	| 日期 | H1 status | H1 current_value | H2 status | H2 current_value |
//	  H3 status | H3 current_value | KS1 status | KS1 value | KS2 status | KS2 value |
//
// Column index (0-based) → field:
//
//	[0] date  [1] H1 status  [3] H2 status  [5] H3 status
//	[7] KS1 status  [9] KS2 status
//
// The H1/H2/H3 statuses are PRODUCT-hypothesis falsification signals and are
// distinct from the KS1/KS2/KS3 kill switches. The kill-switch monitor must
// read the dedicated KS columns ([7]/[9]) written by bin/assumption-snapshot.sh,
// NOT the H1/H2/H3 columns. KS3 (trial→paid conversion) has no column in this
// table yet — it lives in lurus-platform subscriptions and is intentionally
// omitted (see bin/assumption-snapshot.sh KS3 note), so this parser does not
// fabricate a ks3 signal from H3.
//
// Older rows predate the KS columns and carry only 7 cells; for those rows the
// KS signals are simply absent (no reading), which is the honest representation.
//
// Per-status mapping is delegated to statusToValue:
//
//	"truthy"    → real reading 1.0 (green)
//	"falsified" → real reading 0.0 (breach)
//	"inconclusive" / "pending" / "n/a" / "—" / unknown → NO reading
//
// Signals with no reading are NOT appended to the snapshot. The evaluator
// treats absent signals as neither red nor green (no streak start, no reset),
// which matches the assumptions.md protocol: inconclusive is only treated as
// falsified after a human-reviewed 1-sprint extension, not immediately by the
// daily monitor.
//
// Returns an empty slice (not an error) when the file exists but contains
// no parseable rows. An empty result (or an open/scan error) puts the caller
// into the DATA-UNAVAILABLE degraded path unless KILLSWITCH_ALLOW_MOCK is set;
// it never silently synthesizes a breach verdict.
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
	)

	// Column indices in the 状态历史 table (see doc comment above).
	const (
		colDate = 0
		colKS1  = 7 // "KS1 status"
		colKS2  = 9 // "KS2 status"
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

		// Data row: | 2026-05-18 | pending | — | ... | KS1 status | KS1 val | KS2 status | KS2 val |
		cols := splitTableRow(line)
		// Need at least the date column to identify a data row; KS columns
		// may be absent on older (7-cell) rows and are handled per-signal.
		if len(cols) <= colDate {
			continue
		}

		dateStr := strings.TrimSpace(cols[colDate])
		date, err := time.Parse(time.DateOnly, dateStr)
		if err != nil {
			continue // not a data row
		}

		// Read the dedicated KS columns. A signal is appended only when its
		// column exists AND statusToValue reports a real reading; otherwise it
		// is left absent (no breach, no streak reset).
		var signals []alert.Signal
		if v, ok := statusAt(cols, colKS1); ok {
			signals = append(signals, alert.Signal{
				Name:      "ks1_onboarding_rate",
				Value:     v,
				Threshold: thresholdKS1,
				Direction: alert.DirectionLT,
			})
		}
		if v, ok := statusAt(cols, colKS2); ok {
			signals = append(signals, alert.Signal{
				Name:      "ks2_ai_po_order_rate",
				Value:     v,
				Threshold: thresholdKS2,
				Direction: alert.DirectionLT,
			})
		}
		// ks3_trial_conversion is intentionally not derived here: it has no
		// column in this table (see doc comment). Emitting it would fabricate
		// a perpetual breach from missing data.

		snapshots = append(snapshots, alert.Snapshot{Date: date, Signals: signals})
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

// statusAt reads cols[idx] (if present) and maps it through statusToValue.
// Returns ok=false when the column is out of range or the status carries no
// real reading.
func statusAt(cols []string, idx int) (float64, bool) {
	if idx >= len(cols) {
		return 0, false
	}
	return statusToValue(strings.TrimSpace(cols[idx]))
}

// statusToValue converts an assumptions.md status string to a numeric signal
// value for breach detection. The second return value reports whether the
// status represents a *real reading*; a false value means "no data — skip
// this signal for this day" (the evaluator then treats it as absent, neither
// red nor green).
//
// Mapping rationale (see assumptions.md § 评分约定):
//   - "truthy"      → (1.0, true)   green: hypothesis confirmed for this period
//   - "falsified"   → (0.0, true)   red: hypothesis falsified → counts as breach
//   - "inconclusive"→ (_, false)    NOT yet falsified — protocol says it only
//     becomes falsified after a human-reviewed 1-sprint extension, so the
//     daily monitor must not treat it as a breach.
//   - "pending"     → (_, false)    no data yet — not a breach.
//   - "n/a" / "—"   → (_, false)    no data — not a breach.
//   - anything else → (_, false)    unknown status — fail safe to no-reading
//     rather than fabricating a breach.
func statusToValue(status string) (float64, bool) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "truthy":
		return 1.0, true
	case "falsified":
		return 0.0, true
	default:
		// inconclusive, pending, n/a, —, empty, or anything unknown: no reading.
		return 0, false
	}
}

// mockSnapshots returns synthetic Snapshot data so the CLI can exercise the
// full alert pipeline during a local dry-run. All signals are set to 0 (breach)
// for 14 consecutive days. This is data with no real-world meaning, so it is
// ONLY used when KILLSWITCH_ALLOW_MOCK=true is explicitly set (see mockAllowed);
// by default a missing/unparseable file exits DATA-UNAVAILABLE instead, so
// absent input never fabricates a breach verdict in production.
func mockSnapshots() []alert.Snapshot {
	base := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -13)
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
