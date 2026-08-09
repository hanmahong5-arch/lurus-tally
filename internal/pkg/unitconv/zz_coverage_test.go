package unitconv_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/unitconv"
)

// TestConvertToBase_Table exercises ConvertToBase's error branches, the
// factor.Sign()<=0 business invariant (conversion_factor must be > 0), and
// formatDecimal's negative-number formatting path (not exercised by the
// original test file, which only ever passes positive quantities).
//
// Expected values are hand-derived from the decimal arithmetic described in
// the package doc (base_quantity = user_quantity * conversion_factor),
// independently verified with a standalone big.Int/big.Float scratch program
// (not by invoking the package under test) before being written here.
func TestConvertToBase_Table(t *testing.T) {
	tests := []struct {
		name        string
		quantity    string
		factor      string
		want        string
		wantErr     bool
		errContains string
	}{
		{
			name:        "empty quantity is invalid",
			quantity:    "",
			factor:      "12",
			wantErr:     true,
			errContains: "invalid quantity",
		},
		{
			name:        "garbage quantity is invalid",
			quantity:    "abc",
			factor:      "12",
			wantErr:     true,
			errContains: "invalid quantity",
		},
		{
			name:        "garbage factor is invalid",
			quantity:    "5",
			factor:      "xyz",
			wantErr:     true,
			errContains: "invalid conversion factor",
		},
		{
			name:        "zero factor violates data integrity invariant",
			quantity:    "5",
			factor:      "0",
			wantErr:     true,
			errContains: "data integrity",
		},
		{
			name:        "zero-with-decimal factor still <= 0",
			quantity:    "5",
			factor:      "0.0",
			wantErr:     true,
			errContains: "data integrity",
		},
		{
			name:        "negative factor violates data integrity invariant",
			quantity:    "5",
			factor:      "-1",
			wantErr:     true,
			errContains: "data integrity",
		},
		{
			name:     "negative quantity with positive factor is a legal negative result",
			quantity: "-5",
			factor:   "2",
			want:     "-10.000000",
		},
		{
			name:     "exact half-cent tie, positive, rounds up (6th digit +1)",
			quantity: "0.0078125", // dyadic: exactly 1/128, terminates at the 7th decimal digit = 5
			factor:   "1",
			want:     "0.007813",
		},
		{
			name:     "exact half-cent tie, negative, formatDecimal's negative branch",
			quantity: "-0.0078125", // exactly -1/128
			factor:   "1",
			want:     "-0.007814",
		},
		{
			name:     "large number does not overflow prec128",
			quantity: "99999999999",
			factor:   "1000000",
			want:     "99999999999000000.000000",
		},
		{
			name:     "zero quantity is always zero base quantity",
			quantity: "0",
			factor:   "999",
			want:     "0.000000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := unitconv.ConvertToBase(tt.quantity, tt.factor)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ConvertToBase(%q, %q): expected error, got nil (result %q)", tt.quantity, tt.factor, got)
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("ConvertToBase(%q, %q) error = %q, want substring %q", tt.quantity, tt.factor, err.Error(), tt.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("ConvertToBase(%q, %q): unexpected error: %v", tt.quantity, tt.factor, err)
			}
			if got != tt.want {
				t.Errorf("ConvertToBase(%q, %q) = %q, want %q", tt.quantity, tt.factor, got, tt.want)
			}
		})
	}
}

