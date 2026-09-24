package ai

import (
	"context"
	"strings"

	"github.com/google/uuid"
)

// How shop owners actually name regulars: by surname plus a form of address —
// 老张 / 小王 / 阿李, or 王老板 / 李总 / 周姐 / 赵哥 / 孙师傅. The customer record
// holds the full name (张三), so "老张上次买了什么" (PRD US-9.4's own example)
// matched nobody.

var aliasPrefixes = []string{"老", "小", "阿"}

// Longest first so 大姐 is tried before 姐.
var aliasSuffixes = []string{"老板娘", "老板", "师傅", "老师", "经理", "阿姨", "大姐", "大哥", "总", "姐", "哥", "叔", "婶"}

// aliasSurname returns the surname in a name that is exactly an address form
// ("老张" → "张", "王老板" → "王"), or "".
func aliasSurname(name string) string {
	r := []rune(strings.TrimSpace(name))
	for _, s := range aliasSuffixes {
		if string(r) == s { // 老板 / 老师 are titles, not 老 + surname
			return ""
		}
	}
	for _, p := range aliasPrefixes {
		if len(r) == 2 && string(r[0]) == p && isHan(r[1]) {
			return string(r[1])
		}
	}
	for _, s := range aliasSuffixes {
		sr := []rune(s)
		if len(r) == 1+len(sr) && string(r[1:]) == s && isHan(r[0]) {
			return string(r[0])
		}
	}
	return ""
}

// aliasSurnamesInText returns every surname an address form in text could
// refer to, in order of appearance, without duplicates. It over-collects on
// purpose ("小时" yields 时): only surnames that match exactly one customer
// are ever used, so noise that matches nobody is harmless.
func aliasSurnamesInText(text string) []string {
	r := []rune(text)
	seen := map[string]bool{}
	var out []string
	add := func(s rune) {
		if isHan(s) && !seen[string(s)] {
			seen[string(s)] = true
			out = append(out, string(s))
		}
	}
	for i := range r {
		for _, p := range aliasPrefixes {
			if string(r[i]) == p && i+1 < len(r) {
				add(r[i+1])
			}
		}
		for _, s := range aliasSuffixes {
			sr := []rune(s)
			if i > 0 && i+len(sr) <= len(r) && string(r[i:i+len(sr)]) == s {
				add(r[i-1])
			}
		}
	}
	return out
}

// customersWithSurname keeps the customer records (not walk-in names) whose
// name starts with surname.
func customersWithSurname(cands []CustomerRef, surname string) []CustomerRef {
	var out []CustomerRef
	for _, c := range cands {
		if c.ID != uuid.Nil && strings.HasPrefix(c.Name, surname) && len([]rune(c.Name)) > 1 {
			out = append(out, c)
		}
	}
	return out
}

// SurnameMatcher looks customers up by (part of) their name. The SQL sale repo
// implements it; ResolveCustomer uses it for address forms when the resolver
// also provides it.
type SurnameMatcher interface {
	MatchCustomers(ctx context.Context, tenantID uuid.UUID, name string) ([]CustomerRef, error)
}

// resolveAliasInText attributes text to a customer named only by an address
// form. Exactly one customer across all surnames found, or nil.
func resolveAliasInText(ctx context.Context, sm SurnameMatcher, tenantID uuid.UUID, text string) *CustomerRef {
	surnames := aliasSurnamesInText(text)
	if len(surnames) > maxAliasLookups {
		surnames = surnames[:maxAliasLookups]
	}
	found := map[uuid.UUID]CustomerRef{}
	for _, s := range surnames {
		cands, err := sm.MatchCustomers(ctx, tenantID, s)
		if err != nil {
			return nil
		}
		for _, c := range customersWithSurname(cands, s) {
			found[c.ID] = c
		}
	}
	if len(found) != 1 {
		return nil
	}
	for _, c := range found {
		return &c
	}
	return nil
}

// maxAliasLookups bounds the queries one message can cause.
const maxAliasLookups = 4

func isHan(r rune) bool {
	return r >= 0x4E00 && r <= 0x9FFF
}
