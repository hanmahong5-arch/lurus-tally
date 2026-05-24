package reports_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	appreports "github.com/hanmahong5-arch/lurus-tally/internal/app/reports"
)

// ── stub repo ─────────────────────────────────────────────────────────────────

type stubRepo struct {
	sales  []appreports.SaleRow
	stocks []appreports.StockRow
}

func (s *stubRepo) ListRecentSaleLines(_ context.Context, _ uuid.UUID, _ int) ([]appreports.SaleRow, error) {
	return s.sales, nil
}

func (s *stubRepo) ListStockSnapshots(_ context.Context, _ uuid.UUID) ([]appreports.StockRow, error) {
	return s.stocks, nil
}

var _ appreports.Repo = (*stubRepo)(nil)

// ── helpers ──────────────────────────────────────────────────────────────────

func dec(s string) decimal.Decimal {
	d, _ := decimal.NewFromString(s)
	return d
}

func saleRow(id uuid.UUID, name string, revenue, margin string) appreports.SaleRow {
	return appreports.SaleRow{
		ProductID:   id,
		ProductName: name,
		Qty:         dec("1"),
		Revenue:     dec(revenue),
		Margin:      dec(margin),
		SoldAt:      time.Now(),
	}
}

// ── GrossMarginSummary ────────────────────────────────────────────────────────

func TestGrossMarginSummary_Overall(t *testing.T) {
	pA := uuid.New()
	pB := uuid.New()
	// two rows for pA (margin 0.5 each), one row for pB (margin 0.2)
	repo := &stubRepo{
		sales: []appreports.SaleRow{
			saleRow(pA, "Alpha", "100", "0.5"),
			saleRow(pA, "Alpha", "200", "0.5"),
			saleRow(pB, "Beta", "50", "0.2"),
		},
	}
	uc := appreports.New(repo)
	res, err := uc.GrossMarginSummary(context.Background(), uuid.New(), 30)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// overall = (0.5+0.5+0.2)/3 = 0.4 → "40.0%"
	if res.OverallMargin != "40.0%" {
		t.Errorf("OverallMargin = %q, want 40.0%%", res.OverallMargin)
	}
	if res.Days != 30 {
		t.Errorf("Days = %d, want 30", res.Days)
	}
}

func TestGrossMarginSummary_Top10OrderedDescending(t *testing.T) {
	rows := make([]appreports.SaleRow, 0, 12)
	for i := 0; i < 12; i++ {
		rows = append(rows, saleRow(uuid.New(), "P", "10", decimal.NewFromFloat(float64(i)*0.01).String()))
	}
	repo := &stubRepo{sales: rows}
	uc := appreports.New(repo)
	res, err := uc.GrossMarginSummary(context.Background(), uuid.New(), 30)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Top10) != 10 {
		t.Errorf("Top10 len = %d, want 10", len(res.Top10))
	}
	if len(res.Bottom10) != 10 {
		t.Errorf("Bottom10 len = %d, want 10", len(res.Bottom10))
	}
}

func TestGrossMarginSummary_NoSales(t *testing.T) {
	repo := &stubRepo{}
	uc := appreports.New(repo)
	res, err := uc.GrossMarginSummary(context.Background(), uuid.New(), 30)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.OverallMargin != "0.0%" {
		t.Errorf("empty overall margin = %q, want 0.0%%", res.OverallMargin)
	}
}

// ── ABCClassify ───────────────────────────────────────────────────────────────

