package ai

import (
	"sort"
	"strings"

	"github.com/shopspring/decimal"
)

// nameHit records one occurrence of a draft line's product name in spoken text.
type nameHit struct {
	start, end, clause, line int
}

// goodsQuantities returns spoken numbers representing quantities of goods rather than money or ordinals.
func goodsQuantities(rs []rune) []Quantity {
	all := ExtractQuantities(string(rs))
	var out []Quantity
	for qi, q := range all {
		if q.Money {
			continue
		}
		if q.Unit == "" && q.Pos > 0 {
			lo := q.Pos - 3
			if lo < 0 {
				lo = 0
			}
			if strings.ContainsAny(string(rs[lo:q.Pos]), "每价按@￥¥卖") {
				continue
			}
		}
		if isCountingWord(rs, q) || isPriceDenominator(rs, all, qi) {
			continue
		}
		out = append(out, q)
	}
	return out
}

// isCountingWord reports whether q counts kinds or batches rather than units of goods.
func isCountingWord(rs []rune, q Quantity) bool {
	if q.End < len(rs) && strings.ContainsRune("种样类款", rs[q.End]) {
		return true
	}
	if q.Unit == "批" {
		if q.Pos > 0 && rs[q.Pos-1] == '第' {
			return true
		}
		if q.End < len(rs) && rs[q.End] == '次' {
			return true
		}
	}
	return false
}

// isPriceDenominator reports whether q is a per-one unit denominator adjacent to a money amount.
func isPriceDenominator(rs []rune, all []Quantity, qi int) bool {
	q := all[qi]
	if q.Money || q.Unit == "" || !q.Value.Equal(decimal.NewFromInt(1)) {
		return false
	}
	other := false
	for _, o := range all[:qi] {
		if !o.Money {
			other = true
			break
		}
	}
	if !other {
		return false
	}
	for _, o := range all[:qi] {
		if !o.Money {
			continue
		}
		end := o.End
		if end < len(rs) && rs[end] == '钱' {
			end++
		}
		if end == q.Pos {
			return true
		}
	}
	for _, o := range all[qi+1:] {
		if o.Money && o.Pos == q.End {
			return true
		}
	}
	return false
}

// clauseIndex assigns a punctuation-delimited clause index to each rune offset.
func clauseIndex(rs []rune) []int {
	out := make([]int, len(rs))
	c := 0
	for i, r := range rs {
		out[i] = c
		if strings.ContainsRune("，,。；;：:！!？?\n", r) {
			c++
		}
	}
	return out
}

// nameHits locates product name occurrences in rs and returns non-nested hits plus unmatched line indices.
func nameHits(rs []rune, clauseOf []int, lines []CheckLine) ([]nameHit, []int) {
	var hits []nameHit
	var unmatched []int
	for li, ln := range lines {
		name := []rune(strings.TrimSpace(ln.ProductName))
		if len(name) == 0 {
			continue
		}
		found := false
		for _, cand := range nameCandidates(name, rs) {
			for i := 0; i+len(cand) <= len(rs); i++ {
				if hasRunePrefix(rs[i:], string(cand)) {
					hits = append(hits, nameHit{start: i, end: i + len(cand), clause: clauseOf[i], line: li})
					found = true
				}
			}
			if found {
				break
			}
		}
		if !found {
			unmatched = append(unmatched, li)
		}
	}
	var out []nameHit
	for _, h := range hits {
		nested := false
		for _, o := range hits {
			if o.end-o.start > h.end-h.start && o.start <= h.start && h.end <= o.end {
				nested = true
				break
			}
		}
		if !nested {
			out = append(out, h)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].start < out[b].start })
	return out, unmatched
}

// nameCandidates returns candidate rune slices for matching a catalogue product name in speech.
func nameCandidates(name, sentence []rune) [][]rune {
	cands := [][]rune{name}
	base := name
	for i, r := range name {
		if (r == '(' || r == '（') && i > 0 {
			base = name[:i]
			break
		}
	}
	if len(base) != len(name) {
		cands = append(cands, base)
	}
	best := lcsRunes(base, sentence)
	if len(best) >= 2 {
		cands = append(cands, best)
	}
	return cands
}

// lcsRunes computes the longest common non-numeral substring of a and b.
func lcsRunes(a, b []rune) []rune {
	bestLen, bestEnd := 0, 0
	prev := make([]int, len(b)+1)
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		for j := 1; j <= len(b); j++ {
			if a[i-1] == b[j-1] && !isNumRune(a[i-1]) {
				cur[j] = prev[j-1] + 1
				if cur[j] > bestLen {
					bestLen, bestEnd = cur[j], i
				}
			}
		}
		prev = cur
	}
	return a[bestEnd-bestLen : bestEnd]
}
