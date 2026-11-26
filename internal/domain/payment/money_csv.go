package payment

import (
	"encoding/csv"
	"regexp"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

var moneyZone = time.FixedZone("CST", 8*3600)

var accountRe = regexp.MustCompile(`账号[:：]\s*\[([^\]]+)\]`)

func accountBefore(rows [][]string, header int) string {
	for i := 0; i < header; i++ {
		if m := accountRe.FindStringSubmatch(strings.Join(rows[i], "")); len(m) == 2 {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

func stopMoneyRow(row []string) bool {
	if len(row) == 0 {
		return false
	}
	first := normCell(row[0])
	return strings.HasPrefix(first, "#") || strings.Contains(first, "总笔数") || strings.HasPrefix(first, "合计")
}

func readMoneyCSV(text string) ([][]string, error) {
	r := csv.NewReader(strings.NewReader(text))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	r.TrimLeadingSpace = true
	return r.ReadAll()
}

func col(header []string, name string) int {
	for i, h := range header {
		if strings.Contains(normCell(h), name) {
			return i
		}
	}
	return -1
}

func cell(row []string, i int) string {
	if i < 0 || i >= len(row) {
		return ""
	}
	return normCell(row[i])
}

func normCell(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "\uFEFF")
	s = strings.Trim(s, "`\"")
	return strings.TrimSpace(s)
}

func blankRow(row []string) bool {
	for _, c := range row {
		if normCell(c) != "" {
			return false
		}
	}
	return true
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func joinParts(parts ...string) string {
	var kept []string
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return clip(strings.Join(kept, " "), 512)
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func moneyCell(s string) (decimal.Decimal, bool) {
	s = strings.ReplaceAll(s, ",", "")
	s = strings.TrimPrefix(s, "¥")
	s = strings.TrimPrefix(s, "￥")
	s = strings.TrimSuffix(s, "元")
	s = strings.TrimSpace(s)
	if s == "" || s == "-" {
		return decimal.Zero, true
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Zero, false
	}
	return d, true
}

func walletRow(n int, header, row []string, idName, timeName, timeFallback, partyName, memoA, memoB, memoC, amountName, flowName, statusName string) (ParsedLine, bool, error) {
	status := cell(row, col(header, statusName))
	if status != "" && !strings.Contains(status, "成功") && !strings.EqualFold(status, "SUCCESS") {
		return ParsedLine{}, true, nil
	}
	when := cell(row, col(header, timeName))
	if when == "" {
		when = cell(row, col(header, timeFallback))
	}
	amt, ok := moneyCell(cell(row, col(header, amountName)))
	if ok && strings.Contains(cell(row, col(header, flowName)), "支") {
		amt = amt.Abs().Neg()
	} else if ok {
		amt = amt.Abs()
	}
	return finishMoneyRow(n, cell(row, col(header, idName)), when, cell(row, col(header, partyName)),
		joinParts(cell(row, col(header, memoA)), cell(row, col(header, memoB)), cell(row, col(header, memoC))), amt, ok)
}

func parseWhen(s string) (time.Time, bool) {
	if i := strings.IndexByte(s, '.'); i > 0 {
		s = s[:i]
	}
	for _, layout := range []string{
		"2006-01-02 15:04:05", "2006-01-02 15:04", "2006/01/02 15:04:05", "2006/01/02 15:04", "2006-01-02", "2006/01/02",
	} {
		if t, err := time.ParseInLocation(layout, s, moneyZone); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
