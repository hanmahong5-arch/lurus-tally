package unitconv_test

import (
	"testing"

	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/unitconv"
)

// TestConvertToBase_NormalConversion verifies that multiplying by a conversion factor
// produces a result rounded to 6 decimal places.
func TestConvertToBase_NormalConversion(t *testing.T) {
	// 3 boxes, 1 box = 12 pcs → 36 pcs (base)
	got, err := unitconv.ConvertToBase("3", "12")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "36.000000"
	if got != want {
		t.Errorf("ConvertToBase(3, 12) = %q, want %q", got, want)
	}
}

// TestConvertToBase_FractionalFactor verifies precision to 6 decimal places.
func TestConvertToBase_FractionalFactor(t *testing.T) {
	// 1 unit, factor = 0.001 (e.g. 1 g in terms of kg)
	got, err := unitconv.ConvertToBase("1", "0.001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "0.001000"
	if got != want {
		t.Errorf("ConvertToBase(1, 0.001) = %q, want %q", got, want)
	}
}

// TestConvertToBase_ZeroQuantity returns "0.000000" — zero quantity in any unit is zero base.
func TestConvertToBase_ZeroQuantity(t *testing.T) {
	got, err := unitconv.ConvertToBase("0", "12")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "0.000000"
	if got != want {
		t.Errorf("ConvertToBase(0, 12) = %q, want %q", got, want)
	}
}

// TestConvertToBase_ZeroFactor returns an error — zero factor is a division/domain error.
func TestConvertToBase_ZeroFactor(t *testing.T) {
	_, err := unitconv.ConvertToBase("5", "0")
	if err == nil {
		t.Fatal("expected error for zero conversion factor, got nil")
	}
}

// TestConvertToBase_NegativeFactor returns an error.
func TestConvertToBase_NegativeFactor(t *testing.T) {
	_, err := unitconv.ConvertToBase("5", "-1")
	if err == nil {
		t.Fatal("expected error for negative conversion factor, got nil")
	}
}

// TestConvertFromBase_NormalConversion verifies dividing base quantity by factor.
func TestConvertFromBase_NormalConversion(t *testing.T) {
	// 36 pcs ÷ 12 = 3 boxes
	got, err := unitconv.ConvertFromBase("36", "12")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "3.000000"
	if got != want {
		t.Errorf("ConvertFromBase(36, 12) = %q, want %q", got, want)
	}
}

// TestConvertFromBase_ZeroFactor returns an error (division by zero).
func TestConvertFromBase_ZeroFactor(t *testing.T) {
	_, err := unitconv.ConvertFromBase("36", "0")
	if err == nil {
		t.Fatal("expected error for zero conversion factor, got nil")
	}
}

// TestConvertToBase_InvalidQuantity returns an error for non-numeric input.
func TestConvertToBase_InvalidQuantity(t *testing.T) {
	_, err := unitconv.ConvertToBase("abc", "12")
	if err == nil {
		t.Fatal("expected error for non-numeric quantity, got nil")
	}
}

// TestConvertToBase_InvalidFactor returns an error for non-numeric input.
func TestConvertToBase_InvalidFactor(t *testing.T) {
	_, err := unitconv.ConvertToBase("5", "xyz")
	if err == nil {
		t.Fatal("expected error for non-numeric factor, got nil")
	}
}

// TestConvertToBase_LargeNumbers verifies no overflow for NUMERIC(20,6)-scale inputs.
func TestConvertToBase_LargeNumbers(t *testing.T) {
	got, err := unitconv.ConvertToBase("999999999999", "1000000")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "999999999999000000.000000"
	if got != want {
		t.Errorf("ConvertToBase large = %q, want %q", got, want)
	}
}

// TestConvertToBase_SixDecimalPrecision verifies truncation to 6 decimal places.
func TestConvertToBase_SixDecimalPrecision(t *testing.T) {
	// 1/3 ≈ 0.333333...
	got, err := unitconv.ConvertToBase("1", "0.333333")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 1 * 0.333333 = 0.333333 (factor is 6-decimal input)
	want := "0.333333"
	if got != want {
		t.Errorf("ConvertToBase precision = %q, want %q", got, want)
	}
}
