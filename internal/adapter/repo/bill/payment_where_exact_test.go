package bill

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

// The payment_status fragments are locked verbatim (not by substring) so the
// zero-total and boundary behaviour cannot drift unnoticed.
func TestBuildListWhere_PaymentStatusExactFragments(t *testing.T) {
	tid := uuid.New()
	sale := "GREATEST(b.total_amount - COALESCE(r.amt, 0), 0)"
	cases := []struct {
		name string
		typ  domain.BillType
		ps   string
		want string
	}{
		{"purchase unpaid", domain.BillTypePurchase, domain.PaymentUnpaid, " AND b.paid_amount <= 0"},
		{"purchase partial", domain.BillTypePurchase, domain.PaymentPartial, " AND b.paid_amount > 0 AND b.paid_amount < b.total_amount"},
		{"purchase paid", domain.BillTypePurchase, domain.PaymentPaid, " AND b.paid_amount > 0 AND b.paid_amount >= b.total_amount"},
		{"purchase open", domain.BillTypePurchase, "open", " AND b.total_amount > b.paid_amount"},
		{"sale partial is net of returns", domain.BillTypeSale, domain.PaymentPartial, " AND b.paid_amount > 0 AND b.paid_amount < " + sale},
		{"sale paid is net of returns", domain.BillTypeSale, domain.PaymentPaid, " AND b.paid_amount > 0 AND b.paid_amount >= " + sale},
		{"sale open is net of returns", domain.BillTypeSale, "open", " AND " + sale + " > b.paid_amount"},
	}
	for _, tc := range cases {
		w, _, _ := buildListWhere(appbill.BillListFilter{TenantID: tid, BillType: tc.typ, PaymentStatus: tc.ps})
		if !strings.HasSuffix(w, tc.want) {
			t.Errorf("%s: where=%q, want suffix %q", tc.name, w, tc.want)
		}
	}
}
