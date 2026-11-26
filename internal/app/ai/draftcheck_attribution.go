package ai

// attribute returns the draft line indices that quantity qs[qi] belongs to.
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

// lastHitBefore returns the last nameHit ending at or before pos.
func lastHitBefore(hits []nameHit, pos int) *nameHit {
	var last *nameHit
	for i := range hits {
		if hits[i].end <= pos {
			last = &hits[i]
		}
	}
	return last
}

// attributeAcrossClauses attributes a quantity whose own clause contains no product name.
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

// preferUnit retains only quantities carrying a measure word when any exist.
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
