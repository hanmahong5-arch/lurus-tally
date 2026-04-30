// Package ai implements the AI assistant use cases.
// Tools follow the OpenAI function-calling protocol (tools array).
// SAFE (read-only) tools execute immediately.
// DESTRUCTIVE tools return a Plan for user confirmation before executing.
package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/google/uuid"
	domainai "github.com/hanmahong5-arch/lurus-tally/internal/domain/ai"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/llmclient"
	"github.com/shopspring/decimal"
)

// ProductRow is a minimal product row used by tool queries.
type ProductRow struct {
	ID       uuid.UUID
	Name     string
	Code     string
	Brand    string
	Mnemonic string
}

// StockRow is a minimal stock snapshot row used by tool queries.
type StockRow struct {
	ProductID   uuid.UUID
	ProductName string
	ProductCode string
	Qty         decimal.Decimal
	// UnitCost is the weighted average cost per unit in CNY (stored as decimal).
	UnitCost    decimal.Decimal
	// LastMovedAt is the timestamp of the most recent stock movement (for dead stock calc).
	LastMovedAt time.Time
	// AvgDailySales is a pre-computed daily sales qty average from the past 30d.
	AvgDailySales decimal.Decimal
	// LeadTimeDays is the configured lead time for this product.
	LeadTimeDays int
}

// SaleRow is a minimal sale line row used by analytics tools.
type SaleRow struct {
	ProductID   uuid.UUID
	ProductName string
	Qty         decimal.Decimal
	Revenue     decimal.Decimal // total revenue for this row
	Margin      decimal.Decimal // gross margin = (price - cost) / price
	SoldAt      time.Time
}

// ProductRepo is the minimal read interface required by the AI tools.
type ProductRepo interface {
	// SearchProducts returns products matching the query (name/code/mnemonic/brand full-text search).
	SearchProducts(ctx context.Context, tenantID uuid.UUID, query string) ([]ProductRow, error)
	// ListAllProducts returns all active products for the tenant.
	ListAllProducts(ctx context.Context, tenantID uuid.UUID) ([]ProductRow, error)
}

// StockRepo is the minimal read interface required by the AI tools.
type StockRepo interface {
	// ListStockSnapshots returns current stock rows for the tenant.
	ListStockSnapshots(ctx context.Context, tenantID uuid.UUID) ([]StockRow, error)
}

// SaleRepo is the minimal read interface required by the AI tools.
type SaleRepo interface {
	// ListRecentSaleLines returns individual sale line rows within the past N days.
	ListRecentSaleLines(ctx context.Context, tenantID uuid.UUID, days int) ([]SaleRow, error)
}

// ExchangeRateRepo is the minimal interface for exchange rate queries.
type ExchangeRateRepo interface {
	// GetRate returns the exchange rate from → to (stored as decimal string).
	GetRate(ctx context.Context, tenantID uuid.UUID, from, to string) (decimal.Decimal, error)
}

// Registry holds all tool definitions and their implementations.
type Registry struct {
	productRepo      ProductRepo
	stockRepo        StockRepo
	saleRepo         SaleRepo
	exchangeRateRepo ExchangeRateRepo
}

// NewRegistry constructs a Registry with the provided repo implementations.
func NewRegistry(p ProductRepo, s StockRepo, sl SaleRepo, e ExchangeRateRepo) *Registry {
	return &Registry{
		productRepo:      p,
		stockRepo:        s,
		saleRepo:         sl,
		exchangeRateRepo: e,
	}
}

