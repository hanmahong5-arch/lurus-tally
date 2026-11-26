package export

import (
	"strings"
	"testing"

	billdomain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

// The invoice-status filter must compare against the net total truncated to cents,
// matching the list query and the domain InvoiceStatus().
func TestBuildBillsWhere_InvoiceStatusTruncatesTotal(t *testing.T) {
	const base = "TRUNC(GREATEST(bh.total_amount - COALESCE(r.amt, 0), 0), 2)"
	for _, st := range []string{billdomain.InvoicePartial, billdomain.InvoiceFull, "open"} {
		where, _ := buildBillsWhere(BillsFilter{InvoiceStatus: st})
		if !strings.Contains(where, base) {
			t.Errorf("%s: want %s in %q", st, base, where)
		}
		if strings.Contains(strings.ReplaceAll(where, base, ""), "bh.total_amount") {
			t.Errorf("%s: raw bh.total_amount still compared in %q", st, where)
		}
	}
	none, _ := buildBillsWhere(BillsFilter{InvoiceStatus: billdomain.InvoiceNone})
	if !strings.Contains(none, base+" > 0") {
		t.Errorf("none must skip a sale with nothing left to invoice: %q", none)
	}
}
