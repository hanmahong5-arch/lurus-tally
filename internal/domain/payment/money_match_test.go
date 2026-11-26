package payment_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/payment"
)

func moneyBill(name, direction, outstanding string) domain.MoneyBill {
	return domain.MoneyBill{
		ID: uuid.New(), BillNo: name, PartnerName: name, Direction: direction,
		Outstanding: decimal.RequireFromString(outstanding), BillDate: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
	}
}

func TestSuggestMoney_NameNarrowsToOne(t *testing.T) {
	lao := moneyBill("龙口老李", domain.MoneyAR, "1280.00")
	other := moneyBill("别人", domain.MoneyAR, "1280.00")
	suggested, hits := domain.SuggestMoney(decimal.RequireFromString("1280"), "龙口老李(189****)", "货款", []domain.MoneyBill{other, lao})
	if !suggested || len(hits) != 1 || hits[0].PartnerName != "龙口老李" {
		t.Fatalf("suggested=%v hits=%+v", suggested, hits)
	}
}

func TestSuggestMoney_ListsEveryExactAmount(t *testing.T) {
	a := moneyBill("甲", domain.MoneyAR, "1280.00")
	b := moneyBill("乙", domain.MoneyAR, "1280.00")
	purchase := moneyBill("甲", domain.MoneyAP, "1280.00")
	suggested, hits := domain.SuggestMoney(decimal.RequireFromString("1280.00"), "", "", []domain.MoneyBill{a, b, purchase})
	if suggested || len(hits) != 2 {
		t.Fatalf("suggested=%v hits=%d", suggested, len(hits))
	}
}

func TestSuggestMoney_PaymentMatchesPurchase(t *testing.T) {
	sale := moneyBill("龙口老李", domain.MoneyAR, "800.00")
	purchase := moneyBill("龙口老李", domain.MoneyAP, "800.00")
	suggested, hits := domain.SuggestMoney(decimal.RequireFromString("-800"), "付给龙口老李", "", []domain.MoneyBill{sale, purchase})
	if !suggested || len(hits) != 1 || hits[0].Direction != domain.MoneyAP {
		t.Fatalf("suggested=%v hits=%+v", suggested, hits)
	}
}

func TestSuggestMoney_PartialIsNotACandidate(t *testing.T) {
	open := moneyBill("甲", domain.MoneyAR, "2000.00")
	suggested, hits := domain.SuggestMoney(decimal.RequireFromString("50"), "甲", "", []domain.MoneyBill{open})
	if suggested || len(hits) != 0 {
		t.Fatalf("suggested=%v hits=%d", suggested, len(hits))
	}
}
