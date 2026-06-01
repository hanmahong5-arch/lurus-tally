package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hanmahong5-arch/lurus-tally/internal/alert"
)

// TestStatusToValue_SkipSentinel locks in the S7-3 fix: only "truthy" and
// "falsified" are real readings; inconclusive / pending / n/a / — / unknown
// must NOT be treated as a breach (ok=false), so the daily monitor does not
// fire a pivot alert on missing data. This asserts the pure mapping function
// directly — it does not re-derive the values from the parser code path.
func TestStatusToValue_SkipSentinel(t *testing.T) {
	tests := []struct {
		status   string
		wantVal  float64
		wantReal bool // true = a real reading; false = skip (no breach)
	}{
		{"truthy", 1.0, true},
		{"falsified", 0.0, true},
		{"inconclusive", 0, false},
		{"pending", 0, false},
		{"n/a", 0, false},
		{"—", 0, false},
		{"", 0, false},
		{"FALSIFIED", 0.0, true},     // case-insensitive
		{" truthy ", 1.0, true},      // trimmed
		{"garbage_status", 0, false}, // unknown → fail safe to no-reading
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			gotVal, gotReal := statusToValue(tt.status)
			if gotReal != tt.wantReal {
				t.Fatalf("statusToValue(%q) real = %v, want %v", tt.status, gotReal, tt.wantReal)
			}
			if gotReal && gotVal != tt.wantVal {
				t.Errorf("statusToValue(%q) val = %v, want %v", tt.status, gotVal, tt.wantVal)
			}
		})
	}
}

// TestStatusToValue_InconclusiveIsNotBreach is the explicit anti-regression
// for the original bug: "inconclusive" used to map to 0.0 (a breach). It must
// now be a non-reading, and even if it produced a value, that value must not
// register as breached under the LT threshold semantics.
func TestStatusToValue_InconclusiveIsNotBreach(t *testing.T) {
	_, real := statusToValue("inconclusive")
	if real {
		t.Fatal("inconclusive must NOT be a real reading (must not count as breach)")
	}
	_, real = statusToValue("n-a") // hyphen variant — still no reading
	if real {
		t.Fatal("n-a must NOT be a real reading")
	}
}

// TestParseAssumptions_ReadsDedicatedKSColumns locks in the S7-4 fix: the
// parser must read KS1 from col[7] and KS2 from col[9] (the dedicated kill-
// switch columns), NOT from the H1/H2/H3 hypothesis columns [1]/[3]/[5].
//
// The fixture deliberately sets H1/H2/H3 to "truthy" but KS1/KS2 to
// "falsified": if the parser still read [1]/[3]/[5] it would report green;
// reading the correct [7]/[9] yields the breach values. This makes the
// assertion fail loudly on a column-mapping regression.
func TestParseAssumptions_ReadsDedicatedKSColumns(t *testing.T) {
	const md = `# fixture

## 状态历史

| 日期 | H1 status | H1 current_value | H2 status | H2 current_value | H3 status | H3 current_value | KS1 status | KS1 value | KS2 status | KS2 value |
|---|---|---|---|---|---|---|---|---|---|---|
| 2026-05-30 | truthy | 1 | truthy | 1 | truthy | 1 | falsified | 0.05 | falsified | 0.10 |
`
	path := writeFixture(t, md)
	snaps, err := parseAssumptions(path)
	if err != nil {
		t.Fatalf("parseAssumptions: %v", err)
	}
	if len(snaps) != 1 {
		t.Fatalf("got %d snapshots, want 1", len(snaps))
	}
	got := signalValues(snaps[0])

	// KS1/KS2 must come from the falsified KS columns (value 0.0 = breach),
	// NOT the truthy H columns (which would be 1.0).
	if v, ok := got["ks1_onboarding_rate"]; !ok || v != 0.0 {
		t.Errorf("ks1_onboarding_rate = %v (present=%v), want 0.0 from col[7]=falsified", v, ok)
	}
	if v, ok := got["ks2_ai_po_order_rate"]; !ok || v != 0.0 {
		t.Errorf("ks2_ai_po_order_rate = %v (present=%v), want 0.0 from col[9]=falsified", v, ok)
	}
	// ks3 must not be fabricated.
	if _, ok := got["ks3_trial_conversion"]; ok {
		t.Errorf("ks3_trial_conversion must not be present (no column in table)")
	}
}

