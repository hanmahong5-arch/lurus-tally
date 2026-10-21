package todo

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	"github.com/hanmahong5-arch/lurus-tally/internal/app/replenish"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

type fakeBills struct {
	// drafts[subType] and invoiced[status] feed ListBills; err fails the whole source.
	drafts   map[domain.BillSubType][]domain.BillHead
	total    map[domain.BillSubType]int64
	invoiced map[string][]domain.BillHead
	sumSales appbill.BillListSummary
	failSub  domain.BillSubType
	failInv  bool
}

func (f *fakeBills) ListBills(_ context.Context, q appbill.BillListFilter) ([]domain.BillHead, int64, error) {
	if q.InvoiceStatus != "" {
		if f.failInv {
			return nil, 0, errors.New("boom")
		}
		items := f.invoiced[q.InvoiceStatus]
		return items, int64(len(items)), nil
	}
	if q.SubType == f.failSub && f.failSub != "" {
		return nil, 0, errors.New("boom")
	}
	items := f.drafts[q.SubType]
	if len(items) > q.Size {
		items = items[:q.Size]
	}
	return items, f.total[q.SubType], nil
}

func (f *fakeBills) SummarizeBills(context.Context, appbill.BillListFilter) (appbill.BillListSummary, error) {
	return f.sumSales, nil
}

type fakeStock struct {
	rows []replenish.LowStockRow
	err  error
}

func (f fakeStock) Execute(context.Context, uuid.UUID, int) ([]replenish.LowStockRow, error) {
	return f.rows, f.err
}

type fakeRecv struct{ out *appbill.ReceivablesOutput }

func (f fakeRecv) Execute(context.Context, uuid.UUID, string, *time.Time) (*appbill.ReceivablesOutput, error) {
	return f.out, nil
}

type fakeKPI struct {
	ob    Onboarding
	obErr error
}

func (fakeKPI) Collected(context.Context, uuid.UUID, time.Time, time.Time) (decimal.Decimal, error) {
	return decimal.NewFromInt(7), nil
}
func (fakeKPI) Uninvoiced(context.Context, uuid.UUID, time.Time, time.Time) (decimal.Decimal, error) {
	return decimal.NewFromInt(9), nil
}

func (fakeKPI) HasData(context.Context, uuid.UUID) (bool, error) { return false, nil }

func (f fakeKPI) Onboarding(context.Context, uuid.UUID) (Onboarding, error) { return f.ob, f.obErr }

func heads(n int) []domain.BillHead {
	out := make([]domain.BillHead, n)
	for i := range out {
		out[i] = domain.BillHead{ID: uuid.New(), BillNo: "X", TotalAmount: decimal.NewFromInt(1), BillDate: time.Now()}
	}
	return out
}

