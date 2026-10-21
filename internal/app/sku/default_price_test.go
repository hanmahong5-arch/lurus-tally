package sku

import (
	"errors"
	"testing"
)

func sp(s string) *string { return &s }

func TestParsePrices(t *testing.T) {
	got, err := ParsePrices(PriceInput{Retail: sp("12.50"), Wholesale: sp(" "), Purchase: nil})
	if err != nil {
		t.Fatal(err)
	}
	if got.Retail == nil || got.Retail.String() != "12.5" {
		t.Fatalf("retail = %v", got.Retail)
	}
	if got.Wholesale != nil || got.Purchase != nil {
		t.Fatal("blank/nil must stay unset")
	}
	if got.IsEmpty() {
		t.Fatal("not empty")
	}
	if e, _ := ParsePrices(PriceInput{}); !e.IsEmpty() {
		t.Fatal("empty input must be empty")
	}
}

func TestParsePrices_Invalid(t *testing.T) {
	for _, bad := range []string{"abc", "-1", "1e20"} {
		if _, err := ParsePrices(PriceInput{Purchase: sp(bad)}); err == nil {
			t.Fatalf("%q should fail", bad)
		}
	}
}

func TestParsePrices_ErrorIsSentinelWithPlainChinese(t *testing.T) {
	_, err := ParsePrices(PriceInput{Retail: sp("abc")})
	if !errors.Is(err, ErrInvalidPrice) {
		t.Fatalf("want ErrInvalidPrice, got %v", err)
	}
	if err.Error() != "零售价格式不正确，请输入数字" {
		t.Fatalf("message = %q", err.Error())
	}
	var pe *PriceError
	if !errors.As(err, &pe) || pe.Msg != err.Error() {
		t.Fatalf("PriceError not extractable: %v", err)
	}
}

func TestParsePricesForUpdate_BlankClearsNilKeeps(t *testing.T) {
	got, err := ParsePricesForUpdate(PriceInput{Retail: sp(" "), Wholesale: sp("3"), Purchase: nil})
	if err != nil {
		t.Fatal(err)
	}
	if got.Retail == nil || !got.Retail.IsZero() {
		t.Fatalf("blank must clear to zero, got %v", got.Retail)
	}
	if got.Wholesale == nil || got.Wholesale.String() != "3" {
		t.Fatalf("wholesale = %v", got.Wholesale)
	}
	if got.Purchase != nil {
		t.Fatal("nil must stay untouched")
	}
}
