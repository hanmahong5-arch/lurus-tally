package invoice

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	domainbill "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/invoice"
)

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

type fakeRepo struct {
	partnerOK bool
	bills     map[uuid.UUID]BillRef
	invoices  map[uuid.UUID]*domain.Invoice
	nos       map[string]bool
}

func newFake() *fakeRepo {
	return &fakeRepo{partnerOK: true, bills: map[uuid.UUID]BillRef{}, invoices: map[uuid.UUID]*domain.Invoice{}, nos: map[string]bool{}}
}

func (f *fakeRepo) WithTx(_ context.Context, fn func(*sql.Tx) error) error { return fn(nil) }
func (f *fakeRepo) PartnerName(context.Context, *sql.Tx, uuid.UUID, uuid.UUID) (string, error) {
	if !f.partnerOK {
		return "", ErrNotFound
	}
	return "客户甲", nil
}
func (f *fakeRepo) LockBills(_ context.Context, _ *sql.Tx, _ uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]BillRef, error) {
	out := map[uuid.UUID]BillRef{}
	for _, id := range ids {
		if b, ok := f.bills[id]; ok {
			out[id] = b
		}
	}
	return out, nil
}
func (f *fakeRepo) Insert(_ context.Context, _ *sql.Tx, inv *domain.Invoice) error {
	k := string(inv.Direction) + "/" + inv.InvoiceNo
	if f.nos[k] {
		return ErrDuplicateNo
	}
	f.nos[k] = true
	cp := *inv
	f.invoices[inv.ID] = &cp
	return nil
}
func (f *fakeRepo) GetForUpdate(_ context.Context, _ *sql.Tx, _ uuid.UUID, id uuid.UUID) (*domain.Invoice, error) {
	inv, ok := f.invoices[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *inv
	return &cp, nil
}
func (f *fakeRepo) SetStatus(_ context.Context, _ *sql.Tx, _ uuid.UUID, id uuid.UUID, from, to domain.Status) (bool, error) {
	inv := f.invoices[id]
	if inv == nil || inv.Status != from {
		return false, nil
	}
	inv.Status = to
	return true, nil
}
func (f *fakeRepo) GetByID(_ context.Context, _ uuid.UUID, id uuid.UUID) (*domain.Invoice, error) {
	inv, ok := f.invoices[id]
	if !ok {
		return nil, ErrNotFound
	}
	return inv, nil
}
func (f *fakeRepo) List(context.Context, domain.ListFilter) ([]*domain.Invoice, int, error) {
	return nil, 0, nil
}
func (f *fakeRepo) Summarize(context.Context, domain.ListFilter) (domain.Summary, error) {
	return domain.Summary{}, nil
}

func baseInput(partner uuid.UUID, links ...domain.Link) domain.CreateInput {
	return domain.CreateInput{
		TenantID: uuid.New(), Direction: domain.DirectionOut, InvoiceNo: "N001", InvoiceType: "数电普票",
		PartnerID: partner, IssueDate: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		AmountExclTax: dec("100"), TaxAmount: dec("13"), Links: links,
	}
}

func approvedSale(f *fakeRepo, partner uuid.UUID, total, invoiced string) uuid.UUID {
	id := uuid.New()
	f.bills[id] = BillRef{ID: id, BillNo: "SO-1", SubType: domainbill.BillSubTypeSale, Status: domainbill.StatusApproved,
		PartnerID: &partner, TotalAmount: dec(total), Invoiced: dec(invoiced)}
	return id
}

func TestCreate_Success(t *testing.T) {
	f := newFake()
	partner := uuid.New()
	bill := approvedSale(f, partner, "113", "0")
	inv, err := NewCreateUseCase(f).Execute(context.Background(), baseInput(partner, domain.Link{BillID: bill, Amount: dec("113")}))
	if err != nil {
		t.Fatal(err)
	}
	if inv.Links[0].BillNo != "SO-1" || inv.PartnerName != "客户甲" {
		t.Fatalf("%+v", inv)
	}
}

func TestCreate_Rejections(t *testing.T) {
	partner := uuid.New()
	cases := []struct {
		name string
		kind error
		mut  func(f *fakeRepo, id uuid.UUID)
		amt  string
	}{
		{"over total", ErrRule, func(f *fakeRepo, id uuid.UUID) { b := f.bills[id]; b.TotalAmount = dec("100"); f.bills[id] = b }, "113"},
		{"cumulative over", ErrRule, func(f *fakeRepo, id uuid.UUID) { b := f.bills[id]; b.Invoiced = dec("50"); f.bills[id] = b }, "100"},
		{"draft", ErrRule, func(f *fakeRepo, id uuid.UUID) { b := f.bills[id]; b.Status = domainbill.StatusDraft; f.bills[id] = b }, "10"},
		{"wrong direction", ErrRule, func(f *fakeRepo, id uuid.UUID) {
			b := f.bills[id]
			b.SubType = domainbill.BillSubTypePurchase
			f.bills[id] = b
		}, "10"},
		{"wrong partner", ErrRule, func(f *fakeRepo, id uuid.UUID) { o := uuid.New(); b := f.bills[id]; b.PartnerID = &o; f.bills[id] = b }, "10"},
		{"foreign currency", ErrRule, func(f *fakeRepo, id uuid.UUID) { b := f.bills[id]; b.Currency = "USD"; f.bills[id] = b }, "10"},
		{"missing bill", ErrRule, func(f *fakeRepo, id uuid.UUID) { delete(f.bills, id) }, "10"},
	}
	for _, c := range cases {
		f := newFake()
		id := approvedSale(f, partner, "113", "0")
		if c.mut != nil {
			c.mut(f, id)
		}
		_, err := NewCreateUseCase(f).Execute(context.Background(), baseInput(partner, domain.Link{BillID: id, Amount: dec(c.amt)}))
		if !errors.Is(err, c.kind) {
			t.Errorf("%s: err=%v", c.name, err)
		}
		if msg, ok := MessageOf(err); !ok || msg == "" {
			t.Errorf("%s: no message", c.name)
		}
	}
}

func TestCreate_InvalidPartnerAndInput(t *testing.T) {
	f := newFake()
	f.partnerOK = false
	if _, err := NewCreateUseCase(f).Execute(context.Background(), baseInput(uuid.New())); !errors.Is(err, ErrRule) {
		t.Fatalf("partner: %v", err)
	}
	in := baseInput(uuid.New())
	in.InvoiceNo = ""
	if _, err := NewCreateUseCase(newFake()).Execute(context.Background(), in); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("input: %v", err)
	}
}

