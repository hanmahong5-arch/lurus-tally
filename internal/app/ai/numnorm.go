package ai

import (
	"strings"

	"github.com/shopspring/decimal"
)

// numnorm.go turns the numbers a shop owner dictates ("各十棵", "两箱半",
// "一千二") into decimals without a model. The orchestrator uses it to check
// a draft's quantities against what was actually said: arithmetic on spoken
// numerals is deterministic, so a model's reading of "一千二" as 12 must not
// be trusted when a few lines of code can say what the owner meant.

var cnDigit = map[rune]int64{
	'零': 0, '〇': 0, '一': 1, '二': 2, '两': 2, '三': 3, '四': 4,
	'五': 5, '六': 6, '七': 7, '八': 8, '九': 9,
}

var cnSmallUnit = map[rune]int64{'十': 10, '百': 100, '千': 1000}

var cnBigUnit = map[rune]int64{'万': 10000, '亿': 100000000}

var half = decimal.RequireFromString("0.5")

// ParseCNNumber parses one numeral token: Arabic ("12", "3.5", "3万"), Chinese
// ("二十五", "一百零五", "三点五"), or the colloquial forms where the last digit
// stands one unit below the unit before it ("一千二"=1200, "三百五"=350,
// "两万五"=25000). A bare "半" is 0.5. Returns false for anything else.
func ParseCNNumber(s string) (decimal.Decimal, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return decimal.Zero, false
	}
	if s == "半" {
		return half, true
	}
	if i := strings.IndexRune(s, '点'); i >= 0 {
		return parseCNDecimal(s[:i], s[i+len("点"):])
	}

	rs := []rune(s)
	var total, section, num decimal.Decimal
	var hasNum, zero bool
	var lastUnit int64
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case isASCIINumRune(r):
			j := i
			for j < len(rs) && isASCIINumRune(rs[j]) {
				j++
			}
			v, err := decimal.NewFromString(string(rs[i:j]))
			if err != nil || hasNum {
				return decimal.Zero, false
			}
			num, hasNum = v, true
			i = j - 1
		case r == '零' || r == '〇':
			if hasNum {
				return decimal.Zero, false
			}
			zero = true
		case isCNDigit(r):
			if hasNum {
				return decimal.Zero, false
			}
			num, hasNum = decimal.NewFromInt(cnDigit[r]), true
		case cnSmallUnit[r] != 0:
			u := cnSmallUnit[r]
			if lastUnit != 0 && lastUnit <= u && lastUnit < 10000 {
				return decimal.Zero, false
			}
			n := decimal.NewFromInt(1)
			if hasNum {
				n = num
			}
			section = section.Add(n.Mul(decimal.NewFromInt(u)))
			hasNum, zero, lastUnit = false, false, u
		case cnBigUnit[r] != 0:
			u := cnBigUnit[r]
			v := section
			if hasNum {
				v = v.Add(num)
			}
			if v.IsZero() {
				v = decimal.NewFromInt(1)
			}
			total = total.Add(v.Mul(decimal.NewFromInt(u)))
			section, hasNum, zero, lastUnit = decimal.Zero, false, false, u
		default:
			return decimal.Zero, false
		}
	}
	if hasNum {
		if zero || lastUnit == 0 || lastUnit == 10 {
			section = section.Add(num)
		} else {
			// 一千二 / 三百五 / 一万二: the trailing digit is one unit down.
			section = section.Add(num.Mul(decimal.NewFromInt(lastUnit / 10)))
		}
	}
	return total.Add(section), true
}

// parseCNDecimal handles "三点五": the integer part is a numeral, every rune
// of the fraction a single digit.
func parseCNDecimal(intPart, frac string) (decimal.Decimal, bool) {
	if frac == "" {
		return decimal.Zero, false
	}
	ip, ok := ParseCNNumber(intPart)
	if !ok {
		return decimal.Zero, false
	}
	var digits strings.Builder
	for _, r := range frac {
		switch {
		case r >= '0' && r <= '9':
			digits.WriteRune(r)
		case isCNDigit(r):
			digits.WriteByte(byte('0' + cnDigit[r]))
		default:
			return decimal.Zero, false
		}
	}
	f, err := decimal.NewFromString("0." + digits.String())
	if err != nil {
		return decimal.Zero, false
	}
	return ip.Add(f), true
}

func isASCIINumRune(r rune) bool { return (r >= '0' && r <= '9') || r == '.' }

func isCNDigit(r rune) bool { _, ok := cnDigit[r]; return ok }

// isNumRune is a rune that may be part of a numeral token ("点" and "." are
// handled by the tokenizer because they only count between two numerals).
func isNumRune(r rune) bool {
	if isCNDigit(r) || cnSmallUnit[r] != 0 || cnBigUnit[r] != 0 {
		return true
	}
	return r >= '0' && r <= '9'
}

// Quantity is one number found in a spoken sentence.
type Quantity struct {
	Value decimal.Decimal
	Unit  string // measure word right after the number; "" when none
	// Each marks "各X": the number applies to every item named in the sentence.
	Each bool
	// Money marks an amount of money (块/元/毛/角), not a quantity of goods.
	Money bool
	Raw   string
	// Pos and End are the rune offsets of the token (number and measure word)
	// in the text it was read from.
	Pos, End int
}