// ToolDefs returns the OpenAI-compatible tool definitions to include in every chat request.
func ToolDefs() []llmclient.Tool {
	mustJSON := func(v interface{}) json.RawMessage {
		b, _ := json.Marshal(v)
		return b
	}
	return []llmclient.Tool{
		{Type: "function", Function: llmclient.FunctionDef{
			Name:        "search_products",
			Description: "Full-text search products by name, code, mnemonic, or brand.",
			Parameters: mustJSON(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]string{"type": "string", "description": "Search string"},
				},
				"required": []string{"query"},
			}),
		}},
		{Type: "function", Function: llmclient.FunctionDef{
			Name:        "get_stock_summary",
			Description: "Returns warehouse overview: total SKUs, total inventory value (CNY), low-stock count, dead-stock count.",
			Parameters:  mustJSON(map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}),
		}},
		{Type: "function", Function: llmclient.FunctionDef{
			Name:        "list_low_stock",
			Description: "Lists SKUs where current quantity is below the re-order point (ROP). Returns product name, qty, ROP, and days of supply.",
			Parameters: mustJSON(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"threshold_days": map[string]interface{}{"type": "integer", "description": "Days of supply threshold (default 7)"},
				},
			}),
		}},
		{Type: "function", Function: llmclient.FunctionDef{
			Name:        "list_dead_stock",
			Description: "Lists SKUs with no stock movement in the past N days (dead stock / slow-moving inventory).",
			Parameters: mustJSON(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"days": map[string]interface{}{"type": "integer", "description": "Inactivity threshold in days (default 90)"},
				},
			}),
		}},
		{Type: "function", Function: llmclient.FunctionDef{
			Name:        "abc_classify",
			Description: "ABC classification of products by sales revenue: A=top 80%, B=next 15%, C=bottom 5%. Returns SKU count and cumulative revenue share per tier.",
			Parameters:  mustJSON(map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}),
		}},
		{Type: "function", Function: llmclient.FunctionDef{
			Name:        "recent_sales_top",
			Description: "Top-N products by revenue, margin, or quantity over recent N days.",
			Parameters: mustJSON(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"metric": map[string]interface{}{"type": "string", "enum": []string{"revenue", "margin", "qty"}},
					"days":   map[string]interface{}{"type": "integer", "description": "Lookback days (default 7)"},
					"limit":  map[string]interface{}{"type": "integer", "description": "Number of results (default 10)"},
				},
				"required": []string{"metric"},
			}),
		}},
		{Type: "function", Function: llmclient.FunctionDef{
			Name:        "gross_margin_summary",
			Description: "Overall gross margin over the past N days, plus top-10 highest margin and bottom-10 lowest margin products.",
			Parameters: mustJSON(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"days": map[string]interface{}{"type": "integer", "description": "Lookback days (default 30)"},
				},
			}),
		}},
		{Type: "function", Function: llmclient.FunctionDef{
			Name:        "query_exchange_rate",
			Description: "Returns the current exchange rate from one currency to CNY (or another target).",
			Parameters: mustJSON(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"from": map[string]string{"type": "string", "description": "Source currency code (e.g. USD)"},
					"to":   map[string]string{"type": "string", "description": "Target currency code (default CNY)"},
				},
				"required": []string{"from"},
			}),
		}},
		// Destructive — return plan only
		{Type: "function", Function: llmclient.FunctionDef{
			Name:        "propose_price_change",
			Description: "DESTRUCTIVE: Propose a bulk price change for matching products. Returns a plan_id for user confirmation. Does NOT execute immediately.",
			Parameters: mustJSON(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"filter": map[string]string{"type": "string", "description": "Search filter to select products (e.g. brand name, category)"},
					"action": map[string]string{"type": "string", "description": "Price action: '+5%', '-10%', '=199.00'"},
				},
				"required": []string{"filter", "action"},
			}),
		}},
		{Type: "function", Function: llmclient.FunctionDef{
			Name:        "propose_create_purchase_draft",
			Description: "DESTRUCTIVE: Propose creation of a purchase order draft. Returns a plan_id for user confirmation. Does NOT execute immediately.",
			Parameters: mustJSON(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"items": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"product_name": map[string]string{"type": "string"},
								"qty":          map[string]string{"type": "number"},
							},
						},
					},
				},
				"required": []string{"items"},
			}),
		}},
		{Type: "function", Function: llmclient.FunctionDef{
			Name:        "propose_bulk_stock_adjust",
			Description: "DESTRUCTIVE: Propose a bulk stock quantity adjustment for matching products. Returns a plan_id for user confirmation. Does NOT execute immediately.",
			Parameters: mustJSON(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"filter": map[string]string{"type": "string", "description": "Search filter to select products"},
					"delta":  map[string]string{"type": "number", "description": "Quantity delta to apply (positive = in, negative = out)"},
				},
				"required": []string{"filter", "delta"},
			}),
		}},
	}
}

