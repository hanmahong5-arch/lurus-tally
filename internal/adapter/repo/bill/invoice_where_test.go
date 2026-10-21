package bill

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

func TestBuildListWhere_InvoiceStatus(t *testing.T) {
	tid := uuid.New()
	for st, frag := range map[string]string{
		domain.InvoiceNone:    "<= 0",
		domain.InvoicePartial: "< TRUNC(b.total_amount, 2)",
		domain.InvoiceFull:    ">= TRUNC(b.total_amount, 2)",
	} {
		w, args, _ := buildListWhere(appbill.BillListFilter{TenantID: tid, InvoiceStatus: st})
		if !strings.Contains(w, "tally.invoice_bill") || !strings.Contains(w, "iv.status = 'issued'") || !strings.Contains(w, frag) {
			t.Errorf("%s: where=%q", st, w)
		}
		if !strings.Contains(w, "b.status = $2") || len(args) != 2 || args[1] != int16(domain.StatusApproved) {
			t.Errorf("%s: must imply approved: where=%q args=%v", st, w, args)
		}
	}
	w, _, _ := buildListWhere(appbill.BillListFilter{TenantID: tid})
	if strings.Contains(w, "invoice_bill") {
		t.Errorf("no filter must not touch invoice tables: %q", w)
	}
}