func TestCreate_DuplicateNo(t *testing.T) {
	f := newFake()
	p := uuid.New()
	uc := NewCreateUseCase(f)
	if _, err := uc.Execute(context.Background(), baseInput(p)); err != nil {
		t.Fatal(err)
	}
	if _, err := uc.Execute(context.Background(), baseInput(p)); !errors.Is(err, ErrDuplicateNo) {
		t.Fatalf("want duplicate, got %v", err)
	}
}

func TestVoidAndRed(t *testing.T) {
	f := newFake()
	p := uuid.New()
	inv, err := NewCreateUseCase(f).Execute(context.Background(), baseInput(p))
	if err != nil {
		t.Fatal(err)
	}
	tenant := inv.TenantID
	if _, err := NewVoidUseCase(f).Execute(context.Background(), tenant, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	red, err := NewRedUseCase(f).Execute(context.Background(), RedInput{TenantID: tenant, ID: inv.ID, InvoiceNo: "R001", IssueDate: inv.IssueDate})
	if err != nil {
		t.Fatal(err)
	}
	if red.RedOfID == nil || !red.TotalAmount.Equal(dec("-113")) || f.invoices[inv.ID].Status != domain.StatusRed {
		t.Fatalf("red not applied: %+v", red)
	}
	// second red / void on the struck original, and void of the red-letter, all conflict.
	if _, err := NewRedUseCase(f).Execute(context.Background(), RedInput{TenantID: tenant, ID: inv.ID, InvoiceNo: "R002", IssueDate: inv.IssueDate}); !errors.Is(err, ErrConflict) {
		t.Fatalf("re-red: %v", err)
	}
	if _, err := NewVoidUseCase(f).Execute(context.Background(), tenant, inv.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("void red'd: %v", err)
	}
	if _, err := NewVoidUseCase(f).Execute(context.Background(), tenant, red.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("void red-letter: %v", err)
	}
	// duplicate red number
	inv2, _ := NewCreateUseCase(f).Execute(context.Background(), func() domain.CreateInput { i := baseInput(p); i.InvoiceNo = "N002"; return i }())
	if _, err := NewRedUseCase(f).Execute(context.Background(), RedInput{TenantID: tenant, ID: inv2.ID, InvoiceNo: "R001", IssueDate: inv.IssueDate}); !errors.Is(err, ErrDuplicateNo) {
		t.Fatalf("dup red no: %v", err)
	}
}

func TestVoid_OK(t *testing.T) {
	f := newFake()
	inv, _ := NewCreateUseCase(f).Execute(context.Background(), baseInput(uuid.New()))
	out, err := NewVoidUseCase(f).Execute(context.Background(), inv.TenantID, inv.ID)
	if err != nil || out.Status != domain.StatusVoid {
		t.Fatalf("%v %+v", err, out)
	}
	if _, err := NewVoidUseCase(f).Execute(context.Background(), inv.TenantID, inv.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("double void: %v", err)
	}
}

func TestList_ClampsPaging(t *testing.T) {
	f := newFake()
	if _, err := NewListUseCase(f).Execute(context.Background(), domain.ListFilter{}); err == nil {
		t.Fatal("tenant required")
	}
	if _, err := NewListUseCase(f).Execute(context.Background(), domain.ListFilter{TenantID: uuid.New(), Limit: 9999}); err != nil {
		t.Fatal(err)
	}
}

// A sub-cent tail on the bill total (9.9999) must not block invoicing the full 9.99.
func TestCreate_SubCentTailTotal(t *testing.T) {
	f := newFake()
	partner := uuid.New()
	bill := approvedSale(f, partner, "9.9999", "0")
	in := baseInput(partner, domain.Link{BillID: bill, Amount: dec("9.99")})
	in.AmountExclTax, in.TaxAmount = dec("9.99"), dec("0")
	if _, err := NewCreateUseCase(f).Execute(context.Background(), in); err != nil {
		t.Fatalf("9.99 against total 9.9999 should be accepted: %v", err)
	}
	// ...but 10.00 is still over the cent-truncated total.
	in2 := baseInput(partner, domain.Link{BillID: bill, Amount: dec("10")})
	in2.InvoiceNo = "N002"
	in2.AmountExclTax, in2.TaxAmount = dec("10"), dec("0")
	if _, err := NewCreateUseCase(f).Execute(context.Background(), in2); !errors.Is(err, ErrRule) {
		t.Fatalf("10.00 should be rejected, got %v", err)
	}
}