// TestParseAssumptions_InconclusiveRowEmitsNoSignals verifies that the
// historical inconclusive/n-a rows (the current real data in assumptions.md)
// do NOT produce breach signals — they emit no readings at all.
func TestParseAssumptions_InconclusiveRowEmitsNoSignals(t *testing.T) {
	const md = `## 状态历史

| 日期 | H1 status | H1 current_value | H2 status | H2 current_value | H3 status | H3 current_value | KS1 status | KS1 value | KS2 status | KS2 value |
|---|---|---|---|---|---|---|---|---|---|---|
| 2026-05-30 | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a | inconclusive | n/a |
`
	path := writeFixture(t, md)
	snaps, err := parseAssumptions(path)
	if err != nil {
		t.Fatalf("parseAssumptions: %v", err)
	}
	if len(snaps) != 1 {
		t.Fatalf("got %d snapshots, want 1", len(snaps))
	}
	if n := len(snaps[0].Signals); n != 0 {
		t.Errorf("inconclusive row produced %d signals, want 0 (no breach from missing data)", n)
	}
	// And the evaluator must report no breaches for such a history.
	if b := alert.Evaluate(snaps, 1); len(b) != 0 {
		t.Errorf("Evaluate reported %d breaches for an all-inconclusive history, want 0", len(b))
	}
}

// TestParseAssumptions_LegacySevenColumnRow verifies backward-compatibility:
// older rows with only 7 cells (no KS columns) are still parsed as data rows
// (valid date) but emit no KS signals rather than panicking on out-of-range.
func TestParseAssumptions_LegacySevenColumnRow(t *testing.T) {
	const md = `## 状态历史

| 日期 | H1 status | H1 current_value | H2 status | H2 current_value | H3 status | H3 current_value | KS1 status | KS1 value | KS2 status | KS2 value |
|---|---|---|---|---|---|---|---|---|---|---|
| 2026-05-18 | pending | — | pending | — | pending | — |
`
	path := writeFixture(t, md)
	snaps, err := parseAssumptions(path)
	if err != nil {
		t.Fatalf("parseAssumptions: %v", err)
	}
	if len(snaps) != 1 {
		t.Fatalf("got %d snapshots, want 1", len(snaps))
	}
	if n := len(snaps[0].Signals); n != 0 {
		t.Errorf("legacy 7-col row produced %d signals, want 0", n)
	}
}

// TestParseAssumptions_FalsifiedKSBreaches confirms a falsified KS column
// produces a breach value that the evaluator counts as red.
func TestParseAssumptions_FalsifiedKSBreaches(t *testing.T) {
	const md = `## 状态历史

| 日期 | H1 status | H1 current_value | H2 status | H2 current_value | H3 status | H3 current_value | KS1 status | KS1 value | KS2 status | KS2 value |
|---|---|---|---|---|---|---|---|---|---|---|
| 2026-05-29 | pending | — | pending | — | pending | — | falsified | 0.05 | truthy | 0.50 |
`
	path := writeFixture(t, md)
	snaps, err := parseAssumptions(path)
	if err != nil {
		t.Fatalf("parseAssumptions: %v", err)
	}
	breaches := alert.Evaluate(snaps, 1)
	var ks1Breached bool
	for _, b := range breaches {
		if b.SignalName == "ks1_onboarding_rate" {
			ks1Breached = true
		}
		if b.SignalName == "ks2_ai_po_order_rate" {
			t.Errorf("ks2 (truthy) must not breach, got breach %+v", b)
		}
	}
	if !ks1Breached {
		t.Errorf("ks1 (falsified) should breach with requiredConsecutiveDays=1")
	}
}

