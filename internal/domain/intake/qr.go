package intake

import (
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

var (
	qrInFile  = regexp.MustCompile(`01,(?:31|32|08|10|01|04),[^,\s]{0,30},[0-9]{8,30},[0-9]+(?:\.[0-9]{1,2})?,[0-9]{8}`)
	qrURLFile = regexp.MustCompile(`https?://[^\s<>"']+`)
)

// QRFields is the deterministic reading of a Chinese tax-invoice QR code.
// Amount is the single figure printed in the code. It is not a tax split.
type QRFields struct {
	InvoiceNo   string
	InvoiceCode string
	InvoiceType string
	IssueDate   time.Time
	Amount      *decimal.Decimal
	Raw         string
}

// SniffQR finds a national tax QR embedded in a PDF or text file.
func SniffQR(body []byte) string {
	if m := qrInFile.Find(body); m != nil {
		if _, ok := ParseQR(string(m)); ok {
			return string(m)
		}
	}
	for _, m := range qrURLFile.FindAll(body, 8) {
		if _, ok := ParseQR(string(m)); ok {
			return string(m)
		}
	}
	return ""
}

// ParseQR reads the national comma form (01,type,code,number,amount,yyyymmdd,...)
// or a chinatax verification URL. ok is false when the text is not a tax QR.
func ParseQR(raw string) (QRFields, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return QRFields{}, false
	}
	if strings.HasPrefix(raw, "01,") {
		return parseCommaQR(raw)
	}
	if strings.Contains(raw, "://") {
		return parseURLQR(raw)
	}
	return QRFields{}, false
}

func parseCommaQR(raw string) (QRFields, bool) {
	parts := strings.Split(raw, ",")
	if len(parts) < 6 || parts[0] != "01" {
		return QRFields{}, false
	}
	out := QRFields{
		Raw:         raw,
		InvoiceType: invoiceTypeName(parts[1]),
		InvoiceCode: strings.TrimSpace(parts[2]),
		InvoiceNo:   strings.TrimSpace(parts[3]),
	}
	if amt, err := decimal.NewFromString(strings.TrimSpace(parts[4])); err == nil {
		out.Amount = &amt
	}
	if len(parts) > 5 {
		if t, err := time.Parse("20060102", strings.TrimSpace(parts[5])); err == nil {
			out.IssueDate = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		}
	}
	if out.InvoiceNo == "" && out.Amount == nil {
		return QRFields{}, false
	}
	return out, true
}

func parseURLQR(raw string) (QRFields, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return QRFields{}, false
	}
	host := strings.ToLower(u.Host)
	if !strings.Contains(host, "chinatax") && !strings.Contains(host, "invoice") {
		return QRFields{}, false
	}
	q := u.Query()
	out := QRFields{Raw: raw, InvoiceNo: firstQuery(q, "invoiceNo", "fphm", "no")}
	if out.InvoiceNo == "" {
		return QRFields{}, false
	}
	out.InvoiceCode = firstQuery(q, "invoiceCode", "fpdm")
	if amt, err := decimal.NewFromString(firstQuery(q, "amount", "je", "jshj")); err == nil {
		out.Amount = &amt
	}
	if t, err := time.Parse("20060102", firstQuery(q, "date", "kprq")); err == nil {
		out.IssueDate = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	}
	return out, true
}

func firstQuery(q url.Values, keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(q.Get(k)); v != "" {
			return v
		}
	}
	return ""
}

func invoiceTypeName(code string) string {
	switch strings.TrimSpace(code) {
	case "31", "08":
		return "数电专票"
	case "32", "10":
		return "数电普票"
	case "01":
		return "纸质专票"
	case "04":
		return "纸质普票"
	default:
		return ""
	}
}

// ApplyQR copies deterministic QR fields over a parse. A lone QR amount never
// invents the tax split: the three money fields stay empty unless a previous
// reading already reconciled to that same total.
func ApplyQR(p Parsed, q QRFields) Parsed {
	if q.InvoiceNo != "" {
		p.InvoiceNo = q.InvoiceNo
	}
	if q.InvoiceCode != "" {
		p.InvoiceCode = q.InvoiceCode
	}
	if q.InvoiceType != "" {
		p.InvoiceType = q.InvoiceType
	}
	if !q.IssueDate.IsZero() {
		p.IssueDate = q.IssueDate.Format("2006-01-02")
	}
	if q.Amount == nil {
		return p
	}
	total := q.Amount.StringFixed(2)
	if p.MoneyOK && p.TotalAmount == total {
		return p
	}
	p.TotalAmount = total
	p.AmountExclTax = ""
	p.TaxAmount = ""
	p.MoneyOK = false
	p.Hints = append(p.Hints, "二维码只有价税合计，税额待填")
	return p
}