// DispatchResult is what Execute returns for each tool call.
type DispatchResult struct {
	ToolCallID string
	Name       string
	Content    string // JSON string to pass back to LLM
	Plan       *domainai.Plan // non-nil only for destructive tools
}

// Dispatch executes a single tool call identified by its name and JSON args.
// Safe tools execute immediately; destructive tools return a Plan.
func (r *Registry) Dispatch(ctx context.Context, tenantID uuid.UUID, call llmclient.ToolCall) DispatchResult {
	res := DispatchResult{ToolCallID: call.ID, Name: call.Function.Name}
	var resultJSON string
	var plan *domainai.Plan
	var err error

	switch call.Function.Name {
	case "search_products":
		resultJSON, err = r.searchProducts(ctx, tenantID, call.Function.Arguments)
	case "get_stock_summary":
		resultJSON, err = r.getStockSummary(ctx, tenantID)
	case "list_low_stock":
		resultJSON, err = r.listLowStock(ctx, tenantID, call.Function.Arguments)
	case "list_dead_stock":
		resultJSON, err = r.listDeadStock(ctx, tenantID, call.Function.Arguments)
	case "abc_classify":
		resultJSON, err = r.abcClassify(ctx, tenantID)
	case "recent_sales_top":
		resultJSON, err = r.recentSalesTop(ctx, tenantID, call.Function.Arguments)
	case "gross_margin_summary":
		resultJSON, err = r.grossMarginSummary(ctx, tenantID, call.Function.Arguments)
	case "query_exchange_rate":
		resultJSON, err = r.queryExchangeRate(ctx, tenantID, call.Function.Arguments)
	case "propose_price_change":
		plan, resultJSON, err = r.proposePriceChange(ctx, tenantID, call.Function.Arguments)
	case "propose_create_purchase_draft":
		plan, resultJSON, err = r.proposeCreatePurchaseDraft(ctx, tenantID, call.Function.Arguments)
	case "propose_bulk_stock_adjust":
		plan, resultJSON, err = r.proposeBulkStockAdjust(ctx, tenantID, call.Function.Arguments)
	default:
		resultJSON = `{"error":"unknown tool"}`
	}

	if err != nil {
		resultJSON = jsonError(err.Error())
	}

	res.Content = resultJSON
	res.Plan = plan
	return res
}

// --- Safe tool implementations ---

func (r *Registry) searchProducts(ctx context.Context, tenantID uuid.UUID, argsJSON string) (string, error) {
	var args struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("search_products: invalid args: %w", err)
	}
	rows, err := r.productRepo.SearchProducts(ctx, tenantID, args.Query)
	if err != nil {
		return "", fmt.Errorf("search_products: %w", err)
	}
	out := make([]map[string]interface{}, 0, len(rows))
	for _, p := range rows {
		out = append(out, map[string]interface{}{
			"id": p.ID, "name": p.Name, "code": p.Code,
			"brand": p.Brand, "mnemonic": p.Mnemonic,
		})
	}
	return jsonMarshal(map[string]interface{}{"products": out, "count": len(out)})
}

