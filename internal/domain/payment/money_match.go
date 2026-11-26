package payment

import (
	"strings"

	"github.com/shopspring/decimal"
)

// SuggestMoney lists approved bills whose open balance equals the statement amount.
// A partner name found in the counterparty or the memo narrows the list.
// suggested is true only when one bill remains.
func SuggestMoney(amount decimal.Decimal, counterparty, memo string, bills []MoneyBill) (suggested bool, hits []MoneyBill) {
	want := MoneyAR
	if amount.IsNegative() {
		want = MoneyAP
	}
	abs := amount.Abs().Round(2)
	var exact []MoneyBill
	for _, b := range bills {
		if b.Direction == want && b.Outstanding.Round(2).Equal(abs) {
			exact = append(exact, b)
		}
	}
	chosen := exact
	var named []MoneyBill
	for _, b := range exact {
		if nameHit(b.PartnerName, counterparty, memo) {
			named = append(named, b)
		}
	}
	if len(named) > 0 {
		chosen = named
	}
	suggested = len(chosen) == 1
	if len(chosen) > 8 {
		chosen = chosen[:8]
	}
	return suggested, chosen
}

func nameHit(partner, counterparty, memo string) bool {
	p := compactName(partner)
	if len([]rune(p)) < 2 {
		return false
	}
	return strings.Contains(compactName(counterparty+memo), p)
}

func compactName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch r {
		case ' ', '\t', '*', '(', ')', '（', '）':
			continue
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
