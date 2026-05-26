package decimalutil_test

import (
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/decimalutil"
)

func TestParse_ValidDecimal(t *testing.T) {
	d, err := decimalutil.Parse("12.34", "qty")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !d.Equal(decimal.NewFromFloat(12.34)) {
		t.Errorf("got %s, want 12.34", d)
	}
}

func TestParse_EmptyTreatedAsZero(t *testing.T) {
	d, err := decimalutil.Parse("", "qty")
	if err != nil {
		t.Fatalf("Parse empty: %v", err)
	}
	if !d.IsZero() {
		t.Errorf("got %s, want 0", d)
	}
}

func TestParse_MalformedReturnsError(t *testing.T) {
	_, err := decimalutil.Parse("not-a-number", "qty")
	if err == nil {
		t.Fatal("expected error for malformed input")
	}
	if !strings.Contains(err.Error(), "qty") {
		t.Errorf("error should tag field name, got: %v", err)
	}
	if !strings.Contains(err.Error(), "not-a-number") {
		t.Errorf("error should quote bad input, got: %v", err)
	}
}
