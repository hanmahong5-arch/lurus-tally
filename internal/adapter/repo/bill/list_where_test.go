package bill

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

func TestEscapeLike(t *testing.T) {
	cases := map[string]string{
		`a\b`: `a\\b`,
		`\`:   `\\`,
		`50%`: `50\%`,
		`a_b`: `a\_b`,
		`abc`: `abc`,
	}
	for in, want := range cases {
		if got := escapeLike(in); got != want {
			t.Errorf("escapeLike(%q)=%q want %q", in, got, want)
		}
	}
}

func TestBuildListWhere(t *testing.T) {
	tid := uuid.New()
	to := time.Date(2026, 11, 1, 0, 0, 0, 0, time.FixedZone("CST", 8*3600))
	draft := domain.StatusDraft

	t.Run("payment status implies approved", func(t *testing.T) {
		for ps, frag := range map[string]string{
			domain.PaymentUnpaid:  "b.paid_amount <= 0",
			domain.PaymentPartial: "b.paid_amount > 0 AND b.paid_amount < b.total_amount",
			domain.PaymentPaid:    "b.paid_amount >= b.total_amount",
			"open":                "b.total_amount > b.paid_amount",
		} {
			w, args, _ := buildListWhere(appbill.BillListFilter{TenantID: tid, PaymentStatus: ps})
			if !strings.Contains(w, frag) || !strings.Contains(w, "b.status = $2") {
				t.Errorf("%s: where=%q", ps, w)
			}
			if len(args) != 2 || args[1] != int16(domain.StatusApproved) {
				t.Errorf("%s: args=%v", ps, args)
			}
		}
	})
	t.Run("date_to is exclusive", func(t *testing.T) {
		w, args, _ := buildListWhere(appbill.BillListFilter{TenantID: tid, DateTo: &to})
		if !strings.Contains(w, "b.bill_date < $2") || strings.Contains(w, "<=") {
			t.Errorf("where=%q", w)
		}
		if len(args) != 2 {
			t.Errorf("args=%v", args)
		}
	})
	t.Run("keyword placeholder and escape", func(t *testing.T) {
		w, args, idx := buildListWhere(appbill.BillListFilter{TenantID: tid, Status: &draft, Keyword: `5%_\`})
		if !strings.Contains(w, "b.bill_no ILIKE $3") || !strings.Contains(w, "p.name ILIKE $3") {
			t.Errorf("where=%q", w)
		}
		if args[2] != `%5\%\_\\%` || idx != 4 {
			t.Errorf("args=%v idx=%d", args, idx)
		}
	})
}

func TestBuildListWhereSaleOpenIsNetOfReturns(t *testing.T) {
	tid := uuid.New()
	f := appbill.BillListFilter{TenantID: tid, BillType: domain.BillTypeSale, PaymentStatus: "open"}
	w, _, _ := buildListWhere(f)
	if !strings.Contains(w, "GREATEST(b.total_amount - COALESCE(r.amt, 0), 0) > b.paid_amount") {
		t.Errorf("sale open must use the net total: %q", w)
	}
	if !strings.HasPrefix(listCTE(f), "WITH") || !strings.Contains(listCTE(f), "rsum") || !strings.Contains(listFromFor(f), "JOIN rsum") {
		t.Error("sale list must carry the returns CTE and join")
	}
	buy := appbill.BillListFilter{TenantID: tid, BillType: domain.BillTypePurchase}
	if listCTE(buy) != "" || strings.Contains(listFromFor(buy), "rsum") {
		t.Error("purchase list must not touch returns")
	}
}

func TestBuildListWhereSubType(t *testing.T) {
	w, args, idx := buildListWhere(appbill.BillListFilter{TenantID: uuid.New(), BillType: domain.BillTypePurchase, SubType: domain.BillSubTypePurchase})
	if !strings.Contains(w, "b.sub_type = $3") || args[2] != "采购" || idx != 4 {
		t.Errorf("where=%q args=%v idx=%d", w, args, idx)
	}
}
