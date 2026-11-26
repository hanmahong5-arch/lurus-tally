package ai

import (
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
)

// draftcheckEvaluateLines compares attributed and pooled quantities against draft lines.
func draftcheckEvaluateLines(
	lines []CheckLine,
	qs []Quantity,
	claimed []bool,
	cand map[int][]Quantity,
	forced map[int]bool,
	notes map[int]string,
) []DraftFlag {
	var flags []DraftFlag
	var eachPool, plainPool []decimal.Decimal
	for qi, q := range qs {
		if claimed[qi] {
			continue
		}
		if q.Each {
			eachPool = append(eachPool, q.Value)
		} else {
			plainPool = append(plainPool, q.Value)
		}
	}

	var pooled []int
	for i, ln := range lines {
		if !ln.Qty.IsPositive() {
			continue
		}
		if forced[i] {
			f := mismatch(i, ln)
			if note, ok := notes[i]; ok {
				f.Message = note
			}
			flags = append(flags, f)
			continue
		}
		if cs := preferUnit(cand[i]); len(cs) > 0 {
			ok := false
			for _, c := range cs {
				if c.Value.Equal(ln.Qty) {
					ok = true
					break
				}
			}
			if !ok {
				flags = append(flags, mismatch(i, ln))
			}
			continue
		}
		pooled = append(pooled, i)
	}

	if len(pooled) >= 2 && distinctDecs(append(append([]decimal.Decimal{}, eachPool...), plainPool...)) >= 2 {
		for _, i := range pooled {
			flags = append(flags, mismatch(i, lines[i]))
		}
		return flags
	}
	for _, i := range pooled {
		ln := lines[i]
		if containsDec(eachPool, ln.Qty) {
			continue
		}
		if k := indexDec(plainPool, ln.Qty); k >= 0 {
			plainPool = append(plainPool[:k:k], plainPool[k+1:]...)
			continue
		}
		flags = append(flags, mismatch(i, ln))
	}
	return flags
}

// distinctDecs counts distinct decimal values in vs.
func distinctDecs(vs []decimal.Decimal) int {
	var seen []decimal.Decimal
	for _, v := range vs {
		if !containsDec(seen, v) {
			seen = append(seen, v)
		}
	}
	return len(seen)
}

// containsDec reports whether vs contains a decimal equal to v.
func containsDec(vs []decimal.Decimal, v decimal.Decimal) bool { return indexDec(vs, v) >= 0 }

// indexDec returns the first index in vs equal to v, or -1.
func indexDec(vs []decimal.Decimal, v decimal.Decimal) int {
	for i, x := range vs {
		if x.Equal(v) {
			return i
		}
	}
	return -1
}

// mismatch constructs a FlagQtyMismatch DraftFlag for line i.
func mismatch(i int, ln CheckLine) DraftFlag {
	return DraftFlag{
		Line:    i,
		Code:    FlagQtyMismatch,
		Message: fmt.Sprintf("「%s」草稿数量是 %s，和你说的不一致，请核对", ln.ProductName, ln.Qty.String()),
	}
}

// CheckHistoryQty flags lines exceeding histQtyFactor times recent historical sales volume.
func CheckHistoryQty(lines []CheckLine, maxByName map[string]decimal.Decimal) []DraftFlag {
	var flags []DraftFlag
	for i, ln := range lines {
		mx, ok := maxByName[strings.TrimSpace(ln.ProductName)]
		if !ok || !mx.IsPositive() {
			continue
		}
		if ln.Qty.GreaterThan(mx.Mul(decimal.NewFromInt(histQtyFactor))) {
			flags = append(flags, DraftFlag{
				Line: i,
				Code: FlagQtyOverHist,
				Message: fmt.Sprintf("「%s」数量 %s 超过近 %d 天销量 %s 的 %d 倍，请核对",
					ln.ProductName, ln.Qty.String(), histWindowDays, mx.String(), histQtyFactor),
			})
		}
	}
	return flags
}

// CheckPriceDeviation flags priced lines deviating by more than priceDeviationPct from lastByName.
func CheckPriceDeviation(lines []CheckLine, lastByName map[string]decimal.Decimal) []DraftFlag {
	var flags []DraftFlag
	hundred := decimal.NewFromInt(100)
	for i, ln := range lines {
		last, ok := lastByName[strings.TrimSpace(ln.ProductName)]
		if ln.UnitPrice == nil || !ok || !last.IsPositive() {
			continue
		}
		dev := ln.UnitPrice.Sub(last).Abs().Mul(hundred).Div(last)
		if dev.GreaterThan(priceDeviationPct) {
			flags = append(flags, DraftFlag{
				Line: i,
				Code: FlagPriceDeviates,
				Message: fmt.Sprintf("「%s」单价 %s 和上次的 %s 相差较大，请核对",
					ln.ProductName, ln.UnitPrice.String(), last.String()),
			})
		}
	}
	return flags
}

// CheckTotal flags a document whose priced lines do not sum to spokenTotal.
func CheckTotal(lines []CheckLine, spokenTotal decimal.Decimal) []DraftFlag {
	sum := decimal.Zero
	for _, ln := range lines {
		if ln.UnitPrice == nil {
			return nil
		}
		sum = sum.Add(ln.Qty.Mul(*ln.UnitPrice))
	}
	if sum.Round(2).Equal(spokenTotal.Round(2)) {
		return nil
	}
	return []DraftFlag{{
		Line:    -1,
		Code:    FlagTotalMismatch,
		Message: fmt.Sprintf("各行合计 %s，和你说的总额 %s 对不上，请逐行核对", sum.Round(2).String(), spokenTotal.Round(2).String()),
	}}
}

// SpokenTotal extracts the first money amount spoken after a total keyword.
func SpokenTotal(text string) (decimal.Decimal, bool) {
	for _, kw := range []string{"一共", "总共", "合计", "总价", "共计", "总额"} {
		if i := strings.Index(text, kw); i >= 0 {
			for _, q := range ExtractQuantities(text[i+len(kw):]) {
				if q.Money {
					return q.Value, true
				}
			}
		}
	}
	return decimal.Zero, false
}

// FlagsPayload converts DraftFlag values into the JSON-friendly slice stored on a plan.
func FlagsPayload(flags []DraftFlag) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(flags))
	for _, f := range flags {
		out = append(out, map[string]interface{}{"line": f.Line, "code": f.Code, "message": f.Message})
	}
	return out
}

// checkLines adapts purchaseDraftItem values into CheckLine rule inputs.
func checkLines(items []purchaseDraftItem) []CheckLine {
	out := make([]CheckLine, 0, len(items))
	for _, it := range items {
		out = append(out, CheckLine{ProductName: it.ProductName, Qty: decimal.NewFromFloat(it.Qty)})
	}
	return out
}
