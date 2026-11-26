package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// abcClassify classifies products into A/B/C tiers by 365-day cumulative sales revenue.
func (r *Registry) abcClassify(ctx context.Context, tenantID uuid.UUID) (string, error) {
	saleRows, err := r.saleRepo.ListRecentSaleLines(ctx, tenantID, 365)
	if err != nil {
		return "", fmt.Errorf("abc_classify: %w", err)
	}

	rev := make(map[uuid.UUID]decimal.Decimal)
	for _, s := range saleRows {
		rev[s.ProductID] = rev[s.ProductID].Add(s.Revenue)
	}

	type pRev struct {
		id  uuid.UUID
		rev decimal.Decimal
	}
	list := make([]pRev, 0, len(rev))
	total := decimal.Zero
	for id, rv := range rev {
		list = append(list, pRev{id, rv})
		total = total.Add(rv)
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
		share := p.rev.Div(total)
		cumulative = cumulative.Add(share)
		if cumulative.LessThanOrEqual(decimal.NewFromFloat(0.80)) {
			aCnt++
			aRev = aRev.Add(p.rev)
		} else if cumulative.LessThanOrEqual(decimal.NewFromFloat(0.95)) {
			bCnt++
			bRev = bRev.Add(p.rev)
		} else {
			cCnt++
			cRev = cRev.Add(p.rev)
		}
	}

	safeShare := func(v decimal.Decimal) string {
		if total.IsZero() {
			return "0%"
		}
		return v.Div(total).Mul(decimal.NewFromInt(100)).StringFixed(1) + "%"
	}

	return jsonMarshal(map[string]interface{}{
		"a":          map[string]interface{}{"sku_count": aCnt, "revenue_share": safeShare(aRev)},
		"b":          map[string]interface{}{"sku_count": bCnt, "revenue_share": safeShare(bRev)},
		"c":          map[string]interface{}{"sku_count": cCnt, "revenue_share": safeShare(cRev)},
		"total_skus": len(list),
		"period":     "365d",
	})
}

// recentSalesTop ranks top-N products over recent days by revenue, quantity, or margin.
func (r *Registry) recentSalesTop(ctx context.Context, tenantID uuid.UUID, argsJSON string) (string, error) {
	var args recentSalesTopArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("recent_sales_top: invalid args: %w", err)
	}
	days := 7
	if args.Days != nil {
		days = *args.Days
	}
	limit := 10
	if args.Limit != nil {
		limit = *args.Limit
	}

	rows, err := r.saleRepo.ListRecentSaleLines(ctx, tenantID, days)
	if err != nil {
		return "", fmt.Errorf("recent_sales_top: %w", err)
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
		switch args.Metric {
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

	out := make([]map[string]interface{}, 0, len(items))
	for i, it := range items {
		out = append(out, map[string]interface{}{
			"rank":  i + 1,
			"name":  it.name,
			"score": it.score.StringFixed(2),
		})
	}
	return jsonMarshal(map[string]interface{}{
		"top_products": out,
		"metric":       args.Metric,
		"days":         days,
	})
}
