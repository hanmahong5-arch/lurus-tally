package reports_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	appreports "github.com/hanmahong5-arch/lurus-tally/internal/app/reports"
)

// ── error-returning repo (covers all four "repo error → wrapped error" paths) ──

type errRepo struct {
	err error
}

func (r *errRepo) ListRecentSaleLines(_ context.Context, _ uuid.UUID, _ int) ([]appreports.SaleRow, error) {
	return nil, r.err
}

func (r *errRepo) ListStockSnapshots(_ context.Context, _ uuid.UUID) ([]appreports.StockRow, error) {
	return nil, r.err
}

var _ appreports.Repo = (*errRepo)(nil)

func TestGrossMarginSummary_RepoError(t *testing.T) {
	wantErr := errors.New("boom")
	uc := appreports.New(&errRepo{err: wantErr})
	_, err := uc.GrossMarginSummary(context.Background(), uuid.New(), 30)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.HasPrefix(err.Error(), "gross margin summary:") {
		t.Errorf("error = %q, want prefix 'gross margin summary:'", err.Error())
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("error does not wrap original: %v", err)
	}
}

func TestABCClassify_RepoError(t *testing.T) {
	wantErr := errors.New("boom")
	uc := appreports.New(&errRepo{err: wantErr})
	_, err := uc.ABCClassify(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.HasPrefix(err.Error(), "abc classify:") {
		t.Errorf("error = %q, want prefix 'abc classify:'", err.Error())
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("error does not wrap original: %v", err)
	}
}

func TestDeadStock_RepoError(t *testing.T) {
	wantErr := errors.New("boom")
	uc := appreports.New(&errRepo{err: wantErr})
	_, err := uc.DeadStock(context.Background(), uuid.New(), 90)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.HasPrefix(err.Error(), "dead stock:") {
		t.Errorf("error = %q, want prefix 'dead stock:'", err.Error())
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("error does not wrap original: %v", err)
	}
}

func TestSalesTop_RepoError(t *testing.T) {
	wantErr := errors.New("boom")
	uc := appreports.New(&errRepo{err: wantErr})
	_, err := uc.SalesTop(context.Background(), uuid.New(), "revenue", 7, 10)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.HasPrefix(err.Error(), "sales top:") {
		t.Errorf("error = %q, want prefix 'sales top:'", err.Error())
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("error does not wrap original: %v", err)
	}
}

// ── ABCClassify: zero-total-revenue guard (usecase.go:197-200) ────────────────

func TestABCClassify_ZeroTotalRevenueFallsToC(t *testing.T) {
	pA := uuid.New()
	pB := uuid.New()
	// every row has revenue "0" → total.IsZero() is true for the whole run
	repo := &stubRepo{
		sales: []appreports.SaleRow{
			saleRow(pA, "FreeSampleA", "0", "0.1"),
			saleRow(pB, "FreeSampleB", "0", "0.2"),
		},
	}
	uc := appreports.New(repo)
	res, err := uc.ABCClassify(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.TotalSKUs != 2 {
		t.Errorf("TotalSKUs = %d, want 2", res.TotalSKUs)
	}
	// both products fall into C when total revenue is zero (usecase.go:197-200)
	if res.A.SKUCount != 0 || res.B.SKUCount != 0 || res.C.SKUCount != 2 {
		t.Errorf("tier counts = A:%d B:%d C:%d, want A:0 B:0 C:2", res.A.SKUCount, res.B.SKUCount, res.C.SKUCount)
	}
	if res.A.RevenueShare != "0.0%" || res.B.RevenueShare != "0.0%" || res.C.RevenueShare != "0.0%" {
		t.Errorf("shares = A:%s B:%s C:%s, want all 0.0%%", res.A.RevenueShare, res.B.RevenueShare, res.C.RevenueShare)
	}
}

// ── ABCClassify: single product takes 100% of revenue ─────────────────────────
// Cumulative share after the first (and only) product is 100%, which exceeds
// the B ceiling of 95% (usecase.go:204-214), so a lone product is classified
// C, not A — a direct consequence of the cumulative-share tier boundaries,
// not a special case. RevenueShare is still "100.0%" since it holds all of it.

func TestABCClassify_SingleProductAllRevenueIsC(t *testing.T) {
	pOnly := uuid.New()
	repo := &stubRepo{
		sales: []appreports.SaleRow{
			saleRow(pOnly, "Sole", "1000", "0.3"),
		},
	}
	uc := appreports.New(repo)
	res, err := uc.ABCClassify(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.TotalSKUs != 1 {
		t.Errorf("TotalSKUs = %d, want 1", res.TotalSKUs)
	}
	if res.A.SKUCount != 0 || res.B.SKUCount != 0 || res.C.SKUCount != 1 {
		t.Errorf("tier counts = A:%d B:%d C:%d, want A:0 B:0 C:1", res.A.SKUCount, res.B.SKUCount, res.C.SKUCount)
	}
	if res.C.RevenueShare != "100.0%" {
		t.Errorf("C.RevenueShare = %q, want 100.0%%", res.C.RevenueShare)
	}
}

// ── ABCClassify: a product landing exactly on the 80% A/B boundary ───────────
// Two products, 80/20 split: first product's cumulative share is exactly
// 0.80, which LessThanOrEqual(0.80) admits into A (usecase.go:205-207); the
// second pushes cumulative to 1.00, landing in C (exceeds the 0.95 B
// ceiling) rather than B, which the hand-computed cumulative shares confirm.

func TestABCClassify_BoundaryLandsPreciselyOnProduct(t *testing.T) {
	pBig := uuid.New()
	pSmall := uuid.New()
	repo := &stubRepo{
		sales: []appreports.SaleRow{
			saleRow(pBig, "Big", "80", "0.3"),
			saleRow(pSmall, "Small", "20", "0.1"),
		},
	}
	uc := appreports.New(repo)
	res, err := uc.ABCClassify(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// cumulative after Big = 80/100 = 0.80 <= 0.80 → A
	if res.A.SKUCount != 1 {
		t.Errorf("A.SKUCount = %d, want 1 (Big lands exactly on the 80%% boundary)", res.A.SKUCount)
	}
	if res.A.RevenueShare != "80.0%" {
		t.Errorf("A.RevenueShare = %q, want 80.0%%", res.A.RevenueShare)
	}
	// cumulative after Small = 100/100 = 1.00 > 0.95 → C (not B)
	if res.B.SKUCount != 0 {
		t.Errorf("B.SKUCount = %d, want 0", res.B.SKUCount)
	}
	if res.C.SKUCount != 1 {
		t.Errorf("C.SKUCount = %d, want 1 (Small pushes cumulative past the 95%% B ceiling)", res.C.SKUCount)
	}
	if res.C.RevenueShare != "20.0%" {
		t.Errorf("C.RevenueShare = %q, want 20.0%%", res.C.RevenueShare)
	}
}

// ── DeadStock: strict "Before" boundary — an item moved just inside the ─────
// threshold (more recent than the cutoff) must NOT be counted as dead, while
// one moved just outside it (older than the cutoff) must be. We cannot pin
// the exact instant the implementation's internal time.Now() fires, so we
// use a one-minute guard band around the 90-day cutoff, which is many orders
// of magnitude larger than the scheduling jitter between our two time.Now()
// calls and the implementation's, making the assertions deterministic while
// still exercising the strict-inequality boundary (usecase.go:261).

func TestDeadStock_StrictBeforeBoundary(t *testing.T) {
	now := time.Now()
	justInsideID := uuid.New() // moved 89d23h59m ago: NOT dead (newer than cutoff)
	justOutsideID := uuid.New() // moved 90d00h01m ago: dead (older than cutoff)
	repo := &stubRepo{
		stocks: []appreports.StockRow{
			{ProductID: justInsideID, ProductName: "JustInside", ProductCode: "JI",
				Qty: dec("1"), UnitCost: dec("1"),
				LastMovedAt: now.Add(-90*24*time.Hour + time.Minute)},
			{ProductID: justOutsideID, ProductName: "JustOutside", ProductCode: "JO",
				Qty: dec("1"), UnitCost: dec("1"),
				LastMovedAt: now.Add(-90*24*time.Hour - time.Minute)},
		},
	}
	uc := appreports.New(repo)
	res, err := uc.DeadStock(context.Background(), uuid.New(), 90)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Count != 1 {
		t.Fatalf("Count = %d, want 1 (only the older item is dead)", res.Count)
	}
	if res.Items[0].Name != "JustOutside" {
		t.Errorf("dead item = %q, want JustOutside", res.Items[0].Name)
	}
}

// ── SalesTop: margin metric — score = avg margin over count, not sum ─────────

func TestSalesTop_MarginMetricAveragesNotSums(t *testing.T) {
	pHighAvg := uuid.New()
	pLowAvg := uuid.New()
	repo := &stubRepo{
		sales: []appreports.SaleRow{
			// pHighAvg: single row margin 0.6 → avg 0.6
			saleRow(pHighAvg, "HighAvgMargin", "100", "0.6"),
			// pLowAvg: two rows margin 0.1 and 0.3 → avg 0.2
			saleRow(pLowAvg, "LowAvgMargin", "100", "0.1"),
			saleRow(pLowAvg, "LowAvgMargin", "100", "0.3"),
		},
	}
	uc := appreports.New(repo)
	res, err := uc.SalesTop(context.Background(), uuid.New(), "margin", 30, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.TopProducts) != 2 {
		t.Fatalf("top count = %d, want 2", len(res.TopProducts))
	}
	if res.TopProducts[0].Name != "HighAvgMargin" {
		t.Errorf("top[0].Name = %q, want HighAvgMargin (avg 0.6 > avg 0.2)", res.TopProducts[0].Name)
	}
	if res.TopProducts[0].Score != "0.60" {
		t.Errorf("top[0].Score = %q, want 0.60", res.TopProducts[0].Score)
	}
	if res.TopProducts[1].Score != "0.20" {
		t.Errorf("top[1].Score = %q, want 0.20 (avg of 0.1 and 0.3)", res.TopProducts[1].Score)
	}
	if res.Metric != "margin" {
		t.Errorf("Metric = %q, want margin", res.Metric)
	}
}

// ── SalesTop: qty metric — score is summed quantity, not revenue/margin ──────

func TestSalesTop_QtyMetricSumsQuantity(t *testing.T) {
	pManyUnits := uuid.New()
	pFewUnits := uuid.New()
	repo := &stubRepo{
		sales: []appreports.SaleRow{
			// pManyUnits: qty 3 + 4 = 7, but low revenue/margin
			{ProductID: pManyUnits, ProductName: "ManyUnits", Qty: dec("3"), Revenue: dec("10"), Margin: dec("0.05"), SoldAt: time.Now()},
			{ProductID: pManyUnits, ProductName: "ManyUnits", Qty: dec("4"), Revenue: dec("10"), Margin: dec("0.05"), SoldAt: time.Now()},
			// pFewUnits: qty 2, but high revenue/margin
			{ProductID: pFewUnits, ProductName: "FewUnits", Qty: dec("2"), Revenue: dec("900"), Margin: dec("0.9"), SoldAt: time.Now()},
		},
	}
	uc := appreports.New(repo)
	res, err := uc.SalesTop(context.Background(), uuid.New(), "qty", 30, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.TopProducts) != 2 {
		t.Fatalf("top count = %d, want 2", len(res.TopProducts))
	}
	if res.TopProducts[0].Name != "ManyUnits" {
		t.Errorf("top[0].Name = %q, want ManyUnits (qty 7 > qty 2, despite lower revenue/margin)", res.TopProducts[0].Name)
	}
	if res.TopProducts[0].Score != "7.00" {
		t.Errorf("top[0].Score = %q, want 7.00", res.TopProducts[0].Score)
	}
	if res.TopProducts[1].Score != "2.00" {
		t.Errorf("top[1].Score = %q, want 2.00", res.TopProducts[1].Score)
	}
}

// ── SalesTop: empty rows → empty TopProducts slice, not nil ──────────────────

func TestSalesTop_NoSalesReturnsEmptySlice(t *testing.T) {
	repo := &stubRepo{}
	uc := appreports.New(repo)
	res, err := uc.SalesTop(context.Background(), uuid.New(), "revenue", 30, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.TopProducts) != 0 {
		t.Errorf("TopProducts len = %d, want 0", len(res.TopProducts))
	}
}

// ── GrossMarginSummary: with <=10 products, Top10 and Bottom10 are the same ──
// full sorted set (usecase.go:125-132 — neither slicing branch triggers).

func TestGrossMarginSummary_LessThan10ProductsTopEqualsBottom(t *testing.T) {
	pA := uuid.New()
	pB := uuid.New()
	pC := uuid.New()
	repo := &stubRepo{
		sales: []appreports.SaleRow{
			saleRow(pA, "Alpha", "100", "0.5"), // avg margin 0.5
			saleRow(pB, "Beta", "100", "0.3"),  // avg margin 0.3
			saleRow(pC, "Gamma", "100", "0.1"), // avg margin 0.1
		},
	}
	uc := appreports.New(repo)
	res, err := uc.GrossMarginSummary(context.Background(), uuid.New(), 30)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Top10) != 3 || len(res.Bottom10) != 3 {
		t.Fatalf("Top10 len=%d Bottom10 len=%d, want 3 and 3", len(res.Top10), len(res.Bottom10))
	}
	// full set sorted desc by avg margin: Alpha(0.5), Beta(0.3), Gamma(0.1)
	wantOrder := []string{"Alpha", "Beta", "Gamma"}
	for i, name := range wantOrder {
		if res.Top10[i].Name != name {
			t.Errorf("Top10[%d].Name = %q, want %q", i, res.Top10[i].Name, name)
		}
		if res.Bottom10[i].Name != name {
			t.Errorf("Bottom10[%d].Name = %q, want %q", i, res.Bottom10[i].Name, name)
		}
	}
	if res.Top10[0].AvgMargin != "50.0%" {
		t.Errorf("Top10[0].AvgMargin = %q, want 50.0%%", res.Top10[0].AvgMargin)
	}
	if res.Bottom10[2].AvgMargin != "10.0%" {
		t.Errorf("Bottom10[2].AvgMargin = %q, want 10.0%%", res.Bottom10[2].AvgMargin)
	}
}
