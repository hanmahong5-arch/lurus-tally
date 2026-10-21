// Package todo aggregates the "needs your attention" counts shown on the todo
// page, the dashboard and the sidebar badge: bills waiting for review, low
// stock, customers owing for over 30 days, and approved sales not fully
// invoiced. Every count is the real total (not the preview size), and a failing
// data source is reported in Summary.Failed instead of being rendered as zero,
// so the UI never shows a false "all clear".
package todo

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appbill "github.com/hanmahong5-arch/lurus-tally/internal/app/bill"
	"github.com/hanmahong5-arch/lurus-tally/internal/app/replenish"
	domain "github.com/hanmahong5-arch/lurus-tally/internal/domain/bill"
)

// PreviewSize is how many rows each section carries next to its total count.
const PreviewSize = 5

// Source keys reported in Summary.Failed.
const (
	SrcDraftPurchases = "draft_purchases"
	SrcDraftSales     = "draft_sales"
	SrcLowStock       = "low_stock"
	SrcOverdue        = "overdue_customers"
	SrcUninvoiced     = "uninvoiced_sales"
	SrcKPIs           = "kpis"
	SrcOnboarding     = "onboarding"
	SrcNegativeStock  = "negative_stock"
)

// cst cuts "today" / "this month" at Asia/Shanghai wall-clock; a fixed zone
// avoids LoadLocation, which needs tzdata that scratch images lack.
var cst = time.FixedZone("CST", 8*3600)

// BillReader is the slice of the bill repo the todo lists need.
type BillReader interface {
	ListBills(ctx context.Context, f appbill.BillListFilter) ([]domain.BillHead, int64, error)
	SummarizeBills(ctx context.Context, f appbill.BillListFilter) (appbill.BillListSummary, error)
}

// LowStockReader lists products at or below their reorder point.
type LowStockReader interface {
	Execute(ctx context.Context, tenantID uuid.UUID, limit int) ([]replenish.LowStockRow, error)
}

// ReceivablesReader returns per-customer open balances split by age.
type ReceivablesReader interface {
	Execute(ctx context.Context, tenantID uuid.UUID, direction string, asOf *time.Time) (*appbill.ReceivablesOutput, error)
}

// KPIReader holds the two dashboard figures no existing use case exposes.
type KPIReader interface {
	// Collected is the sale money received in [from, to).
	Collected(ctx context.Context, tenantID uuid.UUID, from, to time.Time) (decimal.Decimal, error)
	// Uninvoiced is the not-yet-invoiced amount of approved sales dated in [from, to).
	Uninvoiced(ctx context.Context, tenantID uuid.UUID, from, to time.Time) (decimal.Decimal, error)
	// HasData reports whether the tenant has any product or bill yet.
	HasData(ctx context.Context, tenantID uuid.UUID) (bool, error)
	// Onboarding reports which getting-started steps the tenant has really done.
	Onboarding(ctx context.Context, tenantID uuid.UUID) (Onboarding, error)
}

// Onboarding is the getting-started progress. The Has* steps count only the
// tenant's own records: demo rows (remark "DEMO") never complete a step, so
// seeding sample data cannot hide the checklist. HasDemo tells the UI whether
// sample data is still around to be cleared.
type Onboarding struct {
	HasProduct  bool
	HasPurchase bool
	HasSale     bool
	HasPayment  bool
	HasDemo     bool
}

// NegativeStockReader lists stock rows whose on-hand quantity went below zero
// (typically offline sales synced before the matching purchase). It returns at
// most limit rows plus the real total.
type NegativeStockReader interface {
	NegativeStock(ctx context.Context, tenantID uuid.UUID, limit int) ([]NegativeStockItem, int, error)
}

// NegativeStockItem is one product/warehouse pair with a negative on-hand quantity.
type NegativeStockItem struct {
	ProductID     uuid.UUID
	ProductName   string
	WarehouseName string
	OnHand        string
}

// NegativeStockSection pairs a real total with the (capped) negative-stock rows.
type NegativeStockSection struct {
	Count int
	Items []NegativeStockItem
}

// NegativeStockLimit caps the rows returned for the negative-stock list.
const NegativeStockLimit = 50

// BillItem is one bill preview row.
type BillItem struct {
	ID          uuid.UUID
	BillNo      string
	PartnerName string
	TotalAmount decimal.Decimal
	BillDate    time.Time
}

// StockItem is one low-stock preview row (quantities are decimal strings).
type StockItem struct {
	ProductID    uuid.UUID
	ProductName  string
	AvailableQty string
	ReorderPoint string
	DaysOfSupply string
}