func (r *Registry) getStockSummary(ctx context.Context, tenantID uuid.UUID) (string, error) {
	rows, err := r.stockRepo.ListStockSnapshots(ctx, tenantID)
	if err != nil {
		return "", fmt.Errorf("get_stock_summary: %w", err)
	}
	totalValue := decimal.Zero
	lowStock := 0
	deadStock := 0
	now := time.Now()
	for _, s := range rows {
		totalValue = totalValue.Add(s.Qty.Mul(s.UnitCost))
		rop := computeROP(s)
		if s.Qty.LessThan(rop) {
			lowStock++
		}
		if now.Sub(s.LastMovedAt) > 90*24*time.Hour {
			deadStock++
		}
	}
	return jsonMarshal(map[string]interface{}{
		"total_skus":       len(rows),
		"total_value_cny":  totalValue.StringFixed(2),
		"low_stock_count":  lowStock,
		"dead_stock_count": deadStock,
	})
}

func (r *Registry) listLowStock(ctx context.Context, tenantID uuid.UUID, argsJSON string) (string, error) {
	var args struct {
		ThresholdDays *int `json:"threshold_days"`
	}
	_ = json.Unmarshal([]byte(argsJSON), &args)
	threshDays := 7
	if args.ThresholdDays != nil {
		threshDays = *args.ThresholdDays
	}

	rows, err := r.stockRepo.ListStockSnapshots(ctx, tenantID)
	if err != nil {
		return "", fmt.Errorf("list_low_stock: %w", err)
	}

	type item struct {
		Name        string `json:"name"`
		Code        string `json:"code"`
		Qty         string `json:"qty"`
		ROP         string `json:"rop"`
		DaysOfSupply string `json:"days_of_supply"`
	}
	var items []item
	for _, s := range rows {
		rop := computeROP(s)
		var daysOfSupply string
		if s.AvgDailySales.IsPositive() {
			d := s.Qty.Div(s.AvgDailySales)
			if d.LessThanOrEqual(decimal.NewFromInt(int64(threshDays))) {
				daysOfSupply = d.StringFixed(1)
				items = append(items, item{
					Name:        s.ProductName,
					Code:        s.ProductCode,
					Qty:         s.Qty.StringFixed(2),
					ROP:         rop.StringFixed(2),
					DaysOfSupply: daysOfSupply,
				})
			}
		} else if s.Qty.LessThan(rop) {
			items = append(items, item{
				Name:        s.ProductName,
				Code:        s.ProductCode,
				Qty:         s.Qty.StringFixed(2),
				ROP:         rop.StringFixed(2),
				DaysOfSupply: "N/A",
			})
		}
	}
	return jsonMarshal(map[string]interface{}{
		"low_stock_items": items,
		"count":           len(items),
		"threshold_days":  threshDays,
	})
}

func (r *Registry) listDeadStock(ctx context.Context, tenantID uuid.UUID, argsJSON string) (string, error) {
	var args struct {
		Days *int `json:"days"`
	}
	_ = json.Unmarshal([]byte(argsJSON), &args)
	days := 90
	if args.Days != nil {
		days = *args.Days
	}

	rows, err := r.stockRepo.ListStockSnapshots(ctx, tenantID)
	if err != nil {
		return "", fmt.Errorf("list_dead_stock: %w", err)
	}
	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	type item struct {
		Name       string `json:"name"`
		Code       string `json:"code"`
		Qty        string `json:"qty"`
		Value      string `json:"value_cny"`
		DaysSince  int    `json:"days_since_last_movement"`
	}
	var items []item
	for _, s := range rows {
		if s.LastMovedAt.Before(cutoff) {
			items = append(items, item{
				Name:      s.ProductName,
				Code:      s.ProductCode,
				Qty:       s.Qty.StringFixed(2),
				Value:     s.Qty.Mul(s.UnitCost).StringFixed(2),
				DaysSince: int(time.Since(s.LastMovedAt).Hours() / 24),
			})
		}
	}
	return jsonMarshal(map[string]interface{}{
		"dead_stock_items": items,
		"count":            len(items),
		"threshold_days":   days,
	})
}

