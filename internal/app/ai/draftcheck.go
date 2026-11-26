package ai

import (
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
)

// Flag codes carried in DraftFlag.Code for confirmation card wording.
const (
	FlagQtyMismatch   = "qty_mismatch"
	FlagQtyInvalid    = "qty_invalid"
	FlagQtyOverHist   = "qty_over_hist"
	FlagPriceDeviates = "price_deviates"
	FlagTotalMismatch = "total_mismatch"
)

const (
	histQtyFactor  = 3
	histWindowDays = 90
)

// priceDeviationPct is the allowed distance from the last price, in percent.
var priceDeviationPct = decimal.NewFromInt(50)

// CheckLine is one line of a drafted document reduced to what the rules need.
type CheckLine struct {
	ProductName string
	Qty         decimal.Decimal
	UnitPrice   *decimal.Decimal
}

// DraftFlag is one rule violation, with Line=-1 for document-wide flags.
type DraftFlag struct {
	Line    int    `json:"line"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// CheckSpokenQuantities compares draft quantities with the numbers spoken by the owner.
func CheckSpokenQuantities(spoken string, lines []CheckLine) []DraftFlag {
	var flags []DraftFlag
	for i, ln := range lines {
		if !ln.Qty.IsPositive() {
			flags = append(flags, DraftFlag{
				Line:    i,
				Code:    FlagQtyInvalid,
				Message: fmt.Sprintf("「%s」的数量不是正数，请核对", ln.ProductName),
			})
		}
	}

	rs := []rune(spoken)
	qs := goodsQuantities(rs)
	if len(qs) == 0 {
		return flags
	}
	clauseOf := clauseIndex(rs)
	hits, unmatched := nameHits(rs, clauseOf, lines)
	qs = draftcheckFilterNameQuantities(qs, hits)

	forced := draftcheckForcedUnmatched(qs, hits, unmatched, clauseOf)
	paired, skipQ := draftcheckPairRespectively(rs, qs, hits, forced)

	corr := correctionPositions(rs)
	adds := positionsOf(rs, addWords)
	addWord := addWords[0]
	if !strings.Contains(spoken, addWords[0]) {
		addWord = addWords[1]
	}
	turns := turnStarts(rs)
	cand, claimed := draftcheckAttributeCandidates(rs, qs, skipQ, paired, hits, clauseOf, corr)

	notes := map[int]string{}
	for _, li := range cancelledLines(rs, qs, hits, cand, positionsOf(rs, cancelWords)) {
		forced[li] = true
		notes[li] = fmt.Sprintf("你说了「不要了」，请确认「%s」还要不要", lines[li].ProductName)
	}
	for li, cs := range cand {
		if amendedByAdding(cs, adds) {
			forced[li] = true
			if _, set := notes[li]; !set {
				notes[li] = fmt.Sprintf("你说了「%s」，请确认「%s」的 %s 是加完后的总数", addWord, lines[li].ProductName, lines[li].Qty.String())
			}
		}
		cand[li] = lastTurnOnly(afterLastCorrection(cs, corr), turns)
	}

	return append(flags, draftcheckEvaluateLines(lines, qs, claimed, cand, forced, notes)...)
}

// draftcheckFilterNameQuantities drops quantities located inside matched product names.
func draftcheckFilterNameQuantities(qs []Quantity, hits []nameHit) []Quantity {
	kept := qs[:0:0]
	for _, q := range qs {
		inName := false
		for _, h := range hits {
			if q.Pos >= h.start && q.Pos < h.end {
				inName = true
				break
			}
		}
		if !inName {
			kept = append(kept, q)
		}
	}
	return kept
}

// draftcheckForcedUnmatched flags clauses where a quantity precedes the first matched name while unmatched lines exist.
func draftcheckForcedUnmatched(qs []Quantity, hits []nameHit, unmatched []int, clauseOf []int) map[int]bool {
	forced := map[int]bool{}
	if len(unmatched) == 0 {
		return forced
	}
	first := map[int]int{}
	for _, h := range hits {
		if f, ok := first[h.clause]; !ok || h.start < f {
			first[h.clause] = h.start
		}
	}
	for _, q := range qs {
		if f, ok := first[clauseOf[q.Pos]]; ok && q.Pos < f {
			for _, h := range hits {
				if h.clause == clauseOf[q.Pos] {
					forced[h.line] = true
				}
			}
		}
	}
	if len(forced) > 0 {
		for _, li := range unmatched {
			forced[li] = true
		}
	}
	return forced
}

// draftcheckPairRespectively resolves "分别" pairings and marks mismatched sentences as forced.
func draftcheckPairRespectively(rs []rune, qs []Quantity, hits []nameHit, forced map[int]bool) (map[int]Quantity, []bool) {
	paired := map[int]Quantity{}
	skipQ := make([]bool, len(qs))
	for _, sp := range pairRespectively(rs, qs, hits) {
		if len(sp.names) != len(sp.qis) {
			for _, li := range sp.names {
				forced[li] = true
			}
			continue
		}
		for k, li := range sp.names {
			paired[li] = qs[sp.qis[k]]
		}
		for _, qi := range sp.qis {
			skipQ[qi] = true
		}
	}
	return paired, skipQ
}

// draftcheckAttributeCandidates attributes spoken quantities to draft lines and tracks claimed quantities.
func draftcheckAttributeCandidates(
	rs []rune,
	qs []Quantity,
	skipQ []bool,
	paired map[int]Quantity,
	hits []nameHit,
	clauseOf []int,
	corr []int,
) (map[int][]Quantity, []bool) {
	cand := make(map[int][]Quantity)
	claimed := make([]bool, len(qs))
	for qi, q := range qs {
		if skipQ[qi] {
			claimed[qi] = true
			continue
		}
		for _, li := range attribute(rs, qs, qi, hits, clauseOf, corr) {
			cand[li] = append(cand[li], q)
			claimed[qi] = true
		}
	}
	for li, q := range paired {
		cand[li] = append(cand[li], q)
	}
	return cand, claimed
}
