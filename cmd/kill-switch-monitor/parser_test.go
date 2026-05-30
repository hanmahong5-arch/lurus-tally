package main

import (
	"os"
	"path/filepath"
	"testing"

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
		{"FALSIFIED", 0.0, true},   // case-insensitive
		{" truthy ", 1.0, true},    // trimmed
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

// --- helpers ---

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
