// Package reports exposes the four AI analytics as first-class report use cases.
// Aggregation logic is re-implemented here (mirroring internal/app/ai/tools.go)
// so the reports package stays independent of the ai package and its LLM deps.
package reports

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// SaleRow is a single sale line returned by the repo.
type SaleRow struct {
	ProductID   uuid.UUID
	ProductName string
	Qty         decimal.Decimal
	Revenue     decimal.Decimal // total revenue for this row
	Margin      decimal.Decimal // gross margin = (price - cost) / price
	SoldAt      time.Time
}

// StockRow is a current stock snapshot row returned by the repo.
type StockRow struct {
	ProductID     uuid.UUID
	ProductName   string
	ProductCode   string
	Qty           decimal.Decimal
	UnitCost      decimal.Decimal
	LastMovedAt   time.Time
	AvgDailySales decimal.Decimal
	LeadTimeDays  int
}

// Repo is the narrow data interface required by all report use cases.
type Repo interface {
	ListRecentSaleLines(ctx context.Context, tenantID uuid.UUID, days int) ([]SaleRow, error)
	ListStockSnapshots(ctx context.Context, tenantID uuid.UUID) ([]StockRow, error)
}

// UseCase bundles all four report aggregations.
type UseCase struct {
	repo Repo
}

// New constructs a UseCase backed by repo.
func New(repo Repo) *UseCase {
	return &UseCase{repo: repo}
}

// ── Gross Margin Summary ─────────────────────────────────────────────────────

// MarginProduct holds per-product average margin for the summary.
type MarginProduct struct {
	Name      string `json:"name"`
	AvgMargin string `json:"avg_margin"` // e.g. "42.3%"
}

// GrossMarginResult is the output of GrossMarginSummary.
type GrossMarginResult struct {
	OverallMargin string          `json:"overall_margin"`
	Top10         []MarginProduct `json:"top10"`
	Bottom10      []MarginProduct `json:"bottom10"`
	Days          int             `json:"days"`
}

// GrossMarginSummary returns overall gross margin for the past N days plus top-10
// and bottom-10 products by average margin.
func (uc *UseCase) GrossMarginSummary(ctx context.Context, tenantID uuid.UUID, days int) (*GrossMarginResult, error) {
	rows, err := uc.repo.ListRecentSaleLines(ctx, tenantID, days)
	if err != nil {
		return nil, fmt.Errorf("gross margin summary: %w", err)
	}

	type pAgg struct {
		name      string
		marginSum decimal.Decimal
		count     int
	}
	aggMap := make(map[uuid.UUID]*pAgg)
	totalMarginSum := decimal.Zero
	rowCount := 0
	for _, s := range rows {
		a, ok := aggMap[s.ProductID]
		if !ok {
			a = &pAgg{name: s.ProductName}
			aggMap[s.ProductID] = a
		}
		a.marginSum = a.marginSum.Add(s.Margin)
		a.count++
		totalMarginSum = totalMarginSum.Add(s.Margin)
		rowCount++
	}

	type scored struct {
		name      string
		avgMargin decimal.Decimal
	}
	items := make([]scored, 0, len(aggMap))
	for _, a := range aggMap {
		avg := decimal.Zero
		if a.count > 0 {
			avg = a.marginSum.Div(decimal.NewFromInt(int64(a.count)))
		}
		items = append(items, scored{name: a.name, avgMargin: avg})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].avgMargin.GreaterThan(items[j].avgMargin)
	})

	toSlice := func(ss []scored) []MarginProduct {
		out := make([]MarginProduct, 0, len(ss))
		for _, s := range ss {
			out = append(out, MarginProduct{
				Name:      s.name,
				AvgMargin: s.avgMargin.Mul(decimal.NewFromInt(100)).StringFixed(1) + "%",
			})
		}
		return out
	}

	top10 := items
	if len(top10) > 10 {
		top10 = top10[:10]
	}
	bottom10 := items
	if len(bottom10) > 10 {
		bottom10 = bottom10[len(bottom10)-10:]
	}

	overall := decimal.Zero
	if rowCount > 0 {
		overall = totalMarginSum.Div(decimal.NewFromInt(int64(rowCount)))
	}

	return &GrossMarginResult{
		OverallMargin: overall.Mul(decimal.NewFromInt(100)).StringFixed(1) + "%",
		Top10:         toSlice(top10),
		Bottom10:      toSlice(bottom10),
		Days:          days,
	}, nil
}