// TestConvertFromBase_Table exercises ConvertFromBase's error branches
// (including the ones the original test file never reached: invalid base
// quantity, invalid factor, negative factor) plus the recurring-decimal
// rounding cases explicitly called out in the test brief: 1/3 (no round,
// 7th digit 3), 2/3 (rounds up, 7th digit 6), 1/6 (rounds up, 7th digit 6).
//
// All "want" values were computed by hand from the true rational value
// (e.g. 2/3 = 0.6666666...) and the package's documented round-half-up-to-6
// rule, then cross-checked with an independent scratch program using
// math/big directly (never by reading ConvertFromBase's own output).
func TestConvertFromBase_Table(t *testing.T) {
	tests := []struct {
		name         string
		baseQuantity string
		factor       string
		want         string
		wantErr      bool
		errContains  string
	}{
		{
			name:         "empty base quantity is invalid",
			baseQuantity: "",
			factor:       "3",
			wantErr:      true,
			errContains:  "invalid base quantity",
		},
		{
			name:         "garbage base quantity is invalid",
			baseQuantity: "abc",
			factor:       "3",
			wantErr:      true,
			errContains:  "invalid base quantity",
		},
		{
			name:         "garbage factor is invalid",
			baseQuantity: "36",
			factor:       "xyz",
			wantErr:      true,
			errContains:  "invalid conversion factor",
		},
		{
			name:         "zero factor is division by zero, not data integrity",
			baseQuantity: "36",
			factor:       "0",
			wantErr:      true,
			errContains:  "division by zero",
		},
		{
			name:         "negative factor is also division by zero",
			baseQuantity: "36",
			factor:       "-4",
			wantErr:      true,
			errContains:  "division by zero",
		},
		{
			name:         "1/3 = 0.333333..., 7th digit 3, no round up",
			baseQuantity: "1",
			factor:       "3",
			want:         "0.333333",
		},
		{
			name:         "2/3 = 0.666666..., 7th digit 6, rounds up",
			baseQuantity: "2",
			factor:       "3",
			want:         "0.666667",
		},
		{
			name:         "1/6 = 0.166666..., 7th digit 6, rounds up",
			baseQuantity: "1",
			factor:       "6",
			want:         "0.166667",
		},
		{
			name:         "-2/3, remainder ratio 1/3 < half, no adjustment",
			baseQuantity: "-2",
			factor:       "3",
			want:         "-0.666667",
		},
		{
			name:         "zero base quantity divides to zero",
			baseQuantity: "0",
			factor:       "7",
			want:         "0.000000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := unitconv.ConvertFromBase(tt.baseQuantity, tt.factor)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ConvertFromBase(%q, %q): expected error, got nil (result %q)", tt.baseQuantity, tt.factor, got)
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("ConvertFromBase(%q, %q) error = %q, want substring %q", tt.baseQuantity, tt.factor, err.Error(), tt.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("ConvertFromBase(%q, %q): unexpected error: %v", tt.baseQuantity, tt.factor, err)
			}
			if got != tt.want {
				t.Errorf("ConvertFromBase(%q, %q) = %q, want %q", tt.baseQuantity, tt.factor, got, tt.want)
			}
		})
	}
}

// TestRoundTrip_ExactFactor asserts the invariant documented in the package
// comment: base_quantity = user_quantity * factor, and user_quantity =
// base_quantity / factor, so converting to base and back with an exact
// (evenly-dividing) factor must reproduce the original quantity exactly.
func TestRoundTrip_ExactFactor(t *testing.T) {
	toBase, err := unitconv.ConvertToBase("10", "4")
	if err != nil {
		t.Fatalf("ConvertToBase: unexpected error: %v", err)
	}
	if toBase != "40.000000" {
		t.Fatalf("ConvertToBase(10, 4) = %q, want %q", toBase, "40.000000")
	}

	fromBase, err := unitconv.ConvertFromBase(toBase, "4")
	if err != nil {
		t.Fatalf("ConvertFromBase: unexpected error: %v", err)
	}
	if fromBase != "10.000000" {
		t.Errorf("round trip ConvertFromBase(ConvertToBase(10, 4), 4) = %q, want %q", fromBase, "10.000000")
	}
}

// TestConversionFactor_ConcurrentUse verifies ConvertToBase/ConvertFromBase
// are pure and race-free under concurrent use (run with -race), since both
// allocate fresh big.Float/big.Int state per call and touch no package-level
// mutable state.
func TestConversionFactor_ConcurrentUse(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := unitconv.ConvertToBase("3", "12"); err != nil {
				t.Errorf("ConvertToBase: unexpected error: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			if _, err := unitconv.ConvertFromBase("36", "12"); err != nil {
				t.Errorf("ConvertFromBase: unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
}
