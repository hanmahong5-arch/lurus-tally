package ai

import (
	"context"
	"sort"
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
	name = strings.TrimSpace(name)
	if maskEverydayWords(name) != name { // 老顾客 / 小时 name nobody
		return ""
	}
	r := []rune(name)
	for _, s := range aliasSuffixes {
		if string(r) == s { // 老板 / 老师 are titles, not 老 + surname
			return ""
		}
	}
	for _, p := range aliasPrefixes {
		if len(r) == 2 && string(r[0]) == p && commonSurname(r[1]) {
			return string(r[1])
		}
	}
	for _, s := range aliasSuffixes {
		sr := []rune(s)
		if len(r) == 1+len(sr) && string(r[1:]) == s && commonSurname(r[0]) {
			return string(r[0])
		}
	}
	return ""
}

// aliasMention is one address form found in a message: the form as written
// ("老李") and the surname it names ("李").
type aliasMention struct {
	Form    string
	Surname string
}

// aliasSurnamesInText returns the address forms in text, in order of
// appearance, one per surname.
//
// A form counts only when its surname character is a common surname and it is
// not part of an everyday word. Chinese has no spaces, so "the surname is
// followed by a word boundary" is checked the only way it can be without a
// segmenter: the words that begin with 老/小/阿 + a surname character or put a
// surname character before 总/姐/哥 (老顾客, 小时, 小区, 提高总量) are listed
// and blanked out before the scan. Before, the scan took any character: with
// one customer surnamed 顾, "老顾客一律打九五折" was filed under that customer.
func aliasSurnamesInText(text string) []aliasMention {
	r := []rune(maskEverydayWords(text))
	seen := map[rune]bool{}
	var out []aliasMention
	add := func(s rune, form []rune) {
		if commonSurname(s) && !seen[s] {
			seen[s] = true
			out = append(out, aliasMention{Form: string(form), Surname: string(s)})
		}
	}
	for i := range r {
		for _, p := range aliasPrefixes {
			if string(r[i]) == p && i+1 < len(r) {
				add(r[i+1], r[i:i+2])
			}
		}
		for _, s := range aliasSuffixes {
			sr := []rune(s)
			if i > 0 && i+len(sr) <= len(r) && string(r[i:i+len(sr)]) == s {
				add(r[i-1], r[i-1:i+len(sr)])
				break // longest suffix first: 老板娘 is not also 老板
			}
		}
	}
	return out
}

// everydayWords begin with an address prefix followed by a surname character,
// or run a surname character into 总/姐/哥, without naming anyone. Each entry
// is here because the character after 老/小/阿 (or before the suffix) is in
// commonSurnames; words whose character is not a surname need no entry.
var everydayWords = []string{
	// 老 + surname character
	"老顾客", "老常客", "老关系", "老路子", "老毛病", "老方法", "老江湖", "老古董",
	// 小 + surname character
	"小时", "小区", "小包装", "小包裹", "小麦", "小米", "小单子", "小车", "小程序", "小项目",
	"小于", "小费", "小马扎", "小龙虾", "小白菜", "小黄鱼", "小苏打", "小金库", "小游戏",
	// 阿 + surname character
	"阿司匹林",
	// surname character + 总 (提高总销量, 汇总金额); not 总要/总得, which
	// also read as 李总要…
	"总共", "总计", "总是", "总额", "总价", "总数", "总量", "总体", "总店", "总部", "总算",
	"总销", "总成本", "总金额", "总库存", "总收入", "总账", "总表", "总结", "总和", "总之",
	// surname character + 姐/哥/叔/婶 as kinship words
	"姐姐", "哥哥", "叔叔", "婶婶",
}

// maskEverydayWords blanks every everydayWords occurrence (longest first) so
// no address form is read inside one.
func maskEverydayWords(text string) string {
	for _, w := range everydayWordsLongestFirst {
		if strings.Contains(text, w) {
			text = strings.ReplaceAll(text, w, strings.Repeat(" ", len([]rune(w))))
		}
	}
	return text
}

var everydayWordsLongestFirst = func() []string {
	ws := append([]string(nil), everydayWords...)
	sort.SliceStable(ws, func(i, j int) bool { return len([]rune(ws[i])) > len([]rune(ws[j])) })
	return ws
}()

// commonSurnames are the surnames an address form is read for. A character
// outside the list (街 in 老街口, 票 in 小票) names nobody, and every lookup it
// caused counted against maxAliasLookups.
const commonSurnames = "王李张刘陈杨黄赵吴周徐孙马朱胡郭何高林罗郑梁谢宋唐许韩冯邓曹彭曾肖田董袁潘于蒋蔡余杜叶程苏魏吕丁任沈姚卢姜崔钟谭陆汪范金石廖贾夏韦付傅方白邹孟熊秦邱江尹薛闫段雷侯龙史陶黎贺顾毛郝龚邵万钱严覃武戴莫孔向汤常温康施文牛樊葛邢安齐易乔伍庞颜倪庄聂章鲁岳翟殷詹申欧耿关兰焦俞左柳甘祝包宁尚符舒阮柯纪梅童凌毕单季裴霍涂成苗谷盛曲翁冉骆蓝路游辛靳管柴蒙鲍华喻祁蒲房滕屈饶解牟艾尤阳时穆农司卓古吉缪简车项连芦麦褚娄窦戚岑景党宫费卜冷晏席卫米柏宗瞿桂佟应臧闵苟邬边卞姬仇栾隋商刁沙荣巫寇桑郎甄丛仲虞敖巩佘池查麻苑迟邝区"

func commonSurname(r rune) bool {
	return strings.ContainsRune(commonSurnames, r)
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
// form. Exactly one customer across all surnames found, or nil; the form that
// found them is returned with them.
func resolveAliasInText(ctx context.Context, sm SurnameMatcher, tenantID uuid.UUID, text string) (*CustomerRef, string) {
	mentions := aliasSurnamesInText(text)
	if len(mentions) > maxAliasLookups {
		mentions = mentions[:maxAliasLookups]
	}
	found := map[uuid.UUID]CustomerRef{}
	var form string
	for _, m := range mentions {
		cands, err := sm.MatchCustomers(ctx, tenantID, m.Surname)
		if err != nil {
			return nil, ""
		}
		for _, c := range customersWithSurname(cands, m.Surname) {
			found[c.ID] = c
			form = m.Form
		}
	}
	if len(found) != 1 {
		return nil, ""
	}
	for _, c := range found {
		return &c, form
	}
	return nil, ""
}

// maxAliasLookups bounds the queries one message can cause.
const maxAliasLookups = 4
