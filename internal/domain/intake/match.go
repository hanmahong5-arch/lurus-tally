package intake

import (
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

var fen = decimal.RequireFromString("0.01")

func nearMoney(a, b decimal.Decimal) bool {
	return a.Sub(b).Abs().LessThanOrEqual(fen)
}

// SuggestIDs picks bills the way a clerk matches a supplier invoice:
// one bill whose remaining equals the total, else up to three bills that
// add up to it, else the oldest remainings that still fit underneath.
// bills are already oldest first. Drafts and fully invoiced rows are ignored.
func SuggestIDs(total decimal.Decimal, bills []OpenBill) []uuid.UUID {
	usable := usableBills(bills)
	if !total.IsPositive() || len(usable) == 0 {
		return nil
	}
	if ids := exactCover(total, usable, 1); ids != nil {
		return ids
	}
	if ids := exactCover(total, usable, 2); ids != nil {
		return ids
	}
	if ids := exactCover(total, usable, 3); ids != nil {
		return ids
	}
	var ids []uuid.UUID
	sum := decimal.Zero
	for _, b := range usable {
		if sum.Add(b.Remaining).GreaterThan(total.Add(fen)) {
			continue
		}
		ids = append(ids, b.ID)
		sum = sum.Add(b.Remaining)
		if nearMoney(sum, total) {
			break
		}
	}
	return ids
}

func usableBills(bills []OpenBill) []OpenBill {
	out := make([]OpenBill, 0, len(bills))
	for _, b := range bills {
		if b.Status == 2 && b.Remaining.IsPositive() {
			out = append(out, b)
		}
	}
	return out
}

func exactCover(total decimal.Decimal, bills []OpenBill, n int) []uuid.UUID {
	ids := make([]uuid.UUID, 0, n)
	var walk func(start, left int, sum decimal.Decimal) []uuid.UUID
	walk = func(start, left int, sum decimal.Decimal) []uuid.UUID {
		if left == 0 {
			if nearMoney(sum, total) {
				cp := append([]uuid.UUID(nil), ids...)
				return cp
			}
			return nil
		}
		for i := start; i <= len(bills)-left; i++ {
			ids = append(ids, bills[i].ID)
			if got := walk(i+1, left-1, sum.Add(bills[i].Remaining)); got != nil {
				return got
			}
			ids = ids[:len(ids)-1]
		}
		return nil
	}
	return walk(0, n, decimal.Zero)
}

// Allocate spends the invoice total on the chosen bills in that order.
// Each bill takes its remaining, or whatever is left of the invoice.
// A bill that is not approved, unknown, or already fully invoiced is skipped.
func Allocate(total decimal.Decimal, bills []OpenBill, chosen []uuid.UUID) []BillLink {
	byID := make(map[uuid.UUID]OpenBill, len(bills))
	for _, b := range bills {
		byID[b.ID] = b
	}
	left := total
	var links []BillLink
	for _, id := range chosen {
		b, ok := byID[id]
		if !ok || b.Status != 2 || !b.Remaining.IsPositive() || !left.GreaterThan(fen) {
			continue
		}
		part := b.Remaining
		if part.GreaterThan(left) {
			part = left
		}
		part = part.Round(2)
		if !part.IsPositive() {
			continue
		}
		links = append(links, BillLink{BillID: id, Amount: part})
		left = left.Sub(part)
	}
	return links
}

// CoverWarning tells the reviewer when the invoice is larger than the bills it was given.
func CoverWarning(total decimal.Decimal, links []BillLink) string {
	sum := decimal.Zero
	for _, l := range links {
		sum = sum.Add(l.Amount)
	}
	gap := total.Sub(sum).Round(2)
	if gap.GreaterThan(fen) {
		return "发票已登记，还有 " + gap.StringFixed(2) + " 元没有对上未开票单据"
	}
	return ""
}