func (r *Registry) abcClassify(ctx context.Context, tenantID uuid.UUID) (string, error) {
	saleRows, err := r.saleRepo.ListRecentSaleLines(ctx, tenantID, 365)
	if err != nil {
		return "", fmt.Errorf("abc_classify: %w", err)
	}

	// Aggregate revenue per product.
	rev := make(map[uuid.UUID]decimal.Decimal)
	names := make(map[uuid.UUID]string)
	for _, s := range saleRows {
		rev[s.ProductID] = rev[s.ProductID].Add(s.Revenue)
		names[s.ProductID] = s.ProductName
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
		aCnt, bCnt, cCnt     int
		aRev, bRev, cRev     decimal.Decimal
		cumulative            = decimal.Zero
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
		"a": map[string]interface{}{"sku_count": aCnt, "revenue_share": safeShare(aRev)},
		"b": map[string]interface{}{"sku_count": bCnt, "revenue_share": safeShare(bRev)},
		"c": map[string]interface{}{"sku_count": cCnt, "revenue_share": safeShare(cRev)},
		"total_skus": len(list),
		"period":     "365d",
	})
}

func (r *Registry) recentSalesTop(ctx context.Context, tenantID uuid.UUID, argsJSON string) (string, error) {
	var args struct {
		Metric string `json:"metric"`
		Days   *int   `json:"days"`
		Limit  *int   `json:"limit"`
	}
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

func (r *Registry) grossMarginSummary(ctx context.Context, tenantID uuid.UUID, argsJSON string) (string, error) {
	var args struct {
		Days *int `json:"days"`
	}
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
		name        string
		avgMargin   decimal.Decimal
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

func (r *Registry) queryExchangeRate(ctx context.Context, tenantID uuid.UUID, argsJSON string) (string, error) {
	var args struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
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

// --- Destructive tool implementations (return Plan, no side effects) ---

func (r *Registry) proposePriceChange(ctx context.Context, tenantID uuid.UUID, argsJSON string) (*domainai.Plan, string, error) {
	var args struct {
		Filter string `json:"filter"`
		Action string `json:"action"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return nil, "", fmt.Errorf("propose_price_change: invalid args: %w", err)
	}

	products, err := r.productRepo.SearchProducts(ctx, tenantID, args.Filter)
	if err != nil {
		return nil, "", fmt.Errorf("propose_price_change: search: %w", err)
	}

	samples := make([]domainai.SampleRow, 0, min(len(products), 10))
	for i, p := range products {
		if i >= 10 {
			break
		}
		samples = append(samples, domainai.SampleRow{
			Name:   p.Name,
			Before: "(current price)",
			After:  args.Action,
		})
	}

	plan := &domainai.Plan{
		ID:       uuid.New(),
		TenantID: tenantID,
		Type:     domainai.PlanTypePriceChange,
		Status:   domainai.PlanStatusPending,
		Payload: map[string]interface{}{
			"filter": args.Filter,
			"action": args.Action,
		},
		Preview: domainai.PlanPreview{
			Description:   fmt.Sprintf("Change price of %d products matching '%s' by %s", len(products), args.Filter, args.Action),
			AffectedCount: len(products),
			SampleRows:    samples,
		},
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(30 * time.Minute),
	}

	result, err := jsonMarshal(map[string]interface{}{
		"plan_id":        plan.ID.String(),
		"affected_count": len(products),
		"description":    plan.Preview.Description,
		"requires_confirmation": true,
		"message": "This operation requires user confirmation. A plan card has been shown to the user.",
	})
	return plan, result, err
}

func (r *Registry) proposeCreatePurchaseDraft(ctx context.Context, tenantID uuid.UUID, argsJSON string) (*domainai.Plan, string, error) {
	var args struct {
		Items []struct {
			ProductName string  `json:"product_name"`
			Qty         float64 `json:"qty"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return nil, "", fmt.Errorf("propose_create_purchase_draft: invalid args: %w", err)
	}

	samples := make([]domainai.SampleRow, 0, len(args.Items))
	for _, it := range args.Items {
		samples = append(samples, domainai.SampleRow{
			Name:   it.ProductName,
			Before: "",
			After:  fmt.Sprintf("qty: %.2f", it.Qty),
		})
	}

	plan := &domainai.Plan{
		ID:       uuid.New(),
		TenantID: tenantID,
		Type:     domainai.PlanTypeCreatePurchase,
		Status:   domainai.PlanStatusPending,
		Payload:  map[string]interface{}{"items": args.Items},
		Preview: domainai.PlanPreview{
			Description:   fmt.Sprintf("Create purchase draft with %d line items", len(args.Items)),
			AffectedCount: len(args.Items),
			SampleRows:    samples,
		},
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(30 * time.Minute),
	}

	result, err := jsonMarshal(map[string]interface{}{
		"plan_id":              plan.ID.String(),
		"item_count":           len(args.Items),
		"description":          plan.Preview.Description,
		"requires_confirmation": true,
		"message":              "This operation requires user confirmation. A plan card has been shown to the user.",
	})
	return plan, result, err
}

func (r *Registry) proposeBulkStockAdjust(ctx context.Context, tenantID uuid.UUID, argsJSON string) (*domainai.Plan, string, error) {
	var args struct {
		Filter string  `json:"filter"`
		Delta  float64 `json:"delta"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return nil, "", fmt.Errorf("propose_bulk_stock_adjust: invalid args: %w", err)
	}

	products, err := r.productRepo.SearchProducts(ctx, tenantID, args.Filter)
	if err != nil {
		return nil, "", fmt.Errorf("propose_bulk_stock_adjust: search: %w", err)
	}

	samples := make([]domainai.SampleRow, 0, min(len(products), 10))
	for i, p := range products {
		if i >= 10 {
			break
		}
		samples = append(samples, domainai.SampleRow{
			Name:   p.Name,
			Before: "(current qty)",
			After:  fmt.Sprintf("Δ%+.2f", args.Delta),
		})
	}

	plan := &domainai.Plan{
		ID:       uuid.New(),
		TenantID: tenantID,
		Type:     domainai.PlanTypeBulkStockAdjust,
		Status:   domainai.PlanStatusPending,
		Payload: map[string]interface{}{
			"filter": args.Filter,
			"delta":  args.Delta,
		},
		Preview: domainai.PlanPreview{
			Description:   fmt.Sprintf("Adjust stock of %d products matching '%s' by %+.2f", len(products), args.Filter, args.Delta),
			AffectedCount: len(products),
			SampleRows:    samples,
		},
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(30 * time.Minute),
	}

	result, err := jsonMarshal(map[string]interface{}{
		"plan_id":              plan.ID.String(),
		"affected_count":       len(products),
		"description":          plan.Preview.Description,
		"requires_confirmation": true,
		"message":              "This operation requires user confirmation. A plan card has been shown to the user.",
	})
	return plan, result, err
}

// --- helpers ---

// computeROP calculates the Re-Order Point using industry formula:
// ROP = lead_time_days × avg_daily_sales + safety_stock
// safety_stock = z × σ × √lead_time (z=1.65, σ approximated as avg_daily_sales * 0.3)
func computeROP(s StockRow) decimal.Decimal {
	if s.AvgDailySales.IsZero() || s.LeadTimeDays == 0 {
		return decimal.Zero
	}
	lt := decimal.NewFromInt(int64(s.LeadTimeDays))
	avgDaily := s.AvgDailySales
	// safety stock: z=1.65, σ = avgDaily * 0.3 (conservative approximation)
	z := decimal.NewFromFloat(1.65)
	sigma := avgDaily.Mul(decimal.NewFromFloat(0.3))
	sqrtLT := decimal.NewFromFloat(math.Sqrt(float64(s.LeadTimeDays)))
	safetyStock := z.Mul(sigma).Mul(sqrtLT)
	return lt.Mul(avgDaily).Add(safetyStock)
}

func jsonMarshal(v interface{}) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("json marshal: %w", err)
	}
	return string(b), nil
}

func jsonError(msg string) string {
	b, _ := json.Marshal(map[string]string{"error": msg})
	return string(b)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
