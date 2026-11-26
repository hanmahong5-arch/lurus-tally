package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// grossMarginSummary computes overall gross margin plus top-10 and bottom-10 products by margin.
func (r *Registry) grossMarginSummary(ctx context.Context, tenantID uuid.UUID, argsJSON string) (string, error) {
	var args grossMarginSummaryArgs
	_ = json.Unmarshal([]byte(argsJSON), &args)
	days := 30
	if args.Days != nil {
		days = *args.Days
	}

	rows, err := r.saleRepo.ListRecentSaleLines(ctx, tenantID, days)
	if err != nil {
		return "", fmt.Errorf("gross_margin_summary: %w", err)
	}

	type pAgg struct {
		name    string
		revenue decimal.Decimal
		margin  decimal.Decimal
		count   int
	}
	aggMap := make(map[uuid.UUID]*pAgg)
	totalRevenue := decimal.Zero
	totalMarginSum := decimal.Zero
	for _, s := range rows {
		a, ok := aggMap[s.ProductID]
		if !ok {
			a = &pAgg{name: s.ProductName}
			aggMap[s.ProductID] = a
		}
		a.revenue = a.revenue.Add(s.Revenue)
		a.margin = a.margin.Add(s.Margin)
		a.count++
		totalRevenue = totalRevenue.Add(s.Revenue)
		totalMarginSum = totalMarginSum.Add(s.Margin)
	}

	type scored struct {
		name      string
		avgMargin decimal.Decimal
	}
	items := make([]scored, 0, len(aggMap))
	for _, a := range aggMap {
		avgM := decimal.Zero
		if a.count > 0 {
			avgM = a.margin.Div(decimal.NewFromInt(int64(a.count)))
		}
		items = append(items, scored{name: a.name, avgMargin: avgM})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].avgMargin.GreaterThan(items[j].avgMargin)
	})

	top10 := items
	if len(top10) > 10 {
		top10 = top10[:10]
	}
	bottom10 := items
	if len(bottom10) > 10 {
		bottom10 = bottom10[len(bottom10)-10:]
	}

	overallMargin := decimal.Zero
	if len(rows) > 0 {
		overallMargin = totalMarginSum.Div(decimal.NewFromInt(int64(len(rows))))
	}

	toSlice := func(ss []scored) []map[string]interface{} {
		out := make([]map[string]interface{}, 0, len(ss))
		for _, s := range ss {
			out = append(out, map[string]interface{}{
				"name":       s.name,
				"avg_margin": s.avgMargin.Mul(decimal.NewFromInt(100)).StringFixed(1) + "%",
			})
		}
		return out
	}

	return jsonMarshal(map[string]interface{}{
		"overall_margin": overallMargin.Mul(decimal.NewFromInt(100)).StringFixed(1) + "%",
		"top10":          toSlice(top10),
		"bottom10":       toSlice(bottom10),
		"days":           days,
	})
}

// queryExchangeRate looks up the currency conversion rate from args.From to args.To.
func (r *Registry) queryExchangeRate(ctx context.Context, tenantID uuid.UUID, argsJSON string) (string, error) {
	var args queryExchangeRateArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("query_exchange_rate: invalid args: %w", err)
	}
	if args.To == "" {
		args.To = "CNY"
	}
	rate, err := r.exchangeRateRepo.GetRate(ctx, tenantID, args.From, args.To)
	if err != nil {
		return "", fmt.Errorf("query_exchange_rate: %w", err)
	}
	return jsonMarshal(map[string]interface{}{
		"from": args.From,
		"to":   args.To,
		"rate": rate.StringFixed(6),
	})
}
