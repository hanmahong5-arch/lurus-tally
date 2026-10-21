package bill

import (
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

func TestParseListFilter_InvoiceStatus(t *testing.T) {
	for _, ok := range []string{"none", "partial", "full"} {
		q, _ := url.ParseQuery("invoice_status=" + ok)
		var f appbill.BillListFilter
		if err := parseListFilter(q, uuid.Nil, &f); err != nil || f.InvoiceStatus != ok {
			t.Errorf("%s: err=%v f=%q", ok, err, f.InvoiceStatus)
		}
	}
	for _, bad := range []string{"invoice_status=weird", "invoice_status=full&status=0", "invoice_status=none&status=9"} {
		q, _ := url.ParseQuery(bad)
		var f appbill.BillListFilter
		if err := parseListFilter(q, uuid.Nil, &f); err == nil {
			t.Errorf("%s: want error", bad)
		}
	}
}

func TestBillHead_InvoiceStatus(t *testing.T) {
	d := decimal.RequireFromString
	cases := []struct{ total, inv, want string }{
		{"100", "0", domain.InvoiceNone},
		{"100", "40", domain.InvoicePartial},
		{"100", "100", domain.InvoiceFull},
		{"100", "120", domain.InvoiceFull},
	}
	for _, c := range cases {
		b := domain.BillHead{TotalAmount: d(c.total), InvoicedAmount: d(c.inv)}
		if got := b.InvoiceStatus(); got != c.want {
			t.Errorf("%s/%s: %s want %s", c.inv, c.total, got, c.want)
		}
	}
	item := newListItem(domain.BillHead{TotalAmount: d("100"), InvoicedAmount: d("40")})
	if item.InvoiceStatus != "partial" || item.InvoicedAmount != "40" {
		t.Errorf("%+v", item)
	}
}