// ── ABC Classification ───────────────────────────────────────────────────────

// ABCTier holds summary metrics for one tier.
type ABCTier struct {
	SKUCount     int    `json:"sku_count"`
	RevenueShare string `json:"revenue_share"` // e.g. "80.0%"
}

// ABCResult is the output of ABCClassify.
type ABCResult struct {
	A         ABCTier `json:"a"`
	B         ABCTier `json:"b"`
	C         ABCTier `json:"c"`
	TotalSKUs int     `json:"total_skus"`
	Period    string  `json:"period"`
}

// ABCClassify classifies all products into A/B/C tiers by 365-day revenue.
// A = top 80 %, B = next 15 %, C = bottom 5 %.
func (uc *UseCase) ABCClassify(ctx context.Context, tenantID uuid.UUID) (*ABCResult, error) {
	rows, err := uc.repo.ListRecentSaleLines(ctx, tenantID, 365)
	if err != nil {
		return nil, fmt.Errorf("abc classify: %w", err)
	}

	rev := make(map[uuid.UUID]decimal.Decimal)
	for _, s := range rows {
		rev[s.ProductID] = rev[s.ProductID].Add(s.Revenue)
	}

	type pRev struct {
		id  uuid.UUID
		rev decimal.Decimal
	}
	list := make([]pRev, 0, len(rev))
	total := decimal.Zero
	for id, r := range rev {
		list = append(list, pRev{id, r})
		total = total.Add(r)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].rev.GreaterThan(list[j].rev)
	})

	var (
		aCnt, bCnt, cCnt int
		aRev, bRev, cRev decimal.Decimal
		cumulative       = decimal.Zero
	)
	for _, p := range list {
		if total.IsZero() {
			cCnt++
			cRev = cRev.Add(p.rev)
			continue
		}
		share := p.rev.Div(total)
		cumulative = cumulative.Add(share)
		switch {
		case cumulative.LessThanOrEqual(decimal.NewFromFloat(0.80)):
			aCnt++
			aRev = aRev.Add(p.rev)
		case cumulative.LessThanOrEqual(decimal.NewFromFloat(0.95)):
			bCnt++
			bRev = bRev.Add(p.rev)
		default:
			cCnt++
			cRev = cRev.Add(p.rev)
		}
	}

	safeShare := func(v decimal.Decimal) string {
		if total.IsZero() {
			return "0.0%"
		}
		return v.Div(total).Mul(decimal.NewFromInt(100)).StringFixed(1) + "%"
	}

	return &ABCResult{
		A:         ABCTier{SKUCount: aCnt, RevenueShare: safeShare(aRev)},
		B:         ABCTier{SKUCount: bCnt, RevenueShare: safeShare(bRev)},
		C:         ABCTier{SKUCount: cCnt, RevenueShare: safeShare(cRev)},
		TotalSKUs: len(list),
		Period:    "365d",
	}, nil
}

// ── Dead Stock ───────────────────────────────────────────────────────────────

// DeadStockItem is one product with no movement in the threshold period.
type DeadStockItem struct {
	Name      string `json:"name"`
	Code      string `json:"code"`
	Qty       string `json:"qty"`
	ValueCNY  string `json:"value_cny"`
	DaysSince int    `json:"days_since_last_movement"`
}

// DeadStockResult is the output of DeadStock.
type DeadStockResult struct {
	Items         []DeadStockItem `json:"items"`
	Count         int             `json:"count"`
	ThresholdDays int             `json:"threshold_days"`
}