func TestABCClassify_Tiers(t *testing.T) {
	// 3 products: A=80 revenue, B=15, C=5; total=100
	pA := uuid.New()
	pB := uuid.New()
	pC := uuid.New()
	repo := &stubRepo{
		sales: []appreports.SaleRow{
			saleRow(pA, "TopSeller", "80", "0.3"),
			saleRow(pB, "Mid", "15", "0.2"),
			saleRow(pC, "Tail", "5", "0.1"),
		},
	}
	uc := appreports.New(repo)
	res, err := uc.ABCClassify(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.TotalSKUs != 3 {
		t.Errorf("TotalSKUs = %d, want 3", res.TotalSKUs)
	}
	// pA cumulative share=80% → A tier
	if res.A.SKUCount != 1 {
		t.Errorf("A.SKUCount = %d, want 1", res.A.SKUCount)
	}
	// pB cumulative share=95% → B tier
	if res.B.SKUCount != 1 {
		t.Errorf("B.SKUCount = %d, want 1", res.B.SKUCount)
	}
	// pC → C tier
	if res.C.SKUCount != 1 {
		t.Errorf("C.SKUCount = %d, want 1", res.C.SKUCount)
	}
	if res.Period != "365d" {
		t.Errorf("Period = %q, want 365d", res.Period)
	}
}

func TestABCClassify_NoSales(t *testing.T) {
	repo := &stubRepo{}
	uc := appreports.New(repo)
	res, err := uc.ABCClassify(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.TotalSKUs != 0 {
		t.Errorf("TotalSKUs = %d, want 0", res.TotalSKUs)
	}
}

// ── DeadStock ─────────────────────────────────────────────────────────────────

func TestDeadStock_FiltersCorrectly(t *testing.T) {
	now := time.Now()
	repo := &stubRepo{
		stocks: []appreports.StockRow{
			{ProductID: uuid.New(), ProductName: "Old", ProductCode: "O1",
				Qty: dec("10"), UnitCost: dec("5"),
				LastMovedAt: now.Add(-100 * 24 * time.Hour)}, // dead (>90d)
			{ProductID: uuid.New(), ProductName: "Fresh", ProductCode: "F1",
				Qty: dec("5"), UnitCost: dec("2"),
				LastMovedAt: now.Add(-30 * 24 * time.Hour)}, // active
		},
	}
	uc := appreports.New(repo)
	res, err := uc.DeadStock(context.Background(), uuid.New(), 90)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Count != 1 {
		t.Errorf("dead stock count = %d, want 1", res.Count)
	}
	if res.Items[0].Name != "Old" {
		t.Errorf("dead stock item name = %q, want Old", res.Items[0].Name)
	}
	// value = 10 * 5 = 50.00
	if res.Items[0].ValueCNY != "50.00" {
		t.Errorf("ValueCNY = %q, want 50.00", res.Items[0].ValueCNY)
	}
	if res.ThresholdDays != 90 {
		t.Errorf("ThresholdDays = %d, want 90", res.ThresholdDays)
	}
}

func TestDeadStock_NoneReturnsEmptySlice(t *testing.T) {
	now := time.Now()
	repo := &stubRepo{
		stocks: []appreports.StockRow{
			{ProductID: uuid.New(), ProductName: "Fresh", ProductCode: "F1",
				Qty: dec("5"), UnitCost: dec("2"),
				LastMovedAt: now.Add(-10 * 24 * time.Hour)},
		},
	}
	uc := appreports.New(repo)
	res, err := uc.DeadStock(context.Background(), uuid.New(), 90)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Count != 0 {
		t.Errorf("dead stock count = %d, want 0", res.Count)
	}
	if res.Items == nil {
		t.Error("Items should be non-nil empty slice")
	}
}

// ── SalesTop ──────────────────────────────────────────────────────────────────

func TestSalesTop_RevenueMetric(t *testing.T) {
	pA := uuid.New()
	pB := uuid.New()
	repo := &stubRepo{
		sales: []appreports.SaleRow{
			saleRow(pA, "HighRev", "500", "0.3"),
			saleRow(pB, "LowRev", "100", "0.5"),
		},
	}
	uc := appreports.New(repo)
	res, err := uc.SalesTop(context.Background(), uuid.New(), "revenue", 7, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.TopProducts) != 2 {
		t.Errorf("top count = %d, want 2", len(res.TopProducts))
	}
	if res.TopProducts[0].Name != "HighRev" {
		t.Errorf("top[0].Name = %q, want HighRev", res.TopProducts[0].Name)
	}
	if res.TopProducts[0].Rank != 1 {
		t.Errorf("top[0].Rank = %d, want 1", res.TopProducts[0].Rank)
	}
	if res.Metric != "revenue" {
		t.Errorf("Metric = %q, want revenue", res.Metric)
	}
}

func TestSalesTop_LimitCaps(t *testing.T) {
	sales := make([]appreports.SaleRow, 15)
	for i := range sales {
		sales[i] = saleRow(uuid.New(), "P", "10", "0.1")
	}
	repo := &stubRepo{sales: sales}
	uc := appreports.New(repo)
	res, err := uc.SalesTop(context.Background(), uuid.New(), "revenue", 7, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.TopProducts) != 5 {
		t.Errorf("top count = %d, want 5 (limit)", len(res.TopProducts))
	}
}
