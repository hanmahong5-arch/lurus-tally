package intake

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestParseQR_CommaDigital(t *testing.T) {
	raw := "01,32,,24317000000012345678,113.00,20260315,ABCD,EE"
	got, ok := ParseQR(raw)
	if !ok {
		t.Fatal("expected a tax QR")
	}
	if got.InvoiceNo != "24317000000012345678" || got.InvoiceType != "数电普票" {
		t.Fatalf("no/type = %s %s", got.InvoiceNo, got.InvoiceType)
	}
	if got.IssueDate.Format("2006-01-02") != "2026-03-15" {
		t.Fatalf("date %s", got.IssueDate)
	}
	if got.Amount == nil || !got.Amount.Equal(decimal.RequireFromString("113.00")) {
		t.Fatalf("amount %v", got.Amount)
	}
	merged := ApplyQR(Parsed{MoneyOK: true, TotalAmount: "113.00", AmountExclTax: "100.00", TaxAmount: "13.00"}, got)
	if !merged.MoneyOK || merged.AmountExclTax != "100.00" {
		t.Fatalf("matching split should stay: %+v", merged)
	}
	blank := ApplyQR(Parsed{}, got)
	if blank.MoneyOK || blank.AmountExclTax != "" || blank.TotalAmount != "113.00" {
		t.Fatalf("lone QR amount must not invent tax: %+v", blank)
	}
}

func TestSniffQR_FromPDFBytes(t *testing.T) {
	body := []byte("%PDF-1.4 junk 01,32,,24317000000012345678,113.00,20260315,ABCD more")
	got := SniffQR(body)
	q, ok := ParseQR(got)
	if !ok || q.InvoiceNo != "24317000000012345678" {
		t.Fatalf("%q", got)
	}
	if SniffQR([]byte{0xff, 0xd8, 0xff}) != "" {
		t.Fatal("a jpeg without a tax QR must stay empty")
	}
	urlBody := []byte("see https://inv-veri.chinatax.gov.cn/veri?invoiceNo=24317000000012345678&amount=113.00&date=20260315 end")
	q, ok = ParseQR(SniffQR(urlBody))
	if !ok || q.InvoiceNo != "24317000000012345678" {
		t.Fatal("a verification URL inside a file should be read")
	}
}

func TestParseQR_RejectsBarcode(t *testing.T) {
	if _, ok := ParseQR("6901234567890"); ok {
		t.Fatal("a product barcode is not a tax QR")
	}
}

func TestParseQR_ChinaTaxURL(t *testing.T) {
	raw := "https://inv-veri.chinatax.gov.cn/v?invoiceNo=24317000000012345678&amount=20.00&date=20260102"
	got, ok := ParseQR(raw)
	if !ok || got.InvoiceNo != "24317000000012345678" || got.Amount == nil {
		t.Fatalf("%+v ok=%v", got, ok)
	}
	if got.IssueDate != time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC) {
		t.Fatalf("date %s", got.IssueDate)
	}
}

func TestApplyVision_ClearsBrokenMoney(t *testing.T) {
	p := ApplyVision(VisionRaw{
		InvoiceNo: "12345678", SellerTaxNo: "91310000AAA", Seen: "发票号码12345678 税号91310000AAA 金额100 税额13 合计999",
		AmountExclTax: "100.00", TaxAmount: "13.00", TotalAmount: "999.00",
		Lines: []ParsedLine{{Name: "不在图上", Qty: "1", UnitPrice: "5"}},
	})
	if p.MoneyOK || p.TotalAmount != "" || p.InvoiceNo != "12345678" || p.SellerTaxNo != "91310000AAA" {
		t.Fatalf("%+v", p)
	}
	if len(p.Lines) != 0 {
		t.Fatalf("line name was not on the page: %+v", p.Lines)
	}
	if len(p.Hints) == 0 {
		t.Fatal("expected a hint that money was cleared")
	}
}

func TestApplyVision_KeepsReconcilingAmounts(t *testing.T) {
	p := ApplyVision(VisionRaw{
		Seen: "100.00 13.00 113.00 红叶石楠", AmountExclTax: "100.00", TaxAmount: "13.00", TotalAmount: "113.00",
		Lines: []ParsedLine{{Name: "红叶石楠", Qty: "2", UnitPrice: "50"}},
	})
	if !p.MoneyOK || p.TotalAmount != "113.00" {
		t.Fatalf("%+v", p)
	}
	if len(p.Lines) != 1 || p.Lines[0].UnitPrice != "" {
		t.Fatalf("price 50 is not on the page: %+v", p.Lines)
	}
}

func TestMoneyReconciles(t *testing.T) {
	if !MoneyReconciles(decimal.RequireFromString("100"), decimal.RequireFromString("13"), decimal.RequireFromString("113")) {
		t.Fatal("100+13=113")
	}
	if MoneyReconciles(decimal.RequireFromString("100"), decimal.RequireFromString("13"), decimal.RequireFromString("112")) {
		t.Fatal("mismatch must fail")
	}
}

func TestMatchTranscript_NoPrice(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	lines := MatchTranscript("来十棵红叶石楠 2", map[string]uuid.UUID{"红叶石楠": id, "石楠": uuid.New()})
	if len(lines) != 1 || lines[0].ProductID != id.String() || lines[0].UnitPrice != "" {
		t.Fatalf("%+v", lines)
	}
	if lines[0].Qty != "2" {
		t.Fatalf("qty %q", lines[0].Qty)
	}
}
