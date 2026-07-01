package grill_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/hanmahong5-arch/lurus-tally/internal/domain/grill"
)

func money(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func item(price string, qty int, calibrated *int, status grill.OrderItemStatus) grill.OrderItem {
	return grill.OrderItem{
		ID:                uuid.New(),
		Qty:               qty,
		CalibratedQty:     calibrated,
		UnitPriceSnapshot: money(price),
		Status:            status,
	}
}

// TestSessionTotal covers INV-1 and that a 退单 (cancelled line) reduces the
// total without the record being removed.
func TestSessionTotal(t *testing.T) {
	a := item("2.50", 10, intp(14), grill.ItemServed) // 35.00 (calibrated 14)
	b := item("5.00", 2, nil, grill.ItemPending)      // 10.00 (ordered 2)
	c := item("3.00", 5, intp(5), grill.ItemGrilling) // 15.00

	charges := []grill.SharedCharge{
		{Label: "炭火费", Amount: money("5.00"), SplitMode: grill.SplitFixedPerPerson}, // ×2 people = 10.00
	}
	nPeople := 2

	all := []grill.OrderItem{a, b, c}
	if got := grill.SessionTotal(all, charges, nPeople); !got.Equal(money("70.00")) {
		t.Errorf("total (all active)=%s, want 70.00", got) // 35+10+15+10
	}

	// 退单: cancel c → its 15.00 drops out, record retained.
	all[2].Status = grill.ItemCancelled
	if got := grill.SessionTotal(all, charges, nPeople); !got.Equal(money("55.00")) {
		t.Errorf("total (c cancelled)=%s, want 55.00", got) // 35+10+10
	}
}

func TestChargeTotal(t *testing.T) {
	fixed := grill.SharedCharge{Amount: money("5.00"), SplitMode: grill.SplitFixedPerPerson}
	if got := grill.ChargeTotal(fixed, 3); !got.Equal(money("15.00")) {
		t.Errorf("fixed_per_person ×3 = %s, want 15.00", got)
	}
	equal := grill.SharedCharge{Amount: money("5.00"), SplitMode: grill.SplitEqual}
	if got := grill.ChargeTotal(equal, 3); !got.Equal(money("5.00")) {
		t.Errorf("equal total = %s, want 5.00", got)
	}
}

func people(n int) []grill.Person {
	ps := make([]grill.Person, n)
	for i := range ps {
		ps[i] = grill.Person{ID: uuid.New()}
	}
	return ps
}

func sumShares(m map[uuid.UUID]decimal.Decimal) decimal.Decimal {
	s := decimal.Zero
	for _, v := range m {
		s = s.Add(v)
	}
	return s
}

func TestSplitCharge_Equal(t *testing.T) {
	ps := people(3)
	c := grill.SharedCharge{Amount: money("10.00"), SplitMode: grill.SplitEqual}
	shares, err := grill.SplitCharge(c, ps, nil)
	if err != nil {
		t.Fatalf("SplitCharge: %v", err)
	}
	if got := sumShares(shares); !got.Equal(money("10.00")) {
		t.Errorf("shares sum=%s, want 10.00", got)
	}
	// 1000 fen / 3 → 334/333/333; first (largest remainder, stable) gets the extra fen.
	if !shares[ps[0].ID].Equal(money("3.34")) {
		t.Errorf("person0 share=%s, want 3.34", shares[ps[0].ID])
	}
}

func TestSplitCharge_ByCount(t *testing.T) {
	ps := people(2)
	c := grill.SharedCharge{Amount: money("9.00"), SplitMode: grill.SplitByCount}
	counts := map[uuid.UUID]int{ps[0].ID: 2, ps[1].ID: 1}
	shares, err := grill.SplitCharge(c, ps, counts)
	if err != nil {
		t.Fatalf("SplitCharge: %v", err)
	}
	if !shares[ps[0].ID].Equal(money("6.00")) || !shares[ps[1].ID].Equal(money("3.00")) {
		t.Errorf("by_count shares=%v, want 6.00/3.00", shares)
	}
	if got := sumShares(shares); !got.Equal(money("9.00")) {
		t.Errorf("sum=%s, want 9.00", got)
	}
}

func TestSplitCharge_FixedPerPerson(t *testing.T) {
	ps := people(2)
	c := grill.SharedCharge{Amount: money("5.00"), SplitMode: grill.SplitFixedPerPerson}
	shares, err := grill.SplitCharge(c, ps, nil)
	if err != nil {
		t.Fatalf("SplitCharge: %v", err)
	}
	for _, p := range ps {
		if !shares[p.ID].Equal(money("5.00")) {
			t.Errorf("person %v share=%s, want 5.00", p.ID, shares[p.ID])
		}
	}
}

func TestSplitCharge_Errors(t *testing.T) {
	c := grill.SharedCharge{Amount: money("5.00"), SplitMode: grill.SplitEqual}
	if _, err := grill.SplitCharge(c, nil, nil); err != grill.ErrNoPeople {
		t.Errorf("no people: got %v, want ErrNoPeople", err)
	}
	byCount := grill.SharedCharge{Amount: money("5.00"), SplitMode: grill.SplitByCount}
	ps := people(2)
	if _, err := grill.SplitCharge(byCount, ps, map[uuid.UUID]int{}); err != grill.ErrNoWeight {
		t.Errorf("by_count zero weight: got %v, want ErrNoWeight", err)
	}
}

// TestSplitCharge_CentAccuracy: an amount that does not divide evenly must still
// sum to the original to the fen (no lost / phantom money).
func TestSplitCharge_CentAccuracy(t *testing.T) {
	ps := people(3)
	c := grill.SharedCharge{Amount: money("0.10"), SplitMode: grill.SplitEqual} // 10 fen / 3
	shares, err := grill.SplitCharge(c, ps, nil)
	if err != nil {
		t.Fatalf("SplitCharge: %v", err)
	}
	if got := sumShares(shares); !got.Equal(money("0.10")) {
		t.Errorf("cent-accuracy: shares sum=%s, want exactly 0.10", got)
	}
}
