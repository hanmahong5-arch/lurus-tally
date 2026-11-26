package ai

import (
	"sort"
	"strings"
)

// correctionWords lists words indicating the owner amended what was just spoken.
var correctionWords = []string{"不对", "改成", "改为", "换成", "错了", "算了", "不要了", "还是", "重新", "再加", "再来"}

// addWords lists incremental addition phrases requiring manual total verification.
var addWords = []string{"再加", "再来"}

// cancelWords lists phrases indicating the owner dropped the product just named.
var cancelWords = []string{"不要了", "不用了", "去掉", "删了", "删掉"}

// cancelFiller contains punctuation and filler characters allowed between a quantity and a cancel word.
const cancelFiller = "，,。；;、 \n哎呀啊嗯呃吧那这个就算了也都我们先还是暂时这次回今天再全部"

// cancelledLines returns draft line indices cancelled in speech without a subsequent quantity.
func cancelledLines(rs []rune, qs []Quantity, hits []nameHit, cand map[int][]Quantity, cancels []int) []int {
	var out []int
	seen := map[int]bool{}
	for _, p := range cancels {
		var last *Quantity
		for k := range qs {
			if qs[k].End <= p {
				last = &qs[k]
			}
		}
		if last == nil {
			continue
		}
		named := false
		for k := last.End; k < p; k++ {
			if strings.ContainsRune(cancelFiller, rs[k]) || inEarlierCancel(rs, cancels, p, k) {
				continue
			}
			inHit := false
			for _, h := range hits {
				if k >= h.start && k < h.end {
					inHit = true
					break
				}
			}
			if !inHit {
				named = true
				break
			}
		}
		if named {
			continue
		}
		if sweepsAll(rs, p) {
			for _, h := range hits {
				if h.end > p || seen[h.line] {
					continue
				}
				if !draftcheckHasQtyAfter(cand[h.line], p) {
					seen[h.line] = true
					out = append(out, h.line)
				}
			}
			continue
		}
		if h := lastHitBefore(hits, p); h != nil && h.end > last.End {
			if !draftcheckHasQtyAfter(cand[h.line], p) && !seen[h.line] {
				seen[h.line] = true
				out = append(out, h.line)
			}
			continue
		}
		for li, cs := range cand {
			owns, renewed := false, false
			for _, c := range cs {
				if c.Pos == last.Pos {
					owns = true
				}
				if c.Pos > p {
					renewed = true
				}
			}
			if owns && !renewed && !seen[li] {
				seen[li] = true
				out = append(out, li)
			}
		}
	}
	sort.Ints(out)
	return out
}

// draftcheckHasQtyAfter reports whether any attributed quantity starts after pos.
func draftcheckHasQtyAfter(cs []Quantity, pos int) bool {
	for _, c := range cs {
		if c.Pos > pos {
			return true
		}
	}
	return false
}

// inEarlierCancel reports whether rune k sits inside a cancel word starting before p.
func inEarlierCancel(rs []rune, cancels []int, p, k int) bool {
	for _, c := range cancels {
		if c >= p {
			continue
		}
		for _, w := range cancelWords {
			if hasRunePrefix(rs[c:], w) && k >= c && k < c+len([]rune(w)) {
				return true
			}
		}
	}
	return false
}

// sweepsAll reports whether the cancel word at p is preceded by 都 or 全部.
func sweepsAll(rs []rune, p int) bool {
	pre := string(rs[:p])
	return strings.HasSuffix(pre, "都") || strings.HasSuffix(pre, "全部")
}

// correctionPositions lists rune offsets where a correction word starts.
func correctionPositions(rs []rune) []int { return positionsOf(rs, correctionWords) }

// positionsOf lists rune offsets where any word in words starts, sorted ascending.
func positionsOf(rs []rune, words []string) []int {
	var out []int
	for _, w := range words {
		for i := 0; i+len([]rune(w)) <= len(rs); i++ {
			if hasRunePrefix(rs[i:], w) {
				out = append(out, i)
			}
		}
	}
	sort.Ints(out)
	return out
}

// amendedByAdding reports whether cs has quantities on both sides of an add word.
func amendedByAdding(cs []Quantity, adds []int) bool {
	for _, a := range adds {
		before, after := false, false
		for _, c := range cs {
			if c.Pos < a {
				before = true
			} else {
				after = true
			}
		}
		if before && after {
			return true
		}
	}
	return false
}

// turnStarts lists rune offsets where each newline-delimited message turn begins.
func turnStarts(rs []rune) []int {
	out := []int{0}
	for i, r := range rs {
		if r == '\n' {
			out = append(out, i+1)
		}
	}
	return out
}

// lastTurnOnly keeps only quantities spoken in the latest message turn containing any quantity.
func lastTurnOnly(cs []Quantity, turns []int) []Quantity {
	turnOf := func(pos int) int {
		t := 0
		for k, s := range turns {
			if pos >= s {
				t = k
			}
		}
		return t
	}
	last := -1
	for _, c := range cs {
		if t := turnOf(c.Pos); t > last {
			last = t
		}
	}
	var kept []Quantity
	for _, c := range cs {
		if turnOf(c.Pos) == last {
			kept = append(kept, c)
		}
	}
	return kept
}

// afterLastCorrection keeps only quantities spoken after the last correction word dividing cs.
func afterLastCorrection(cs []Quantity, corr []int) []Quantity {
	for k := len(corr) - 1; k >= 0; k-- {
		before, after := false, false
		for _, c := range cs {
			if c.Pos < corr[k] {
				before = true
			} else {
				after = true
			}
		}
		if before && after {
			var kept []Quantity
			for _, c := range cs {
				if c.Pos >= corr[k] {
					kept = append(kept, c)
				}
			}
			return kept
		}
	}
	return cs
}

// respectively captures a "分别" pairing between product names and following quantities.
type respectively struct {
	names []int
	qis   []int
}

// pairRespectively finds sentences containing "分别" with names before it and quantities after it.
func pairRespectively(rs []rune, qs []Quantity, hits []nameHit) []respectively {
	const stops = "。；;！!？?\n"
	var out []respectively
	for i := 0; i+2 <= len(rs); i++ {
		if !hasRunePrefix(rs[i:], "分别") {
			continue
		}
		lo, hi := 0, len(rs)
		for j := i - 1; j >= 0; j-- {
			if strings.ContainsRune(stops, rs[j]) {
				lo = j + 1
				break
			}
		}
		for j := i + 2; j < len(rs); j++ {
			if strings.ContainsRune(stops, rs[j]) {
				hi = j
				break
			}
		}
		var r respectively
		seen := map[int]bool{}
		for _, h := range hits {
			if h.start >= i+2 && h.start < hi {
				hi = h.start
				break
			}
		}
		for _, h := range hits {
			if h.start >= lo && h.end <= i && !seen[h.line] {
				seen[h.line] = true
				r.names = append(r.names, h.line)
			}
		}
		for qi, q := range qs {
			if q.Pos >= i+2 && q.Pos < hi && len(r.qis) < len(r.names) {
				r.qis = append(r.qis, qi)
			}
		}
		if len(r.names) > 0 && len(r.qis) > 0 {
			out = append(out, r)
		}
		i = hi
	}
	return out
}
