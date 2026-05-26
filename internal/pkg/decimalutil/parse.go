// Package decimalutil parses decimal strings (typically from PostgreSQL NUMERIC
// columns) with explicit error reporting. Use Parse in repo scan helpers so a
// malformed column surfaces as an error to the caller instead of being silently
// coerced to decimal.Zero.
package decimalutil

import (
	"fmt"

	"github.com/shopspring/decimal"
)

// Parse converts s to a decimal.Decimal. Returns an error tagged with field so
// the caller can identify which column failed. Empty input is treated as zero
// (matches PG NULL → empty scan into *string when COALESCE is used upstream).
func Parse(s string, field string) (decimal.Decimal, error) {
	if s == "" {
		return decimal.Zero, nil
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Zero, fmt.Errorf("decimalutil: parse %s %q: %w", field, s, err)
	}
	return d, nil
}