// DebtorItem is one customer owing for more than 30 days.
type DebtorItem struct {
	PartnerID      *uuid.UUID
	PartnerName    string
	Overdue        decimal.Decimal
	Unpaid         decimal.Decimal
	OldestBillDate *time.Time
}

// BillSection pairs a real total with a short bill preview.
type BillSection struct {
	Count int
	Items []BillItem
}

// StockSection pairs a real total with a short low-stock preview.
type StockSection struct {
	Count int
	Items []StockItem
}

// DebtorSection pairs a real total with a short overdue-customer preview.
type DebtorSection struct {
	Count int
	Items []DebtorItem
}

// KPIs are the four dashboard figures.
type KPIs struct {
	TodaySales       decimal.Decimal
	TodayCollected   decimal.Decimal
	ReceivableUnpaid decimal.Decimal
	MonthUninvoiced  decimal.Decimal
}

// Summary is the full todo payload.
type Summary struct {
	DraftPurchases   BillSection
	DraftSales       BillSection
	LowStock         StockSection
	OverdueCustomers DebtorSection
	UninvoicedSales  BillSection
	NegativeStock    NegativeStockSection
	KPIs             KPIs
	// Total is the sum of the six section counts (the sidebar badge).
	Total int
	// HasData is false only for a brand-new tenant with no product and no bill
	// (the dashboard then shows the getting-started steps). Unknown reads as true.
	HasData bool
	// Onboarding is the getting-started progress (demo data excluded).
	Onboarding Onboarding
	// Failed lists the source keys that could not be read; their sections are empty.
	Failed      []string
	GeneratedAt time.Time
}

// UseCase builds the Summary. Queries run sequentially: a tenant-pinned request
// connection must never be used concurrently.
type UseCase struct {
	bills BillReader
	stock LowStockReader
	recv  ReceivablesReader
	kpi   KPIReader
	neg   NegativeStockReader
	now   func() time.Time
}

// New constructs the use case.
func New(bills BillReader, stock LowStockReader, recv ReceivablesReader, kpi KPIReader) *UseCase {
	return &UseCase{bills: bills, stock: stock, recv: recv, kpi: kpi, now: time.Now}
}

// WithNegativeStock enables the negative-stock section; without it the section stays empty.
func (uc *UseCase) WithNegativeStock(r NegativeStockReader) *UseCase {
	uc.neg = r
	return uc
}

// Execute gathers every section; per-source errors land in Summary.Failed.
func (uc *UseCase) Execute(ctx context.Context, tenantID uuid.UUID) (Summary, error) {
	if tenantID == uuid.Nil {
		return Summary{}, fmt.Errorf("todo: tenant_id is required")
	}
	now := uc.now().In(cst)
	s := Summary{GeneratedAt: now.UTC()}
	fail := func(key string) { s.Failed = append(s.Failed, key) }

	var err error
	if s.DraftPurchases, err = uc.drafts(ctx, tenantID, domain.BillTypePurchase, domain.BillSubTypePurchase); err != nil {
		fail(SrcDraftPurchases)
	}
	if s.DraftSales, err = uc.drafts(ctx, tenantID, domain.BillTypeSale, domain.BillSubTypeSale); err != nil {
		fail(SrcDraftSales)
	}
	if s.LowStock, err = uc.lowStock(ctx, tenantID); err != nil {
		fail(SrcLowStock)
	}
	if s.UninvoicedSales, err = uc.uninvoiced(ctx, tenantID); err != nil {
		fail(SrcUninvoiced)
	}

	if uc.neg != nil {
		items, n, err := uc.neg.NegativeStock(ctx, tenantID, NegativeStockLimit)
		if err != nil {
			fail(SrcNegativeStock)
		} else {
			s.NegativeStock = NegativeStockSection{Count: n, Items: items}
		}
	}

	recv, err := uc.recv.Execute(ctx, tenantID, appbill.ReceivableAR, nil)
	if err != nil {
		fail(SrcOverdue)
	} else {
		s.OverdueCustomers = overdue(recv)
		s.KPIs.ReceivableUnpaid = recv.Total.Unpaid
	}

	if err := uc.fillKPIs(ctx, tenantID, now, &s.KPIs); err != nil {
		fail(SrcKPIs)
	}
	s.HasData = true
	if has, err := uc.kpi.HasData(ctx, tenantID); err != nil {
		fail(SrcKPIs)
	} else {
		s.HasData = has
	}

	if s.Onboarding, err = uc.kpi.Onboarding(ctx, tenantID); err != nil {
		fail(SrcOnboarding)
	}

	s.Total = s.DraftPurchases.Count + s.DraftSales.Count + s.LowStock.Count +
		s.OverdueCustomers.Count + s.UninvoicedSales.Count + s.NegativeStock.Count
	return s, nil
}

