package search

import (
	"context"
	"strings"
	"testing"

	"github.com/hanmahong5-arch/lurus-tally/internal/domain/permission"
)

func TestBillSearchSQL_HidesPurchaseSideWithoutCostView(t *testing.T) {
	if strings.Contains(billSearchSQL(false), "采购") {
		t.Fatal("a caller with cost.view searches every bill")
	}
	hidden := billSearchSQL(true)
	if !strings.Contains(hidden, "sub_type NOT LIKE '采购%'") {
		t.Fatalf("purchase side must be excluded: %s", hidden)
	}
	for _, want := range []string{"tenant_id = $1", "bill_no ILIKE $2", "LIMIT $3"} {
		if !strings.Contains(hidden, want) {
			t.Errorf("lost %q: %s", want, hidden)
		}
	}
}

func TestSearchBills_ReadsTheContextMark(t *testing.T) {
	if permission.CostHidden(context.Background()) {
		t.Fatal("plain context must not be marked")
	}
	if !permission.CostHidden(permission.WithCostHidden(context.Background())) {
		t.Fatal("marked context must report hidden")
	}
}
