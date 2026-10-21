// Package print lays out a sales receipt for a thermal printer. It is pure:
// no I/O, no clock, so the same input always gives the same bytes.
package print

import (
	"strings"
	"time"
)

// PaperWidth is the paper roll width in millimetres.
type PaperWidth int

const (
	Paper58 PaperWidth = 58
	Paper80 PaperWidth = 80
)

// Columns is how many half-width columns one line holds on this paper.
func (w PaperWidth) Columns() int {
	if w == Paper80 {
		return 48
	}
	return 32
}

// ParsePaper turns the stored "58" / "80" into a PaperWidth (anything else is 58).
func ParsePaper(s string) PaperWidth {
	if strings.TrimSpace(s) == "80" {
		return Paper80
	}
	return Paper58
}

// Line is one sold item. Qty, Price and Amount are pre-formatted decimal strings.
type Line struct {
	Name   string
	Qty    string
	Unit   string
	Price  string
	Amount string
}

// Receipt is everything printed on one sales receipt.
type Receipt struct {
	ShopName   string
	Phone      string
	Address    string
	BillNo     string
	OccurredAt time.Time
	Lines      []Line
	Total      string
	Paid       string
	Change     string
	PayMethod  string
	Footer     string
	Cashier    string
}

// RuneWidth is the display width of r: 2 for CJK and full-width forms, else 1.
func RuneWidth(r rune) int {
	switch {
	case r >= 0x1100 && r <= 0x115F,
		r >= 0x2E80 && r <= 0x303E,
		r >= 0x3041 && r <= 0x33FF,
		r >= 0x3400 && r <= 0x4DBF,
		r >= 0x4E00 && r <= 0x9FFF,
		r >= 0xA000 && r <= 0xA4CF,
		r >= 0xAC00 && r <= 0xD7A3,
		r >= 0xF900 && r <= 0xFAFF,
		r >= 0xFE30 && r <= 0xFE6F,
		r >= 0xFF00 && r <= 0xFF60,
		r >= 0xFFE0 && r <= 0xFFE6,
		r >= 0x20000 && r <= 0x3FFFD:
		return 2
	}
	return 1
}

// StrWidth is the display width of s in half-width columns.
func StrWidth(s string) int {
	n := 0
	for _, r := range s {
		n += RuneWidth(r)
	}
	return n
}

// wrap splits s into pieces no wider than max columns, never cutting a rune.
func wrap(s string, max int) []string {
	if max < 2 {
		max = 2
	}
	var out []string
	var cur strings.Builder
	w := 0
	for _, r := range s {
		rw := RuneWidth(r)
		if w+rw > max {
			out = append(out, cur.String())
			cur.Reset()
			w = 0
		}
		cur.WriteRune(r)
		w += rw
	}
	if cur.Len() > 0 || len(out) == 0 {
		out = append(out, cur.String())
	}
	return out
}

func padLeft(s string, w int) string {
	if d := w - StrWidth(s); d > 0 {
		return strings.Repeat(" ", d) + s
	}
	return s
}

func padRight(s string, w int) string {
	if d := w - StrWidth(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

func center(s string, cols int) string {
	if d := cols - StrWidth(s); d > 1 {
		return strings.Repeat(" ", d/2) + s
	}
	return s
}

// pair puts left at the left edge and right at the right edge of one line.
func pair(left, right string, cols int) string {
	gap := cols - StrWidth(left) - StrWidth(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

// Render lays the receipt out as plain text for the given paper. Every line
// ends in a newline. Each item is its name (wrapped) on its own lines, then
// "quantity+unit  price  amount" right-aligned on the next line.
func Render(r Receipt, width PaperWidth) string {
	cols := width.Columns()
	var b strings.Builder
	line := func(s string) { b.WriteString(s + "\n") }
	rule := strings.Repeat("-", cols)

	if r.ShopName != "" {
		for _, p := range wrap(r.ShopName, cols) {
			line(center(p, cols))
		}
	}
	if r.Phone != "" {
		line(center("电话："+r.Phone, cols))
	}
	if r.Address != "" {
		for _, p := range wrap(r.Address, cols) {
			line(center(p, cols))
		}
	}
	line(rule)
	line("单号：" + r.BillNo)
	if !r.OccurredAt.IsZero() {
		line("时间：" + r.OccurredAt.Format("2006-01-02 15:04"))
	}
	if r.Cashier != "" {
		line("收银：" + r.Cashier)
	}
	line(rule)

	// Column plan for the numbers row: qty | price | amount, right-aligned.
	qtyW, priceW, amtW := 7, 8, 9
	if cols >= 48 {
		qtyW, priceW, amtW = 10, 12, 14
	}
	lead := cols - qtyW - priceW - amtW
	line(padRight("商品", lead) + padLeft("数量", qtyW) + padLeft("单价", priceW) + padLeft("金额", amtW))
	for _, l := range r.Lines {
		for _, p := range wrap(l.Name, cols) {
			line(p)
		}
		line(strings.Repeat(" ", lead) + padLeft(l.Qty+l.Unit, qtyW) + padLeft(l.Price, priceW) + padLeft(l.Amount, amtW))
	}
	line(rule)
	line(pair("合计", r.Total, cols))
	if r.Paid != "" {
		line(pair("实收", r.Paid, cols))
	}
	if r.Change != "" {
		line(pair("找零", r.Change, cols))
	}
	if r.PayMethod != "" {
		line("付款：" + r.PayMethod)
	}
	line(rule)
	if r.Footer != "" {
		for _, p := range wrap(r.Footer, cols) {
			line(center(p, cols))
		}
	}
	return b.String()
}

// RenderFeie is Render wrapped in the cloud printer's markup: the shop name is
// centred and enlarged (CB), the total line is bold (B), and line breaks are
// BR tags. Layout width is unchanged, so the plain-text version stays the
// reference for alignment.
func RenderFeie(r Receipt, width PaperWidth) string {
	plain := Render(r, width)
	lines := strings.Split(strings.TrimRight(plain, "\n"), "\n")
	var b strings.Builder
	shopRows := 0
	if r.ShopName != "" {
		shopRows = len(wrap(r.ShopName, width.Columns()))
	}
	for i, ln := range lines {
		switch {
		case i < shopRows:
			b.WriteString("<CB>" + strings.TrimSpace(ln) + "</CB><BR>")
		case strings.HasPrefix(ln, "合计"):
			b.WriteString("<B>" + ln + "</B><BR>")
		default:
			b.WriteString(ln + "<BR>")
		}
	}
	b.WriteString("<BR><BR>")
	return b.String()
}

// TestPage is a short sample receipt for "print a test page".
func TestPage(shop string, at time.Time) Receipt {
	return Receipt{
		ShopName:   shop,
		BillNo:     "TEST-0001",
		OccurredAt: at,
		Lines:      []Line{{Name: "测试商品", Qty: "1", Unit: "个", Price: "1.00", Amount: "1.00"}},
		Total:      "1.00",
		Footer:     "打印机连接正常",
	}
}
