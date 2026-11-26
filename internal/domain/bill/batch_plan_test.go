package bill

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func TestPlanOpenRemaining_CoversNegativeOnHand(t *testing.T) {
	if got := PlanOpenRemaining(dec("10"), dec("7")); !got.Equal(dec("7")) {
		t.Fatalf("fill a hole of 3: got %s", got)
	}
	if got := PlanOpenRemaining(dec("4"), dec("-6")); !got.Equal(dec("0")) {
		t.Fatalf("receipt smaller than the hole: got %s", got)
	}
	if got := PlanOpenRemaining(dec("2"), dec("12")); !got.Equal(dec("2")) {
		t.Fatalf("positive stock: got %s", got)
	}
}

func TestPlanDraws_UntrackedThenOldest(t *testing.T) {
	wh := uuid.New()
	older := BatchLot{ID: uuid.MustParse("00000000-0000-0000-0000-000000000002"), WarehouseID: wh, Remaining: dec("3"), ReceivedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)}
	newer := BatchLot{ID: uuid.MustParse("00000000-0000-0000-0000-000000000001"), WarehouseID: wh, Remaining: dec("5"), ReceivedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	sale := uuid.New()
	draws, rem, err := PlanDraws(wh, dec("10"), []BatchLot{older, newer}, []DrawRequest{{
		SaleItemID: sale, QtyBase: dec("4"), Factor: dec("1"),
	}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(draws) != 2 || draws[0].BatchID != uuid.Nil || !draws[0].QtyBase.Equal(dec("2")) {
		t.Fatalf("untracked first: %+v", draws)
	}
	if draws[1].BatchID != newer.ID || !draws[1].QtyBase.Equal(dec("2")) {
		t.Fatalf("then the earlier receipt: %+v", draws[1])
	}
	if !rem[newer.ID].Equal(dec("3")) || !rem[older.ID].Equal(dec("3")) {
		t.Fatalf("remaining: %+v", rem)
	}
}

func TestPlanDraws_PinStaysOnItsReceipt(t *testing.T) {
	wh := uuid.New()
	src := uuid.New()
	lot := BatchLot{ID: uuid.New(), SourceItemID: src, WarehouseID: wh, Remaining: dec("1"), ReceivedAt: time.Now()}
	other := BatchLot{ID: uuid.New(), WarehouseID: wh, Remaining: dec("9"), ReceivedAt: lot.ReceivedAt.Add(-time.Hour)}
	draws, _, err := PlanDraws(wh, dec("10"), []BatchLot{lot, other}, []DrawRequest{{
		SaleItemID: uuid.New(), QtyBase: dec("1"), Factor: dec("1"), SourceItemID: src,
	}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(draws) != 1 || draws[0].BatchID != lot.ID {
		t.Fatalf("pinned draw: %+v", draws)
	}
}

func TestPlanDraws_PinShortNamesTheLineUnit(t *testing.T) {
	wh := uuid.New()
	src := uuid.New()
	lot := BatchLot{ID: uuid.New(), SourceItemID: src, WarehouseID: wh, Remaining: dec("10")}
	_, _, err := PlanDraws(wh, dec("10"), []BatchLot{lot}, []DrawRequest{{
		SaleItemID: uuid.New(), QtyBase: dec("20"), Factor: dec("10"), SourceItemID: src,
	}}, false)
	short, ok := err.(*BatchShortError)
	if !ok || !short.Remaining.Equal(dec("1")) || !short.Requested.Equal(dec("2")) {
		t.Fatalf("got %v", err)
	}
}

func TestPlanDraws_OtherWarehouseIsNotThisReceipt(t *testing.T) {
	here, there := uuid.New(), uuid.New()
	src := uuid.New()
	lot := BatchLot{ID: uuid.New(), SourceItemID: src, WarehouseID: there, Remaining: dec("1")}
	_, _, err := PlanDraws(here, dec("1"), []BatchLot{lot}, []DrawRequest{{
		SaleItemID: uuid.New(), QtyBase: dec("1"), Factor: dec("1"), SourceItemID: src,
	}}, false)
	if _, ok := err.(*BatchWarehouseError); !ok {
		t.Fatalf("got %v", err)
	}
}

func TestPlanDraws_MissingPinFallsThrough(t *testing.T) {
	wh := uuid.New()
	draws, _, err := PlanDraws(wh, dec("4"), nil, []DrawRequest{{
		SaleItemID: uuid.New(), QtyBase: dec("1"), Factor: dec("1"), SourceItemID: uuid.New(),
	}}, false)
	if err != nil || len(draws) != 1 || draws[0].BatchID != uuid.Nil {
		t.Fatalf("legacy pin draws untracked stock: %+v %v", draws, err)
	}
}

func TestPlanDraws_BatchesAheadOfStock(t *testing.T) {
	wh := uuid.New()
	lot := BatchLot{ID: uuid.New(), WarehouseID: wh, Remaining: dec("5")}
	_, _, err := PlanDraws(wh, dec("4"), []BatchLot{lot}, []DrawRequest{{
		SaleItemID: uuid.New(), QtyBase: dec("1"), Factor: dec("1"),
	}}, false)
	if err != ErrBatchOutOfSync {
		t.Fatalf("got %v", err)
	}
}

func TestPlanRestore_NewestDrawFirst(t *testing.T) {
	first, second := uuid.New(), uuid.New()
	a, b := uuid.New(), uuid.New()
	add, left := PlanRestore([]DrawRow{
		{ID: second, BatchID: b, QtyBase: dec("2")},
		{ID: first, BatchID: a, QtyBase: dec("3")},
	}, dec("3"))
	if !add[b].Equal(dec("2")) || !add[a].Equal(dec("1")) {
		t.Fatalf("add %+v", add)
	}
	if !left[second].IsZero() || !left[first].Equal(dec("2")) {
		t.Fatalf("left %+v", left)
	}
}
