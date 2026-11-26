package intake

import (
	"regexp"
	"sort"
	"strings"

	"github.com/google/uuid"
)

var qtyAfterName = regexp.MustCompile(`^\s*(\d+(?:\.\d+)?)`)

// MatchTranscript finds catalog names inside text. It never fills a price.
func MatchTranscript(text string, names map[string]uuid.UUID) []ParsedLine {
	text = strings.TrimSpace(text)
	if text == "" || len(names) == 0 {
		return nil
	}
	type pair struct {
		name string
		id   uuid.UUID
	}
	ordered := make([]pair, 0, len(names))
	for name, id := range names {
		name = strings.TrimSpace(name)
		if name == "" || id == uuid.Nil {
			continue
		}
		ordered = append(ordered, pair{name, id})
	}
	sort.Slice(ordered, func(i, j int) bool { return len([]rune(ordered[i].name)) > len([]rune(ordered[j].name)) })
	var lines []ParsedLine
	type span struct{ start, end int }
	var taken []span
	overlaps := func(start, end int) bool {
		for _, s := range taken {
			if start < s.end && end > s.start {
				return true
			}
		}
		return false
	}
	for _, p := range ordered {
		idx := strings.Index(text, p.name)
		end := idx + len(p.name)
		if idx < 0 || overlaps(idx, end) {
			continue
		}
		taken = append(taken, span{idx, end})
		line := ParsedLine{Name: p.name, ProductID: p.id.String()}
		rest := text[end:]
		if m := qtyAfterName.FindStringSubmatch(rest); len(m) == 2 {
			line.Qty = m[1]
		}
		lines = append(lines, line)
	}
	return lines
}