func TestExecute_CountsAreRealTotalsNotPreviewSize(t *testing.T) {
	b := &fakeBills{
		drafts: map[domain.BillSubType][]domain.BillHead{domain.BillSubTypeSale: heads(6), domain.BillSubTypePurchase: heads(2)},
		total:  map[domain.BillSubType]int64{domain.BillSubTypeSale: 6, domain.BillSubTypePurchase: 2},
		invoiced: map[string][]domain.BillHead{
			"none": heads(3), "partial": heads(4),
		},
	}
	pid := uuid.New()
	recv := fakeRecv{out: &appbill.ReceivablesOutput{
		Items: []appbill.PartnerReceivable{
			{PartnerID: &pid, PartnerName: "A", Unpaid: decimal.NewFromInt(10), B0to30: decimal.NewFromInt(10)},
			{PartnerID: &pid, PartnerName: "B", Unpaid: decimal.NewFromInt(50), B31to60: decimal.NewFromInt(20), B90Plus: decimal.NewFromInt(30)},
		},
		Total: appbill.PartnerReceivable{Unpaid: decimal.NewFromInt(60)},
	}}
	uc := New(b, fakeStock{rows: make([]replenish.LowStockRow, 8)}, recv, fakeKPI{})
	s, err := uc.Execute(context.Background(), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if s.DraftSales.Count != 6 || len(s.DraftSales.Items) != 5 {
		t.Fatalf("draft sales: %+v", s.DraftSales.Count)
	}
	if s.DraftPurchases.Count != 2 {
		t.Fatalf("draft purchases: %d", s.DraftPurchases.Count)
	}
	if s.LowStock.Count != 8 || len(s.LowStock.Items) != 5 {
		t.Fatalf("low stock: %d/%d", s.LowStock.Count, len(s.LowStock.Items))
	}
	if s.UninvoicedSales.Count != 7 || len(s.UninvoicedSales.Items) != 5 {
		t.Fatalf("uninvoiced: %d/%d", s.UninvoicedSales.Count, len(s.UninvoicedSales.Items))
	}
	if s.OverdueCustomers.Count != 1 || s.OverdueCustomers.Items[0].PartnerName != "B" || !s.OverdueCustomers.Items[0].Overdue.Equal(decimal.NewFromInt(50)) {
		t.Fatalf("overdue: %+v", s.OverdueCustomers)
	}
	if s.Total != 6+2+8+7+1 {
		t.Fatalf("total: %d", s.Total)
	}
	if len(s.Failed) != 0 {
		t.Fatalf("failed: %v", s.Failed)
	}
	if s.HasData {
		t.Fatal("HasData should mirror the reader (false)")
	}
	if !s.KPIs.ReceivableUnpaid.Equal(decimal.NewFromInt(60)) || !s.KPIs.TodayCollected.Equal(decimal.NewFromInt(7)) || !s.KPIs.MonthUninvoiced.Equal(decimal.NewFromInt(9)) {
		t.Fatalf("kpis: %+v", s.KPIs)
	}
}

func TestExecute_SourceFailureIsReportedNotHidden(t *testing.T) {
	b := &fakeBills{failSub: domain.BillSubTypeSale, failInv: true}
	uc := New(b, fakeStock{err: errors.New("down")}, fakeRecv{out: &appbill.ReceivablesOutput{}}, fakeKPI{})
	s, err := uc.Execute(context.Background(), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{SrcDraftSales: true, SrcLowStock: true, SrcUninvoiced: true}
	if len(s.Failed) != len(want) {
		t.Fatalf("failed: %v", s.Failed)
	}
	for _, k := range s.Failed {
		if !want[k] {
			t.Fatalf("unexpected failed key %s", k)
		}
	}
}

func TestExecute_RequiresTenant(t *testing.T) {
	uc := New(&fakeBills{}, fakeStock{}, fakeRecv{}, fakeKPI{})
	if _, err := uc.Execute(context.Background(), uuid.Nil); err == nil {
		t.Fatal("want error")
	}
}

func TestExecute_TodaySalesIsNetOfReturns(t *testing.T) {
	b := &fakeBills{sumSales: appbill.BillListSummary{
		TotalAmount: decimal.NewFromInt(100), ReturnedAmount: decimal.NewFromInt(30),
	}}
	uc := New(b, fakeStock{}, fakeRecv{out: &appbill.ReceivablesOutput{}}, fakeKPI{})
	s, err := uc.Execute(context.Background(), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if !s.KPIs.TodaySales.Equal(decimal.NewFromInt(70)) {
		t.Fatalf("today sales = %s, want 70 (100 sold - 30 returned)", s.KPIs.TodaySales)
	}
}

func TestExecute_OnboardingPassesThrough(t *testing.T) {
	want := Onboarding{HasProduct: true, HasSale: true, HasDemo: true}
	uc := New(&fakeBills{}, fakeStock{}, fakeRecv{out: &appbill.ReceivablesOutput{}}, fakeKPI{ob: want})
	s, err := uc.Execute(context.Background(), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if s.Onboarding != want {
		t.Fatalf("onboarding = %+v, want %+v", s.Onboarding, want)
	}
	if len(s.Failed) != 0 {
		t.Fatalf("failed: %v", s.Failed)
	}
}

func TestExecute_OnboardingFailureIsReported(t *testing.T) {
	uc := New(&fakeBills{}, fakeStock{}, fakeRecv{out: &appbill.ReceivablesOutput{}}, fakeKPI{obErr: errors.New("down")})
	s, err := uc.Execute(context.Background(), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, k := range s.Failed {
		if k == SrcOnboarding {
			found = true
		}
	}
	if !found {
		t.Fatalf("failed = %v, want %s", s.Failed, SrcOnboarding)
	}
}

type fakeNeg struct {
	items []NegativeStockItem
	n     int
	err   error
}

func (f fakeNeg) NegativeStock(context.Context, uuid.UUID, int) ([]NegativeStockItem, int, error) {
	return f.items, f.n, f.err
}

func TestExecute_NegativeStockCountsIntoTotal(t *testing.T) {
	uc := New(&fakeBills{}, fakeStock{}, fakeRecv{out: &appbill.ReceivablesOutput{}}, fakeKPI{}).
		WithNegativeStock(fakeNeg{items: []NegativeStockItem{{ProductName: "水", WarehouseName: "主仓", OnHand: "-3"}}, n: 7})
	s, err := uc.Execute(context.Background(), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if s.NegativeStock.Count != 7 || len(s.NegativeStock.Items) != 1 || s.Total != 7 {
		t.Fatalf("got %+v total=%d", s.NegativeStock, s.Total)
	}
}

func TestExecute_NegativeStockFailureIsReported(t *testing.T) {
	uc := New(&fakeBills{}, fakeStock{}, fakeRecv{out: &appbill.ReceivablesOutput{}}, fakeKPI{}).
		WithNegativeStock(fakeNeg{err: errors.New("down")})
	s, _ := uc.Execute(context.Background(), uuid.New())
	found := false
	for _, f := range s.Failed {
		found = found || f == SrcNegativeStock
	}
	if !found || s.NegativeStock.Count != 0 {
		t.Fatalf("failed=%v", s.Failed)
	}
}
