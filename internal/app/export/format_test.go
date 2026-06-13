package export

import (
	"database/sql"
	"testing"
	"time"
)

// TestStatusLabel_Mapping covers the bill-status → Chinese-label mapping,
// including the numeric fallback for unknown codes (the branch most likely to
// silently mislabel a new status the DB starts emitting).
func TestStatusLabel_Mapping(t *testing.T) {
	cases := []struct {
		in   int16
		want string
	}{
		{0, "草稿"},
		{2, "已审核"},
		{9, "已取消"},
		{1, "1"},     // unknown → numeric fallback
		{7, "7"},     // unknown → numeric fallback
		{-1, "-1"},   // negative still renders, not panics
		{255, "255"}, // upper int16 range
	}
	for _, c := range cases {
		if got := statusLabel(c.in); got != c.want {
			t.Errorf("statusLabel(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestFormatNullDate covers NULL (→ empty) vs valid (→ layout-formatted) for both
// the date-only and datetime layouts used by the bill and payment exports.
func TestFormatNullDate(t *testing.T) {
	ts := time.Date(2026, 6, 13, 14, 30, 5, 0, time.UTC)

	if got := formatNullDate(sql.NullTime{Valid: false}, dateLayout); got != "" {
		t.Errorf("NULL date: got %q, want empty string", got)
	}
	if got := formatNullDate(sql.NullTime{Valid: false}, dateTimeLayout); got != "" {
		t.Errorf("NULL datetime: got %q, want empty string", got)
	}
	if got := formatNullDate(sql.NullTime{Time: ts, Valid: true}, dateLayout); got != "2026-06-13" {
		t.Errorf("valid date: got %q, want 2026-06-13", got)
	}
	if got := formatNullDate(sql.NullTime{Time: ts, Valid: true}, dateTimeLayout); got != "2026-06-13 14:30:05" {
		t.Errorf("valid datetime: got %q, want 2026-06-13 14:30:05", got)
	}
}

// TestWithRowLimit covers the option resolver: a positive value overrides the
// default, while zero / negative values are ignored so a misconfigured caller
// can't silently disable the cap.
func TestWithRowLimit(t *testing.T) {
	const def = 50_000

	if o := resolve(def, nil); o.rowLimit != def {
		t.Errorf("no opts: rowLimit = %d, want %d", o.rowLimit, def)
	}
	if o := resolve(def, []Option{WithRowLimit(2)}); o.rowLimit != 2 {
		t.Errorf("WithRowLimit(2): rowLimit = %d, want 2", o.rowLimit)
	}
	if o := resolve(def, []Option{WithRowLimit(0)}); o.rowLimit != def {
		t.Errorf("WithRowLimit(0): rowLimit = %d, want %d (ignored)", o.rowLimit, def)
	}
	if o := resolve(def, []Option{WithRowLimit(-5)}); o.rowLimit != def {
		t.Errorf("WithRowLimit(-5): rowLimit = %d, want %d (ignored)", o.rowLimit, def)
	}
}
