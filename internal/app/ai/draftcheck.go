package ai

import (
	"fmt"
	"sort"
	"strings"

	domainai "github.com/hanmahong5-arch/lurus-tally/internal/domain/ai"
	"github.com/shopspring/decimal"
)

// draftcheck.go holds the deterministic rules run on a drafted document before
// the owner confirms it. Nothing here calls a model: a line that fails a rule
// is flagged and the confirmation card makes the owner tick that line.

// Flag codes carried in DraftFlag.Code (the card keys its wording off them).
const (
	FlagQtyMismatch   = "qty_mismatch"   // quantity differs from what was said
	FlagQtyInvalid    = "qty_invalid"    // zero or negative quantity
	FlagQtyOverHist   = "qty_over_hist"  // far above the product's biggest recent line
	FlagPriceDeviates = "price_deviates" // far from the last price paid
	FlagTotalMismatch = "total_mismatch" // lines do not add up to the spoken total
)

const (
	// histQtyFactor: a line above this multiple of the product's sales in the
	// last histWindowDays days is flagged.
	histQtyFactor = 3
	// histWindowDays is how far back the sales-volume rule looks.
	histWindowDays = 90
)

// priceDeviationPct is the allowed distance from the last price, in percent.
var priceDeviationPct = decimal.NewFromInt(50)

// CheckLine is one line of a drafted document, reduced to what the rules need.
type CheckLine struct {
	ProductName string
	Qty         decimal.Decimal
	// UnitPrice is nil when the draft carries no price (the purchase draft tool
	// takes name + quantity only).
	UnitPrice *decimal.Decimal
}

