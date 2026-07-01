package grill

import (
	"errors"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// ErrNoPeople is returned when a charge is split across an empty people set.
var ErrNoPeople = errors.New("grill: cannot split a charge with no people")

// ErrNoWeight is returned for a by_count split when nobody has any items, so
// there is no weight to apportion by.
var ErrNoWeight = errors.New("grill: cannot split by_count when no items are counted")

// SessionTotal is INV-1: the amount due for a session equals the sum of every
// non-cancelled line (calibrated qty × snapshot price) plus the session's share
// of every surcharge. 退单 (cancelled) lines are excluded — that is how a
// cancellation reduces the bill without deleting the record.
func SessionTotal(items []OrderItem, charges []SharedCharge, nPeople int) decimal.Decimal {
	total := decimal.Zero
	for i := range items {
		if items[i].Status == ItemCancelled {
			continue
		}
		total = total.Add(items[i].LineAmount())
	}
	for i := range charges {
		total = total.Add(ChargeTotal(charges[i], nPeople))
	}
	return total
}

// ChargeTotal is the session-level contribution of a single surcharge. For
// fixed_per_person it is Amount × people; for equal / by_count the Amount is
// already the total to apportion.
func ChargeTotal(c SharedCharge, nPeople int) decimal.Decimal {
	if c.SplitMode == SplitFixedPerPerson {
		return c.Amount.Mul(decimal.NewFromInt(int64(nPeople)))
	}
	return c.Amount
}

// SplitCharge apportions a surcharge across people per its SplitMode, returning
// each person's share. Shares always sum exactly to ChargeTotal (cent-accurate
// largest-remainder distribution — no lost or phantom fen).
//
//   - fixed_per_person: every person pays Amount.
//   - equal:            Amount split evenly.
//   - by_count:         Amount weighted by each person's item count
//     (countByItem keyed on person id).
func SplitCharge(c SharedCharge, people []Person, countByPerson map[uuid.UUID]int) (map[uuid.UUID]decimal.Decimal, error) {
	if len(people) == 0 {
		return nil, ErrNoPeople
	}
	switch c.SplitMode {
	case SplitFixedPerPerson:
		out := make(map[uuid.UUID]decimal.Decimal, len(people))
		for _, p := range people {
			out[p.ID] = c.Amount
		}
		return out, nil
	case SplitByCount:
		weights := make([]int, len(people))
		var totalWeight int
		for i, p := range people {
			weights[i] = countByPerson[p.ID]
			totalWeight += weights[i]
		}
		if totalWeight == 0 {
			return nil, ErrNoWeight
		}
		return splitWeighted(c.Amount, people, weights), nil
	case SplitEqual:
		weights := make([]int, len(people))
		for i := range weights {
			weights[i] = 1
		}
		return splitWeighted(c.Amount, people, weights), nil
	default:
		return nil, fmt.Errorf("grill: unknown split mode %q", c.SplitMode)
	}
}

// splitWeighted distributes total across people proportionally to integer
// weights, working in integer fen so the shares sum to total exactly. Any
// leftover fen from integer division go to the largest fractional remainders
// (ties broken by position), the standard largest-remainder method.
func splitWeighted(total decimal.Decimal, people []Person, weights []int) map[uuid.UUID]decimal.Decimal {
	totalFen := total.Mul(decimal.NewFromInt(100)).Round(0).IntPart()
	var W int64
	for _, w := range weights {
		W += int64(w)
	}

	out := make(map[uuid.UUID]decimal.Decimal, len(people))
	if W == 0 {
		for _, p := range people {
			out[p.ID] = decimal.Zero
		}
		return out
	}

	base := make([]int64, len(people))
	rem := make([]int64, len(people))
	var assigned int64
	for i, w := range weights {
		num := totalFen * int64(w)
		base[i] = num / W
		rem[i] = num % W
		assigned += base[i]
	}

	leftover := totalFen - assigned
	order := make([]int, len(people))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return rem[order[a]] > rem[order[b]] })
	for k := int64(0); k < leftover && int(k) < len(order); k++ {
		base[order[k]]++
	}

	for i, p := range people {
		out[p.ID] = decimal.New(base[i], -2) // fen → yuan (×10^-2)
	}
	return out
}