func (uc *UseCase) drafts(ctx context.Context, tenantID uuid.UUID, bt domain.BillType, st domain.BillSubType) (BillSection, error) {
	draft := domain.StatusDraft
	items, total, err := uc.bills.ListBills(ctx, appbill.BillListFilter{
		TenantID: tenantID, BillType: bt, SubType: st, Status: &draft, Page: 1, Size: PreviewSize,
	})
	if err != nil {
		return BillSection{}, err
	}
	return BillSection{Count: int(total), Items: billItems(items)}, nil
}

// uninvoiced merges the "no invoice yet" and "partly invoiced" approved sales.
func (uc *UseCase) uninvoiced(ctx context.Context, tenantID uuid.UUID) (BillSection, error) {
	var all []domain.BillHead
	count := 0
	for _, st := range []string{"none", "partial"} {
		items, total, err := uc.bills.ListBills(ctx, appbill.BillListFilter{
			TenantID: tenantID, BillType: domain.BillTypeSale, SubType: domain.BillSubTypeSale,
			InvoiceStatus: st, OldestFirst: true, Page: 1, Size: PreviewSize,
		})
		if err != nil {
			return BillSection{}, err
		}
		all = append(all, items...)
		count += int(total)
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].BillDate.Before(all[j].BillDate) })
	if len(all) > PreviewSize {
		all = all[:PreviewSize]
	}
	return BillSection{Count: count, Items: billItems(all)}, nil
}

func (uc *UseCase) lowStock(ctx context.Context, tenantID uuid.UUID) (StockSection, error) {
	// A generous limit so Count is the real total, not the default cap of 200.
	rows, err := uc.stock.Execute(ctx, tenantID, 100000)
	if err != nil {
		return StockSection{}, err
	}
	sec := StockSection{Count: len(rows), Items: []StockItem{}}
	for i, r := range rows {
		if i >= PreviewSize {
			break
		}
		sec.Items = append(sec.Items, StockItem{
			ProductID: r.ProductID, ProductName: r.ProductName, AvailableQty: r.AvailableQty,
			ReorderPoint: r.ReorderPoint, DaysOfSupply: r.DaysOfSupply,
		})
	}
	return sec, nil
}

// overdue keeps customers with a balance on bills older than 30 days, biggest first.
func overdue(recv *appbill.ReceivablesOutput) DebtorSection {
	var list []DebtorItem
	for _, r := range recv.Items {
		od := r.B31to60.Add(r.B61to90).Add(r.B90Plus)
		if !od.IsPositive() {
			continue
		}
		list = append(list, DebtorItem{
			PartnerID: r.PartnerID, PartnerName: r.PartnerName, Overdue: od,
			Unpaid: r.Unpaid, OldestBillDate: r.OldestBillDate,
		})
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].Overdue.GreaterThan(list[j].Overdue) })
	sec := DebtorSection{Count: len(list), Items: []DebtorItem{}}
	if len(list) > PreviewSize {
		list = list[:PreviewSize]
	}
	sec.Items = append(sec.Items, list...)
	return sec
}

func (uc *UseCase) fillKPIs(ctx context.Context, tenantID uuid.UUID, now time.Time, k *KPIs) error {
	y, m, d := now.Date()
	dayStart := time.Date(y, m, d, 0, 0, 0, 0, cst)
	dayEnd := dayStart.AddDate(0, 0, 1)
	monthStart := time.Date(y, m, 1, 0, 0, 0, 0, cst)
	monthEnd := monthStart.AddDate(0, 1, 0)

	sum, err := uc.bills.SummarizeBills(ctx, appbill.BillListFilter{
		TenantID: tenantID, BillType: domain.BillTypeSale, SubType: domain.BillSubTypeSale,
		DateFrom: &dayStart, DateTo: &dayEnd,
	})
	if err != nil {
		return err
	}
	// Net of returns, so a refunded sale does not inflate today's figure.
	k.TodaySales = sum.TotalAmount.Sub(sum.ReturnedAmount)
	if k.TodayCollected, err = uc.kpi.Collected(ctx, tenantID, dayStart, dayEnd); err != nil {
		return err
	}
	if k.MonthUninvoiced, err = uc.kpi.Uninvoiced(ctx, tenantID, monthStart, monthEnd); err != nil {
		return err
	}
	return nil
}

func billItems(in []domain.BillHead) []BillItem {
	out := make([]BillItem, 0, len(in))
	for _, b := range in {
		out = append(out, BillItem{ID: b.ID, BillNo: b.BillNo, PartnerName: b.PartnerName, TotalAmount: b.TotalAmount, BillDate: b.BillDate})
	}
	return out
}