// TestResolveSnapshots_MissingInputDegradesNotBreach locks in the P2 fix: when
// the assumptions file is missing/unparseable (parse error) or parses to zero
// rows, the monitor must report DEGRADED — it must NOT substitute the synthetic
// 14-day all-breach mock data, which would fire a false-positive pivot alert.
// Mock is only used when explicitly opted into via KILLSWITCH_ALLOW_MOCK.
func TestResolveSnapshots_MissingInputDegradesNotBreach(t *testing.T) {
	realRow := alert.Snapshot{
		Date: mustDate(t, "2026-05-30"),
		Signals: []alert.Signal{
			{Name: "ks1_onboarding_rate", Value: 0.05, Threshold: 0.40, Direction: alert.DirectionLT},
		},
	}

	tests := []struct {
		name         string
		snapshots    []alert.Snapshot
		parseErr     error
		allowMock    bool
		wantDegraded bool
		wantMock     bool // true = expect synthetic all-breach mock data
	}{
		{
			name:         "parse error, mock off -> degraded, no fabricated breach",
			snapshots:    nil,
			parseErr:     errInput,
			allowMock:    false,
			wantDegraded: true,
			wantMock:     false,
		},
		{
			name:         "empty parse, mock off -> degraded, no fabricated breach",
			snapshots:    []alert.Snapshot{},
			parseErr:     nil,
			allowMock:    false,
			wantDegraded: true,
			wantMock:     false,
		},
		{
			name:         "parse error, mock explicitly on -> mock data, not degraded",
			snapshots:    nil,
			parseErr:     errInput,
			allowMock:    true,
			wantDegraded: false,
			wantMock:     true,
		},
		{
			name:         "real snapshots present -> evaluate them, never mock",
			snapshots:    []alert.Snapshot{realRow},
			parseErr:     nil,
			allowMock:    false,
			wantDegraded: false,
			wantMock:     false,
		},
		{
			name:         "real snapshots present, mock on -> still real data, no override",
			snapshots:    []alert.Snapshot{realRow},
			parseErr:     nil,
			allowMock:    true,
			wantDegraded: false,
			wantMock:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, degraded := resolveSnapshots(tt.snapshots, tt.parseErr, tt.allowMock)
			if degraded != tt.wantDegraded {
				t.Fatalf("degraded = %v, want %v", degraded, tt.wantDegraded)
			}
			if tt.wantDegraded {
				// Degraded path must NOT have synthesized any breach verdict.
				if b := alert.Evaluate(got, 1); len(b) != 0 {
					t.Fatalf("degraded result fabricated %d breaches, want 0", len(b))
				}
			}
			if got2 := isMockData(got); got2 != tt.wantMock {
				t.Errorf("isMockData = %v, want %v (len=%d)", got2, tt.wantMock, len(got))
			}
		})
	}
}

// TestResolveSnapshots_EndToEnd_MissingFileDegrades drives the decision through
// the real parser code path: a nonexistent file yields an open error, and the
// monitor must degrade rather than fabricate a 14-day all-breach verdict.
func TestResolveSnapshots_EndToEnd_MissingFileDegrades(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist.md")
	snaps, err := parseAssumptions(missing)
	if err == nil {
		t.Fatalf("parseAssumptions(%q) returned nil error, want open error", missing)
	}
	got, degraded := resolveSnapshots(snaps, err, false /* allowMock */)
	if !degraded {
		t.Fatal("missing file must degrade the monitor, not fall back to mock data")
	}
	if b := alert.Evaluate(got, 1); len(b) != 0 {
		t.Errorf("missing file produced %d breaches, want 0 (no fabricated verdict)", len(b))
	}
}

// TestResolveSnapshots_EmptyTableDegrades verifies a file that exists but has no
// parseable data rows (e.g. only inconclusive history removed, header only) also
// degrades rather than fabricating a breach.
func TestResolveSnapshots_EmptyTableDegrades(t *testing.T) {
	const md = `## 状态历史

| 日期 | H1 status |
|---|---|
`
	path := writeFixture(t, md)
	snaps, err := parseAssumptions(path)
	if err != nil {
		t.Fatalf("parseAssumptions: %v", err)
	}
	if len(snaps) != 0 {
		t.Fatalf("header-only table produced %d snapshots, want 0", len(snaps))
	}
	_, degraded := resolveSnapshots(snaps, err, false)
	if !degraded {
		t.Fatal("empty parse must degrade the monitor, not fabricate a breach verdict")
	}
}

// --- helpers ---

// errInput is a sentinel parse error for resolveSnapshots table tests.
var errInput = os.ErrNotExist

// isMockData reports whether s looks like the synthetic mockSnapshots output:
// 14 rows that each carry the ks3_trial_conversion signal (which the real
// parser never emits — see parser.go doc comment), all breached.
func isMockData(s []alert.Snapshot) bool {
	if len(s) != 14 {
		return false
	}
	for _, snap := range s {
		hasKS3 := false
		for _, sig := range snap.Signals {
			if sig.Name == "ks3_trial_conversion" {
				hasKS3 = true
			}
		}
		if !hasKS3 {
			return false
		}
	}
	return true
}

func mustDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		t.Fatalf("parse date %q: %v", s, err)
	}
	return d
}

func writeFixture(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "assumptions.md")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func signalValues(s alert.Snapshot) map[string]float64 {
	m := map[string]float64{}
	for _, sig := range s.Signals {
		m[sig.Name] = sig.Value
	}
	return m
}
