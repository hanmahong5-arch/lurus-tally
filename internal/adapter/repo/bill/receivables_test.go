package bill

import (
	"strings"
	"testing"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
)

func TestReceivablesQuery(t *testing.T) {
	ar := receivablesQuery(appbill.ReceivableAR)
	ap := receivablesQuery(appbill.ReceivableAP)
	for _, frag := range []string{"b.status = 2", "bal > 0", "age <= 30", "BETWEEN 31 AND 60", "BETWEEN 61 AND 90", "age > 90", "Asia/Shanghai"} {
		if !strings.Contains(ar, frag) || !strings.Contains(ap, frag) {
			t.Errorf("missing %q", frag)
		}
	}
	if !strings.Contains(ar, "rsum") || strings.Contains(ap, "rsum") {
		t.Error("only the receivable side nets sales returns")
	}
}

func TestListOrder(t *testing.T) {
	if listOrder(appbill.BillListFilter{}) != "b.bill_date DESC, b.created_at DESC" {
		t.Error("default order")
	}
	if listOrder(appbill.BillListFilter{OldestFirst: true}) != "b.bill_date ASC, b.created_at ASC" {
		t.Error("oldest first")
	}
}

func TestSaleReturnsCTEGuards(t *testing.T) {
	ar := receivablesQuery(appbill.ReceivableAR)
	if !strings.Contains(ar, "h.bill_date < $4") {
		t.Error("returns after the as-of day must not be netted")
	}
	// A malformed tail must be filtered by the regex, never cast.
	if !strings.Contains(ar, "[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$") || strings.Contains(ar, "{36}$") {
		t.Error("cancel remark pointer needs a strict uuid pattern")
	}
}
