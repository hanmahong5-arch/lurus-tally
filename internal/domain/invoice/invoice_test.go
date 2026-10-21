package invoice

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func validInput() CreateInput {
	return CreateInput{
		TenantID: uuid.New(), Direction: DirectionOut, InvoiceNo: "12345678",
		InvoiceType: "数电普票", PartnerID: uuid.New(),
		IssueDate:     time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		AmountExclTax: d("100"), TaxAmount: d("13"),
	}
}

func TestNewInvoice_ComputesTotal(t *testing.T) {
	inv, err := NewInvoice(validInput(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !inv.TotalAmount.Equal(d("113")) || inv.Status != StatusIssued {
		t.Fatalf("total=%s status=%s", inv.TotalAmount, inv.Status)
	}
}

func TestNewInvoice_Rejects(t *testing.T) {
	long := strings.Repeat("a", 65)
	cases := map[string]func(*CreateInput){
		"direction": func(in *CreateInput) { in.Direction = "x" },
		"empty no":  func(in *CreateInput) { in.InvoiceNo = "  " },
		"long no":   func(in *CreateInput) { in.InvoiceNo = long },
		"type":      func(in *CreateInput) { in.InvoiceType = "收据" },
		"partner":   func(in *CreateInput) { in.PartnerID = uuid.Nil },
		"date":      func(in *CreateInput) { in.IssueDate = time.Time{} },
		"zero excl": func(in *CreateInput) { in.AmountExclTax = d("0") },
		"neg tax":   func(in *CreateInput) { in.TaxAmount = d("-1") },
		"scale":     func(in *CreateInput) { in.TaxAmount = d("0.00001") },
		"link zero": func(in *CreateInput) { in.Links = []Link{{BillID: uuid.New(), Amount: d("0")}} },
		"link nil":  func(in *CreateInput) { in.Links = []Link{{Amount: d("1")}} },
		"link over total": func(in *CreateInput) {
			in.Links = []Link{{BillID: uuid.New(), Amount: d("100")}, {BillID: uuid.New(), Amount: d("13.01")}}
		},
		"link dup": func(in *CreateInput) {
			id := uuid.New()
			in.Links = []Link{{BillID: id, Amount: d("1")}, {BillID: id, Amount: d("1")}}
		},
	}
	for name, mut := range cases {
		in := validInput()
		mut(&in)
		if _, err := NewInvoice(in, time.Now()); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestNewInvoice_LinksAtTotalOK(t *testing.T) {
	in := validInput()
	in.Links = []Link{{BillID: uuid.New(), Amount: d("100")}, {BillID: uuid.New(), Amount: d("13")}}
	if _, err := NewInvoice(in, time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestNewRed_NegatesAndLinksOrig(t *testing.T) {
	orig, _ := NewInvoice(validInput(), time.Now())
	red, err := NewRed(orig, "99999999", "", orig.IssueDate, "", nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if red.RedOfID == nil || *red.RedOfID != orig.ID || !red.TotalAmount.Equal(d("-113")) || !red.AmountExclTax.Equal(d("-100")) {
		t.Fatalf("bad red: %+v", red)
	}
	if _, err := NewRed(orig, " ", "", orig.IssueDate, "", nil, time.Now()); err == nil {
		t.Fatal("empty red no must fail")
	}
}

func TestCanVoidCanRed(t *testing.T) {
	id := uuid.New()
	cases := []struct {
		name     string
		inv      Invoice
		void, rd bool
	}{
		{"issued", Invoice{Status: StatusIssued}, true, true},
		{"void", Invoice{Status: StatusVoid}, false, false},
		{"red", Invoice{Status: StatusRed}, false, false},
		{"red letter", Invoice{Status: StatusIssued, RedOfID: &id}, false, false},
	}
	for _, c := range cases {
		if (c.inv.CanVoid() == nil) != c.void {
			t.Errorf("%s: CanVoid", c.name)
		}
		if (c.inv.CanRed() == nil) != c.rd {
			t.Errorf("%s: CanRed", c.name)
		}
	}
}
