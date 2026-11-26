package intake

import (
	"strings"
	"unicode"

	"github.com/shopspring/decimal"
)

// VisionRaw is what a model claims to have seen. Money is kept only when each
// figure appears in Seen and the three amounts add up.
type VisionRaw struct {
	InvoiceNo     string
	InvoiceCode   string
	InvoiceType   string
	SellerTaxNo   string
	SellerName    string
	IssueDate     string
	AmountExclTax string
	TaxAmount     string
	TotalAmount   string
	Direction     string
	Seen          string
	Lines         []ParsedLine
}

// ApplyVision keeps a field only when Seen contains it. Broken money is cleared.
func ApplyVision(raw VisionRaw) Parsed {
	seen := normalizeSeen(raw.Seen)
	p := Parsed{
		InvoiceNo:   keepIfSeen(raw.InvoiceNo, seen),
		InvoiceCode: keepIfSeen(raw.InvoiceCode, seen),
		SellerName:  strings.TrimSpace(raw.SellerName),
		IssueDate:   keepIfSeen(raw.IssueDate, seen),
	}
	if raw.InvoiceType != "" {
		p.InvoiceType = strings.TrimSpace(raw.InvoiceType)
	}
	if d := strings.TrimSpace(raw.Direction); d == DirectionIn || d == DirectionOut {
		p.Direction = d
	}
	tax := strings.ToUpper(strings.TrimSpace(raw.SellerTaxNo))
	if tax != "" && strings.Contains(seen, normalizeSeen(tax)) {
		p.SellerTaxNo = tax
	}
	excl, taxAmt, total, ok := amountsFromSeen(raw.AmountExclTax, raw.TaxAmount, raw.TotalAmount, seen)
	if ok {
		p.AmountExclTax = excl
		p.TaxAmount = taxAmt
		p.TotalAmount = total
		p.MoneyOK = true
	}
	for _, ln := range raw.Lines {
		name := strings.TrimSpace(ln.Name)
		if name == "" || !strings.Contains(seen, normalizeSeen(name)) {
			continue
		}
		line := ParsedLine{Name: name, Qty: strings.TrimSpace(ln.Qty)}
		price := strings.TrimSpace(ln.UnitPrice)
		if price != "" && strings.Contains(seen, normalizeSeen(price)) {
			line.UnitPrice = price
		}
		p.Lines = append(p.Lines, line)
	}
	if !p.MoneyOK && (raw.AmountExclTax != "" || raw.TaxAmount != "" || raw.TotalAmount != "") {
		p.Hints = append(p.Hints, "识别到的金额对不齐，已留空，请按票面填写")
	}
	return p
}

func amountsFromSeen(excl, tax, total, seen string) (string, string, string, bool) {
	excl, tax, total = strings.TrimSpace(excl), strings.TrimSpace(tax), strings.TrimSpace(total)
	if excl == "" || tax == "" || total == "" {
		return "", "", "", false
	}
	if !strings.Contains(seen, normalizeSeen(excl)) || !strings.Contains(seen, normalizeSeen(tax)) || !strings.Contains(seen, normalizeSeen(total)) {
		return "", "", "", false
	}
	e, err1 := decimal.NewFromString(stripComma(excl))
	t, err2 := decimal.NewFromString(stripComma(tax))
	tot, err3 := decimal.NewFromString(stripComma(total))
	if err1 != nil || err2 != nil || err3 != nil || !e.Add(t).Equal(tot) || !tot.IsPositive() || t.IsNegative() || !e.IsPositive() {
		return "", "", "", false
	}
	return e.StringFixed(2), t.StringFixed(2), tot.StringFixed(2), true
}

// MoneyReconciles reports whether excl + tax equals total and the signs are legal.
func MoneyReconciles(excl, tax, total decimal.Decimal) bool {
	return excl.IsPositive() && !tax.IsNegative() && total.IsPositive() && excl.Add(tax).Equal(total)
}

func keepIfSeen(v, seen string) string {
	v = strings.TrimSpace(v)
	if v == "" || !strings.Contains(seen, normalizeSeen(v)) {
		return ""
	}
	return v
}

func stripComma(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), ",", "")
}

func normalizeSeen(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		if unicode.IsSpace(r) || r == ',' || r == '，' || r == '￥' || r == '¥' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
