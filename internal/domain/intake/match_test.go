package intake

import (
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func bill(no, remaining string) OpenBill {
	return OpenBill{ID: uuid.New(), BillNo: no, Status: 2, Remaining: decimal.RequireFromString(remaining)}
}

func TestSuggestIDs_PrefersAnExactCover(t *testing.T) {
	a, b, c := bill("A", "40"), bill("B", "113"), bill("C", "73")
	got := SuggestIDs(decimal.RequireFromString("113"), []OpenBill{a, b, c})
	if len(got) != 1 || got[0] != b.ID {
		t.Fatalf("%v", got)
	}
	pair := SuggestIDs(decimal.RequireFromString("113"), []OpenBill{a, c, bill("D", "10")})
	if len(pair) != 2 || pair[0] != a.ID || pair[1] != c.ID {
		t.Fatalf("%v", pair)
	}
	draft := bill("E", "113")
	draft.Status = 0
	if SuggestIDs(decimal.RequireFromString("113"), []OpenBill{draft}) != nil {
		t.Fatal("a draft is not a match")
	}
}

func TestSuggestIDs_FillsOldestThatFit(t *testing.T) {
	a, b, c := bill("A", "80"), bill("B", "50"), bill("C", "20")
	got := SuggestIDs(decimal.RequireFromString("100"), []OpenBill{a, b, c})
	if len(got) != 2 || got[0] != a.ID || got[1] != c.ID {
		t.Fatalf("skip the bill that would go over: %v", got)
	}
}

func TestAllocate_CapsAtTheInvoiceAndSkipsAFullBill(t *testing.T) {
	a, b := bill("A", "200"), bill("B", "0")
	links := Allocate(decimal.RequireFromString("113"), []OpenBill{a, b}, []uuid.UUID{a.ID, b.ID})
	if len(links) != 1 || !links[0].Amount.Equal(decimal.RequireFromString("113")) || links[0].BillID != a.ID {
		t.Fatalf("%+v", links)
	}
	if CoverWarning(decimal.RequireFromString("113"), links) != "" {
		t.Fatal("a capped bill covers the invoice")
	}
	gap := CoverWarning(decimal.RequireFromString("113"), nil)
	if gap != "发票已登记，还有 113.00 元没有对上未开票单据" {
		t.Fatal(gap)
	}
}
