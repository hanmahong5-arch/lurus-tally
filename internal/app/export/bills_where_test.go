package export

import (
	"strings"
	"testing"

	billdomain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

// The invoice-status filter must compare against the total truncated to cents,
// matching the list query and the domain InvoiceStatus().
func TestBuildBillsWhere_InvoiceStatusTruncatesTotal(t *testing.T) {
	for _, st := range []string{billdomain.InvoicePartial, billdomain.InvoiceFull} {
		where, _ := buildBillsWhere(BillsFilter{InvoiceStatus: st})
		if !strings.Contains(where, "TRUNC(bh.total_amount, 2)") {
			t.Errorf("%s: want TRUNC(bh.total_amount, 2) in %q", st, where)
		}
		if strings.Contains(strings.ReplaceAll(where, "TRUNC(bh.total_amount, 2)", ""), "bh.total_amount") {
			t.Errorf("%s: raw bh.total_amount still compared in %q", st, where)
		}
	}
}