// DeadStock returns stock rows with no movement in the past N days.
func (uc *UseCase) DeadStock(ctx context.Context, tenantID uuid.UUID, days int) (*DeadStockResult, error) {
	rows, err := uc.repo.ListStockSnapshots(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("dead stock: %w", err)
	}
	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)

	var items []DeadStockItem
	for _, s := range rows {
		if s.LastMovedAt.Before(cutoff) {
			items = append(items, DeadStockItem{
				Name:      s.ProductName,
				Code:      s.ProductCode,
				Qty:       s.Qty.StringFixed(2),
				ValueCNY:  s.Qty.Mul(s.UnitCost).StringFixed(2),
				DaysSince: int(time.Since(s.LastMovedAt).Hours() / 24),
			})
		}
	}
	if items == nil {
		items = []DeadStockItem{}
	}
	return &DeadStockResult{
		Items:         items,
		Count:         len(items),
		ThresholdDays: days,
	}, nil
}

// ── Sales Top-N ──────────────────────────────────────────────────────────────

// SalesTopItem is one ranked product in the top-N list.
type SalesTopItem struct {
	Rank  int    `json:"rank"`
	Name  string `json:"name"`
	Score string `json:"score"`
}

// SalesTopResult is the output of SalesTop.
type SalesTopResult struct {
	TopProducts []SalesTopItem `json:"top_products"`
	Metric      string         `json:"metric"`
	Days        int            `json:"days"`
}

// SalesTop returns top-N products by the chosen metric (revenue | margin | qty)
// over the past N days.
func (uc *UseCase) SalesTop(ctx context.Context, tenantID uuid.UUID, metric string, days, limit int) (*SalesTopResult, error) {
	rows, err := uc.repo.ListRecentSaleLines(ctx, tenantID, days)
	if err != nil {
		return nil, fmt.Errorf("sales top: %w", err)
	}

	type agg struct {
		name    string
		revenue decimal.Decimal
		qty     decimal.Decimal
		margin  decimal.Decimal
		count   int
	}
	aggMap := make(map[uuid.UUID]*agg)
	for _, s := range rows {
		a, ok := aggMap[s.ProductID]
		if !ok {
			a = &agg{name: s.ProductName}
			aggMap[s.ProductID] = a
		}
		a.revenue = a.revenue.Add(s.Revenue)
		a.qty = a.qty.Add(s.Qty)
		a.margin = a.margin.Add(s.Margin)
		a.count++
	}

	type scored struct {
		name  string
		score decimal.Decimal
	}
	items := make([]scored, 0, len(aggMap))
	for _, a := range aggMap {
		var score decimal.Decimal
		switch metric {
		case "margin":
			if a.count > 0 {
				score = a.margin.Div(decimal.NewFromInt(int64(a.count)))
			}
		case "qty":
			score = a.qty
		default:
			score = a.revenue
		}
		items = append(items, scored{name: a.name, score: score})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].score.GreaterThan(items[j].score)
	})
	if len(items) > limit {
		items = items[:limit]
	}

	out := make([]SalesTopItem, 0, len(items))
	for i, it := range items {
		out = append(out, SalesTopItem{Rank: i + 1, Name: it.name, Score: it.score.StringFixed(2)})
	}
	return &SalesTopResult{
		TopProducts: out,
		Metric:      metric,
		Days:        days,
	}, nil
}

// ── ROP helper (mirrors internal/app/ai/tools.go:computeROP) ─────────────────

// computeROP calculates the Re-Order Point.
// ROP = lead_time_days × avg_daily_sales + safety_stock
// safety_stock = z × σ × √lead_time (z=1.65, σ = avg_daily_sales × 0.3)
func computeROP(s StockRow) decimal.Decimal {
	if s.AvgDailySales.IsZero() || s.LeadTimeDays == 0 {
		return decimal.Zero
	}
	lt := decimal.NewFromInt(int64(s.LeadTimeDays))
	z := decimal.NewFromFloat(1.65)
	sigma := s.AvgDailySales.Mul(decimal.NewFromFloat(0.3))
	sqrtLT := decimal.NewFromFloat(math.Sqrt(float64(s.LeadTimeDays)))
	return lt.Mul(s.AvgDailySales).Add(z.Mul(sigma).Mul(sqrtLT))
}

// suppress unused-import lint when computeROP is referenced only by future callers.
var _ = computeROP