// DraftFlag is one rule violation. Line is the 0-based line index, or -1 for
// the document as a whole.
type DraftFlag struct {
	Line    int    `json:"line"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// CheckSpokenQuantities compares draft quantities with the numbers in what the
// owner said. Dictation usually carries no punctuation ("进可乐三箱雪碧五箱"),
// so each number is attributed to a product by position, not by clause: in the
// usual "name then number" order a number belongs to the nearest product named
// before it; in "number then name" order ("三箱可乐五箱雪碧") to the nearest one
// after it. "各X" covers every product named since the previous number. A line
// must match one of the numbers attributed to its product (numbers with a
// measure word win over bare ones, which may be prices). A product the sentence
// does not name falls back to the pool of numbers no product claimed. A sentence
// with no quantity at all (e.g. "帮我补货": quantities computed from stock, not
// dictated) is not checked.
func CheckSpokenQuantities(spoken string, lines []CheckLine) []DraftFlag {
	var flags []DraftFlag
	for i, ln := range lines {
		if !ln.Qty.IsPositive() {
			flags = append(flags, DraftFlag{Line: i, Code: FlagQtyInvalid,
				Message: fmt.Sprintf("「%s」的数量不是正数，请核对", ln.ProductName)})
		}
	}

	rs := []rune(spoken)
	qs := goodsQuantities(rs)
	if len(qs) == 0 {
		return flags
	}
	clauseOf := clauseIndex(rs)
	hits, unmatched := nameHits(rs, clauseOf, lines)
	// A number inside a product name ("M8螺丝") is part of the name.
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
	qs = kept

	// A draft line whose name is nowhere in the sentence may own a number that
	// precedes the first name of a clause; the order cannot be trusted then, so
	// every named line of such a clause is flagged rather than guessed at.
	forced := map[int]bool{}
	if len(unmatched) > 0 {
		first := map[int]int{} // clause -> start of its first name
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
	}

	if len(forced) > 0 {
		for _, li := range unmatched {
			forced[li] = true
		}
	}

	// "分别": names first, then the numbers in the same order. Pair them off by
	// position; when the counts disagree no pairing can be trusted, so every
	// named line of that sentence is flagged.
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

	corr := correctionPositions(rs)
	adds := positionsOf(rs, addWords)
	addWord := addWords[0]
	if !strings.Contains(spoken, addWords[0]) {
		addWord = addWords[1]
	}
	turns := turnStarts(rs)
	cand := make(map[int][]Quantity) // line -> numbers attributed to it
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
	// A product taken back ("可乐十箱，不要了") with no new number said for it
	// is no longer wanted: its line is flagged whatever the draft holds.
	notes := map[int]string{} // forced line -> why, when more specific than a mismatch
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

	var pooled []int // lines no number was attributed to
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
	// No number attributed to these lines: they draw from the unclaimed pool,
	// each plain number usable once. With two or more lines drawing from a pool
	// of different numbers, which line owns which cannot be told, so all of them
	// are flagged.
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

func distinctDecs(vs []decimal.Decimal) int {
	var seen []decimal.Decimal
	for _, v := range vs {
		if !containsDec(seen, v) {
			seen = append(seen, v)
		}
	}
	return len(seen)
}

// correctionWords: the owner takes back or amends what was just said. Only the
// number said after the last one counts for the line it amends ("三箱，不对，
// 五箱"; "三箱，算了，还是五箱").
var correctionWords = []string{"不对", "改成", "改为", "换成", "错了", "算了", "不要了", "还是", "重新", "再加", "再来"}

// addWords add to what was said ("三箱，再加两箱" means five): the draft may
// carry the total or the increment, so a line with a number on both sides of
// one is always flagged rather than guessed at.
var addWords = []string{"再加", "再来"}

// cancelWords: the owner drops the product just named. With no number said for
// it afterwards, the line should not be in the draft at all.
var cancelWords = []string{"不要了", "不用了", "去掉", "删了", "删掉"}

// cancelFiller: words and marks that may stand between a number and the cancel
// word without naming another product ("三箱，那个不要了", "五棵，算了不要了").
const cancelFiller = "，,。；;、 \n哎呀啊嗯呃吧那这个就算了也都我们先还是暂时这次回今天再全部"

// cancelledLines lists the draft lines whose last number is followed straight
// by a cancel word and which have no number said after it. When the words in
// between name a product that is not in the draft ("可乐三箱 然后雪碧不要了"),
// the cancel is about that product and no line is listed.
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
		// "都不要了": every draft line named so far without a number said after.
		if sweepsAll(rs, p) {
			for _, h := range hits {
				if h.end > p || seen[h.line] {
					continue
				}
				renewed := false
				for _, c := range cand[h.line] {
					if c.Pos > p {
						renewed = true
					}
				}
				if !renewed {
					seen[h.line] = true
					out = append(out, h.line)
				}
			}
			continue
		}
		// A draft product named between the last number and the cancel word is
		// the one taken back ("可乐十箱雪碧五箱，可乐不要了").
		if h := lastHitBefore(hits, p); h != nil && h.end > last.End {
			renewed := false
			for _, c := range cand[h.line] {
				if c.Pos > p {
					renewed = true
				}
			}
			if !renewed && !seen[h.line] {
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

// inEarlierCancel reports whether rune k sits inside a cancel word that starts
// before p ("雪碧不要了，可乐也不要了": the first one is not a product name).
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

// sweepsAll reports whether the cancel word at p is preceded by 都 / 全部,
// dropping everything just listed.
func sweepsAll(rs []rune, p int) bool {
	pre := string(rs[:p])
	return strings.HasSuffix(pre, "都") || strings.HasSuffix(pre, "全部")
}

// correctionPositions lists the rune offsets where a correction word starts.
func correctionPositions(rs []rune) []int { return positionsOf(rs, correctionWords) }

// positionsOf lists the rune offsets where any of words starts, in order.
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

// amendedByAdding reports whether one line has numbers both before and after an
// "add" word.
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

// turnStarts lists the rune offsets where each line of the spoken text starts:
// the orchestrator joins the owner's recent messages one per line.
func turnStarts(rs []rune) []int {
	out := []int{0}
	for i, r := range rs {
		if r == '\n' {
			out = append(out, i+1)
		}
	}
	return out
}

// lastTurnOnly keeps, of the numbers attributed to one line, only those said in
// the last message that has one: a product mentioned again in a later message
// takes that message's quantity, even without a correction word.
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

// afterLastCorrection keeps, of the numbers attributed to one line, only those
// said after the last correction word that has a number on both sides of it.
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

// respectively is one sentence with "分别": the names said before it and the
// numbers said after it.
type respectively struct {
	names []int // draft line per name, in spoken order
	qis   []int // indexes into the quantity slice, in spoken order
}

// pairRespectively finds each sentence holding "分别" with names before it and
// numbers after it.
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
		// The sentence ends where a draft product is named again after
		// "分别": numbers past that point belong to that product.
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
		// Numbers are taken one per name; any further ones are said about
		// other products ("可乐雪碧分别是十箱五箱，矿泉水三箱").
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

// nameHit is one occurrence of a draft line's product name in the sentence.
type nameHit struct {
	start, end, clause, line int
}

// goodsQuantities returns the spoken numbers that can be quantities of goods:
// no money amounts, no bare numbers sitting after a price word ("每棵三十",
// "单价三十"), no counting words ("两种", "第二批") and no per-unit price
// denominators ("三十块一棵", "三毛钱一个", "一个三毛钱").
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

// isCountingWord: "两种货", "三样", "第二批", "两批次" count kinds or rounds,
// not units of goods.
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

// isPriceDenominator: a "一" + measure word hugging a money amount is the "per
// one" of a unit price ("三十块一棵", "一个三毛钱"). Only when another goods
// quantity was said before it, so a lone "樱花一棵三十块" still checks the 1.
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
	// money immediately before ("三十块一棵", "三毛钱一个")
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
	// money immediately after ("一个三毛钱")
	for _, o := range all[qi+1:] {
		if o.Money && o.Pos == q.End {
			return true
		}
	}
	return false
}

// clauseIndex numbers the punctuation-delimited clauses, one entry per rune.
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

// nameHits finds every occurrence of the draft lines' product names. The draft
// often carries the catalogue name ("可口可乐", "樱花树", "螺丝(M8)") where the
// owner said a short form; when the full name is not in the sentence, the
// longest stretch of it that is (two characters at least) counts. An occurrence
// inside a longer name's occurrence ("樱花" inside "樱花树") is dropped so it
// cannot steal that product's number. The second result lists the lines whose
// name appears nowhere in the sentence.
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

// nameCandidates lists the strings to look for, best first: the full name, the
// name without a trailing "(spec)", then the longest common stretch of name
// and sentence (>= 2 runes, no numerals).
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

// lcsRunes is the longest common substring of a and b without numeral runes.
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

// attribute returns the draft lines that quantity qs[qi] belongs to.
func attribute(rs []rune, qs []Quantity, qi int, hits []nameHit, clauseOf []int, corr []int) []int {
	q := qs[qi]
	clause := clauseOf[q.Pos]
	var inClause []nameHit
	for _, h := range hits {
		if h.clause == clause {
			inClause = append(inClause, h)
		}
	}
	if len(inClause) == 0 {
		return attributeAcrossClauses(qs, qi, hits, clauseOf, corr)
	}
	// numberFirst: a number sits right in front of a product name ("三箱可乐",
	// "十箱的可乐") ahead of the clause's first name. A number merely preceding
	// the names (a count, a price) does not make the clause number-first.
	numberFirst := false
	for _, o := range qs {
		if clauseOf[o.Pos] != clause || o.Pos >= inClause[0].start {
			continue
		}
		for _, h := range inClause {
			if h.start == o.End || (h.start == o.End+1 && rs[o.End] == '的') {
				numberFirst = true
			}
		}
	}
	var out []int
	if q.Each {
		prevEnd, nextPos := -1, 1<<30
		for oi, o := range qs {
			if oi == qi || clauseOf[o.Pos] != clause {
				continue
			}
			if o.End <= q.Pos && o.End > prevEnd {
				prevEnd = o.End
			}
			if o.Pos >= q.End && o.Pos < nextPos {
				nextPos = o.Pos
			}
		}
		for _, h := range inClause {
			if !numberFirst && h.start >= prevEnd && h.end <= q.Pos {
				out = append(out, h.line)
			}
			if numberFirst && h.start >= q.End && h.start < nextPos {
				out = append(out, h.line)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	if numberFirst {
		for _, h := range inClause {
			if h.start >= q.End {
				return []int{h.line}
			}
		}
		// "三箱可乐改成五箱": a later number with no name after it amends the
		// name before it.
		if last := lastHitBefore(inClause, q.Pos); last != nil {
			return []int{last.line}
		}
		return nil
	}
	if last := lastHitBefore(inClause, q.Pos); last != nil {
		return []int{last.line}
	}
	return nil
}

// lastHitBefore is the last of hits (in spoken order) that ends by pos.
func lastHitBefore(hits []nameHit, pos int) *nameHit {
	var last *nameHit
	for i := range hits {
		if hits[i].end <= pos {
			last = &hits[i]
		}
	}
	return last
}

// attributeAcrossClauses places a number whose own clause names no product
// ("可乐，三箱，雪碧，五箱", "三箱，可乐，五箱，雪碧", "三箱，不对，五箱"). A
// dictation that opens with a number puts numbers before their names, so the
// nearest name after belongs to it; otherwise the nearest name before does. The
// name is taken only when its own clause carries no number yet, or a correction
// word stands between it and this number.
func attributeAcrossClauses(qs []Quantity, qi int, hits []nameHit, clauseOf []int, corr []int) []int {
	q := qs[qi]
	if len(hits) == 0 {
		return nil
	}
	hasQty := map[int]bool{}
	for _, o := range qs {
		hasQty[clauseOf[o.Pos]] = true
	}
	if qs[0].Pos < hits[0].start {
		for _, h := range hits {
			if h.start >= q.End {
				if !hasQty[h.clause] {
					return []int{h.line}
				}
				return nil
			}
		}
		// "来一箱可乐，不要了，来两箱": a nameless number said after a
		// correction amends the name before it.
		if last := lastHitBefore(hits, q.Pos); last != nil {
			for _, c := range corr {
				if c >= last.end && c < q.Pos {
					return []int{last.line}
				}
			}
		}
		return nil
	}
	last := lastHitBefore(hits, q.Pos)
	if last == nil {
		return nil
	}
	if !hasQty[last.clause] {
		return []int{last.line}
	}
	for _, c := range corr {
		if c >= last.end && c < q.Pos {
			return []int{last.line}
		}
	}
	return nil
}

// preferUnit keeps only the numbers that carry a measure word when there are
// any: a bare number next to "十棵" is more likely a price than a quantity.
func preferUnit(cs []Quantity) []Quantity {
	var withUnit []Quantity
	for _, c := range cs {
		if c.Unit != "" {
			withUnit = append(withUnit, c)
		}
	}
	if len(withUnit) > 0 {
		return withUnit
	}
	return cs
}

func mismatch(i int, ln CheckLine) DraftFlag {
	return DraftFlag{Line: i, Code: FlagQtyMismatch,
		Message: fmt.Sprintf("「%s」草稿数量是 %s，和你说的不一致，请核对", ln.ProductName, ln.Qty.String())}
}

// CheckHistoryQty flags lines above histQtyFactor x the product's reference
// quantity (its total sold in the last histWindowDays days). maxByName is keyed
// by trimmed product name; a product with no history is never flagged.
func CheckHistoryQty(lines []CheckLine, maxByName map[string]decimal.Decimal) []DraftFlag {
	var flags []DraftFlag
	for i, ln := range lines {
		mx, ok := maxByName[strings.TrimSpace(ln.ProductName)]
		if !ok || !mx.IsPositive() {
			continue
		}
		if ln.Qty.GreaterThan(mx.Mul(decimal.NewFromInt(histQtyFactor))) {
			flags = append(flags, DraftFlag{Line: i, Code: FlagQtyOverHist,
				Message: fmt.Sprintf("「%s」数量 %s 超过近 %d 天销量 %s 的 %d 倍，请核对",
					ln.ProductName, ln.Qty.String(), histWindowDays, mx.String(), histQtyFactor)})
		}
	}
	return flags
}

// CheckPriceDeviation flags priced lines more than 50% away from the product's
// last price. lastByName is keyed by trimmed product name; lines without a
// price, or products without a last price, are skipped.
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
			flags = append(flags, DraftFlag{Line: i, Code: FlagPriceDeviates,
				Message: fmt.Sprintf("「%s」单价 %s 和上次的 %s 相差较大，请核对",
					ln.ProductName, ln.UnitPrice.String(), last.String())})
		}
	}
	return flags
}

// CheckTotal flags a document whose priced lines do not add up to the total the
// owner said. Skipped when any line has no price.
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
	return []DraftFlag{{Line: -1, Code: FlagTotalMismatch,
		Message: fmt.Sprintf("各行合计 %s，和你说的总额 %s 对不上，请逐行核对", sum.Round(2).String(), spokenTotal.Round(2).String())}}
}

// SpokenTotal finds a stated document total ("一共五百元", "合计 1200 块") in
// text: the first money amount after a total keyword.
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

// FlagsPayload converts flags to the JSON-friendly shape stored on a plan.
func FlagsPayload(flags []DraftFlag) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(flags))
	for _, f := range flags {
		out = append(out, map[string]interface{}{"line": f.Line, "code": f.Code, "message": f.Message})
	}
	return out
}

func containsDec(vs []decimal.Decimal, v decimal.Decimal) bool { return indexDec(vs, v) >= 0 }

func indexDec(vs []decimal.Decimal, v decimal.Decimal) int {
	for i, x := range vs {
		if x.Equal(v) {
			return i
		}
	}
	return -1
}

// checkLines adapts purchase draft items to the rule input.
func checkLines(items []purchaseDraftItem) []CheckLine {
	out := make([]CheckLine, 0, len(items))
	for _, it := range items {
		out = append(out, CheckLine{ProductName: it.ProductName, Qty: decimal.NewFromFloat(it.Qty)})
	}
	return out
}

// flagMark is appended to the "after" cell of a flagged sample row.
const flagMark = "  ⚠ 需核对"

// descSuffixTail ends the "(N items need checking)" note added to a plan's
// description; it is how a second pass finds and replaces its own note.
const descSuffixTail = " 项需要逐行核对）"

// attachDraftFlags records flags on a plan so its confirmation card can mark
// the lines and require the owner to tick each one. Safe to call more than once
// (flags accumulate); a nil/empty flags slice leaves the plan untouched.
func attachDraftFlags(plan *domainai.Plan, flags []DraftFlag) {
	if plan == nil || len(flags) == 0 {
		return
	}
	existing, _ := plan.Payload["flags"].([]map[string]interface{})
	existing = append(existing, FlagsPayload(flags)...)
	plan.Payload["flags"] = existing
	plan.Payload["require_line_confirm"] = true

	for _, f := range flags {
		if f.Line >= 0 && f.Line < len(plan.Preview.SampleRows) {
			row := &plan.Preview.SampleRows[f.Line]
			if !strings.HasSuffix(row.After, flagMark) {
				row.After += flagMark
			}
		}
	}
	marked := 0
	for _, row := range plan.Preview.SampleRows {
		if strings.HasSuffix(row.After, flagMark) {
			marked++
		}
	}
	desc := plan.Preview.Description
	if i := strings.LastIndex(desc, "（"); i >= 0 && strings.HasSuffix(desc, descSuffixTail) {
		desc = desc[:i]
	}
	plan.Preview.Description = desc + fmt.Sprintf("（%d%s", max(marked, 1), descSuffixTail)
}

// checkPlanAgainstSpoken runs the spoken-quantity rule on a freshly proposed
// purchase draft and attaches any flags. Other plan types are left alone.
func checkPlanAgainstSpoken(plan *domainai.Plan, spoken string) {
	if plan == nil || plan.Type != domainai.PlanTypeCreatePurchase {
		return
	}
	items, ok := plan.Payload["items"].([]purchaseDraftItem)
	if !ok {
		return
	}
	attachDraftFlags(plan, CheckSpokenQuantities(spoken, checkLines(items)))
}

// crossCheckExtracted compares a draft with an independent reading of the same
// dictation (see Extractor): a draft line whose product matches an extracted
// line but whose quantity differs is flagged. Lines the extraction does not
// mention are left to the other rules. When the owner also said a total and
// the extraction priced every line, a total that does not add up flags the
// whole document (the purchase draft tool itself carries no price).
func crossCheckExtracted(plan *domainai.Plan, extracted []ExtractedLine, spoken string) {
	if plan == nil || plan.Type != domainai.PlanTypeCreatePurchase || len(extracted) == 0 {
		return
	}
	items, ok := plan.Payload["items"].([]purchaseDraftItem)
	if !ok {
		return
	}
	var flags []DraftFlag
	for i, ln := range checkLines(items) {
		name := strings.TrimSpace(ln.ProductName)
		if name == "" || !ln.Qty.IsPositive() || hasFlag(plan, i, FlagQtyMismatch) {
			continue
		}
		var same []ExtractedLine
		for _, e := range extracted {
			if strings.Contains(e.ProductName, name) || strings.Contains(name, e.ProductName) {
				same = append(same, e)
			}
		}
		if len(same) == 0 {
			continue
		}
		match := false
		for _, e := range same {
			if e.Qty.Equal(ln.Qty) {
				match = true
				break
			}
		}
		if !match {
			flags = append(flags, mismatch(i, ln))
		}
	}
	if total, ok := SpokenTotal(spoken); ok {
		priced := make([]CheckLine, 0, len(extracted))
		for _, e := range extracted {
			priced = append(priced, CheckLine(e))
		}
		flags = append(flags, CheckTotal(priced, total)...)
	}
	attachDraftFlags(plan, flags)
}

// hasFlag reports whether the plan already carries a flag of code on line.
func hasFlag(plan *domainai.Plan, line int, code string) bool {
	fs, _ := plan.Payload["flags"].([]map[string]interface{})
	for _, f := range fs {
		if f["line"] == line && f["code"] == code {
			return true
		}
	}
	return false
}

// flagsAcknowledged reports whether a plan may be confirmed: it needs no
// acknowledgement unless it requires line confirmation, in which case every
// flag index 0..n-1 must be listed. A plan that requires confirmation but whose
// flags cannot be read is refused rather than waved through.
func flagsAcknowledged(plan *domainai.Plan, acknowledged []int) bool {
	if req, _ := plan.Payload["require_line_confirm"].(bool); !req {
		return true
	}
	n := 0
	switch fs := plan.Payload["flags"].(type) {
	case []map[string]interface{}:
		n = len(fs)
	case []interface{}:
		n = len(fs)
	}
	if n == 0 {
		return false
	}
	got := make(map[int]bool, len(acknowledged))
	for _, i := range acknowledged {
		got[i] = true
	}
	for i := 0; i < n; i++ {
		if !got[i] {
			return false
		}
	}
	return true
}
