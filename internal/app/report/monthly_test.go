package report

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func TestParseMonth_CutsAtBeijingMidnight(t *testing.T) {
	now := time.Date(2026, 10, 10, 3, 0, 0, 0, time.UTC)
	from, to, label, err := ParseMonth("2026-09", now)
	if err != nil {
		t.Fatalf("ParseMonth: %v", err)
	}
	if label != "2026-09" {
		t.Errorf("label = %q", label)
	}
	if want := time.Date(2026, 8, 31, 16, 0, 0, 0, time.UTC); !from.Equal(want) {
		t.Errorf("from = %v, want %v", from.UTC(), want)
	}
	if want := time.Date(2026, 9, 30, 16, 0, 0, 0, time.UTC); !to.Equal(want) {
		t.Errorf("to = %v, want %v", to.UTC(), want)
	}
}

func TestParseMonth_EmptyIsCurrentBeijingMonth(t *testing.T) {
	// 2026-09-30 17:00 UTC is already 2026-10-01 in Beijing.
	now := time.Date(2026, 9, 30, 17, 0, 0, 0, time.UTC)
	_, _, label, err := ParseMonth("", now)
	if err != nil || label != "2026-10" {
		t.Errorf("label = %q, err = %v, want 2026-10", label, err)
	}
}

func TestParseMonth_Rejects(t *testing.T) {
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	for _, in := range []string{"2026-13", "2026/09", "abc", "2026-9", "2099-01", "1999-01"} {
		if _, _, _, err := ParseMonth(in, now); err == nil {
			t.Errorf("ParseMonth(%q) accepted, want error", in)
		}
	}
}

func TestBuild_DerivesNetFigures(t *testing.T) {
	f := Figures{
		Sales: dec("1000"), SaleReturns: dec("100"), CostOfGoods: dec("540.5"),
		Purchases: dec("800"), PurchaseReturns: dec("50"),
		Receipts: dec("600"), Payments: dec("300"),
		Receivable: dec("400"), Payable: dec("450"), InventoryValue: dec("1234.5"),
	}
	m := Build("2026-09", f)
	want := map[string]string{
		"net_sales":     m.NetSales,
		"cost_of_goods": m.CostOfGoods,
		"gross_profit":  m.GrossProfit,
		"net_purchases": m.NetPurchases,
		"receipts":      m.Receipts,
		"payments":      m.Payments,
		"receivable":    m.Receivable,
		"payable":       m.Payable,
		"inventory":     m.InventoryValue,
	}
	exp := map[string]string{
		"net_sales": "900.00", "cost_of_goods": "540.50", "gross_profit": "359.50",
		"net_purchases": "750.00", "receipts": "600.00", "payments": "300.00",
		"receivable": "400.00", "payable": "450.00", "inventory": "1234.50",
	}
	for k, v := range exp {
		if want[k] != v {
			t.Errorf("%s = %q, want %q", k, want[k], v)
		}
	}
	if m.Month != "2026-09" {
		t.Errorf("month = %q", m.Month)
	}
}

func TestBuild_NegativeGrossProfitKept(t *testing.T) {
	m := Build("2026-09", Figures{Sales: dec("100"), CostOfGoods: dec("130")})
	if m.GrossProfit != "-30.00" {
		t.Errorf("gross_profit = %q", m.GrossProfit)
	}
}

func TestParseMonth_FutureMonthHasOwnMessage(t *testing.T) {
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	if _, _, _, err := ParseMonth("2026-11", now); err != ErrFutureMonth {
		t.Errorf("err = %v, want ErrFutureMonth", err)
	}
}