// measureUnits are the measure words recognised after a number, longest first
// where one is a prefix of another.
var measureUnits = []string{
	"公斤", "千克", "kg", "KG", "公里", "平方",
	"棵", "株", "箱", "个", "件", "包", "袋", "瓶", "条", "根", "只", "盒", "把",
	"套", "台", "米", "桶", "块", "张", "双", "对", "盆", "支", "卷", "片", "捆",
	"本", "批", "组", "粒", "颗", "份", "头", "辆", "斤", "吨", "箩", "筐", "垛", "扎",
	"元", "圆", "毛", "角",
}

// notQuantityUnits follow numbers that are dates, times or ordinals.
var notQuantityUnits = []string{"天", "日", "月", "年", "号", "周", "点", "时", "分", "秒", "岁", "楼", "层", "%", "％"}

// ExtractQuantities returns the numbers in text in order of appearance, with
// their measure word, "各" and money flags. It errs on the side of reporting a
// number (an unnecessary line to confirm costs a click; a missed one costs a
// wrong stock count).
func ExtractQuantities(text string) []Quantity {
	rs := []rune(text)
	var out []Quantity
	for i := 0; i < len(rs); {
		r := rs[i]

		// "半箱": a half with no number before it.
		if r == '半' && (i == 0 || !isNumRune(rs[i-1])) {
			u := matchUnit(rs[i+1:])
			if u == "" {
				i++
				continue
			}
			end := i + 1 + len([]rune(u))
			out = append(out, Quantity{Value: half, Unit: u, Each: i > 0 && rs[i-1] == '各', Raw: string(rs[i:end]), Money: isMoneyUnit(u, safeTail(rs, end)), Pos: i, End: end})
			i = end
			continue
		}
		if !isNumRune(r) {
			i++
			continue
		}

		j := i
		for j < len(rs) {
			if isNumRune(rs[j]) {
				j++
				continue
			}
			if (rs[j] == '点' || rs[j] == '.') && j+1 < len(rs) && isNumRune(rs[j+1]) && j > i {
				j++
				continue
			}
			break
		}
		tok := string(rs[i:j])
		each := i > 0 && rs[i-1] == '各'
		start := i
		i = j

		unit := ""
		v, ok := ParseCNNumber(tok)
		if !ok && strings.HasSuffix(tok, "两") {
			// 三两 (weight): the trailing 两 is the unit, not a second digit.
			if pv, pok := ParseCNNumber(strings.TrimSuffix(tok, "两")); pok {
				v, ok, unit = pv, true, "两"
			}
		}
		if !ok {
			continue
		}
		end := j
		if unit == "" {
			if hasAnyPrefix(rs[j:], notQuantityUnits) {
				continue
			}
			if len(tok) >= 7 && isAllASCIIDigits(tok) { // phone / order numbers
				continue
			}
			unit = matchUnit(rs[j:])
			if unit == "个" && j+1 < len(rs) && rs[j+1] == '月' {
				continue // 三个月
			}
			end = j + len([]rune(unit))
			// 一共/一起/一些/一定…: "一" opening a word, not a quantity.
			if unit == "" && tok == "一" && j < len(rs) && strings.ContainsRune("共起些样定直切般边下会旦面方向路块", rs[j]) {
				continue
			}
		}
		money := isMoneyUnit(unit, safeTail(rs, end))
		if unit != "" && end < len(rs) && rs[end] == '半' && !money {
			v = v.Add(half)
			end++
		}
		out = append(out, Quantity{Value: v, Unit: unit, Each: each, Money: money, Raw: string(rs[start:end]), Pos: start, End: end})
		i = end
	}
	return out
}

func matchUnit(rs []rune) string {
	for _, u := range measureUnits {
		if hasRunePrefix(rs, u) {
			return u
		}
	}
	return ""
}

func hasRunePrefix(rs []rune, p string) bool {
	pr := []rune(p)
	if len(rs) < len(pr) {
		return false
	}
	for i, c := range pr {
		if rs[i] != c {
			return false
		}
	}
	return true
}

func hasAnyPrefix(rs []rune, ps []string) bool {
	for _, p := range ps {
		if hasRunePrefix(rs, p) {
			return true
		}
	}
	return false
}

func safeTail(rs []rune, from int) []rune {
	if from >= len(rs) {
		return nil
	}
	return rs[from:]
}

func isAllASCIIDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return s != ""
}

// isMoneyUnit decides whether a number followed by unit is an amount of money.
// 元/圆/毛/角 always are. 块 is money unless a goods noun follows directly
// ("三块板"); "三十块", "三十块钱", "三十块一棵" are money.
func isMoneyUnit(unit string, rest []rune) bool {
	switch unit {
	case "元", "圆", "毛", "角":
		return true
	case "块":
		if len(rest) == 0 {
			return true
		}
		n := rest[0]
		if n == '钱' || n == '一' || n == '的' || n == '每' {
			return true
		}
		return n < 0x4e00 || n > 0x9fff // punctuation, digits, spaces
	}
	return false
}
