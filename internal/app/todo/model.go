package todo

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

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
// Stale, set only on draft purchases, is how many of Count are older than the
// receipt-chase window. It is a subset of Count, so it is not added to Summary.Total.
type BillSection struct {
	Count int
	Items []BillItem
	Stale int
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

// Summary is the full attention payload.
type Summary struct {
	DraftPurchases      BillSection
	DraftSales          BillSection
	LowStock            StockSection
	OverdueCustomers    DebtorSection
	UninvoicedSales     BillSection
	UninvoicedPurchases BillSection
	NegativeStock       NegativeStockSection
	KPIs                KPIs
	// Total is the sum of the section counts (the sidebar badge). A stale draft
	// purchase is already inside DraftPurchases, so it is not added again.
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
