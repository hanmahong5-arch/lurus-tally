// Package ai implements the AI assistant use cases and function-calling tool registry.
package ai

import (
	"context"
	"encoding/json"
	"fmt"
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
	ProductID     uuid.UUID
	ProductName   string
	ProductCode   string
	Qty           decimal.Decimal
	UnitCost      decimal.Decimal
	LastMovedAt   time.Time
	AvgDailySales decimal.Decimal
	LeadTimeDays  int
}

// SaleRow is a minimal sale line row used by analytics tools.
type SaleRow struct {
	ProductID   uuid.UUID
	ProductName string
	Qty         decimal.Decimal
	Revenue     decimal.Decimal
	Margin      decimal.Decimal
	SoldAt      time.Time
}

// CustomerRef identifies a customer or walk-in buyer referenced in a query.
type CustomerRef struct {
	ID    uuid.UUID
	Name  string
	Code  string
	Phone string
}

// CustomerBill is one approved sale bill of a customer, with its lines.
type CustomerBill struct {
	BillNo   string
	BillDate time.Time
	Total    decimal.Decimal
	Lines    []CustomerBillLine
}

// CustomerBillLine is one line of a CustomerBill.
type CustomerBillLine struct {
	ProductName string
	Unit        string
	Qty         decimal.Decimal
	UnitPrice   decimal.Decimal
	Amount      decimal.Decimal
}

// ProductRepo is the minimal read interface required by the AI tools.
type ProductRepo interface {
	SearchProducts(ctx context.Context, tenantID uuid.UUID, query string) ([]ProductRow, error)
	ListAllProducts(ctx context.Context, tenantID uuid.UUID) ([]ProductRow, error)
}

// StockRepo is the minimal read interface required by the AI tools.
type StockRepo interface {
	ListStockSnapshots(ctx context.Context, tenantID uuid.UUID) ([]StockRow, error)
}

// SaleRepo is the minimal read interface required by the AI tools.
type SaleRepo interface {
	ListRecentSaleLines(ctx context.Context, tenantID uuid.UUID, days int) ([]SaleRow, error)
	MatchCustomers(ctx context.Context, tenantID uuid.UUID, name string) ([]CustomerRef, error)
	ListCustomerSales(ctx context.Context, tenantID uuid.UUID, c CustomerRef, limit int) ([]CustomerBill, error)
}

// ExchangeRateRepo is the minimal interface for exchange rate queries.
type ExchangeRateRepo interface {
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

// ToolDefs returns the chat-completions-compatible tool definitions for chat requests.
func ToolDefs() []llmclient.Tool {
	return []llmclient.Tool{
		{Type: "function", Function: llmclient.FunctionDef{
			Name:        "search_products",
			Description: "Full-text search products by name, code, mnemonic, or brand.",
			Parameters:  toolParams(searchProductsArgs{}),
		}},
		{Type: "function", Function: llmclient.FunctionDef{
			Name:        "get_stock_summary",
			Description: "Overall warehouse overview ONLY (库存总体情况 / 仓库概况): total SKUs and total inventory value (CNY). Do NOT use this as a preliminary survey for replenishment (补货 → list_low_stock), dead-stock (滞销 → list_dead_stock), or margin (毛利 → gross_margin_summary) questions — call the dedicated tool directly instead.",
			Parameters:  toolParams(noArgs{}),
		}},
		{Type: "function", Function: llmclient.FunctionDef{
			Name:        "list_low_stock",
			Description: "Lists SKUs where current quantity is below the re-order point (ROP). Returns product name, qty, ROP, and days of supply. Call this for 补货/该进多少货/复库/缺货预警/库存不够 questions — it IS the replenishment calculation.",
			Parameters:  toolParams(listLowStockArgs{}),
		}},
		{Type: "function", Function: llmclient.FunctionDef{
			Name:        "list_dead_stock",
			Description: "Lists SKUs with no stock movement in the past N days (dead stock / slow-moving inventory). Call this for 滞销/呆滞/库存积压/卖不动 questions.",
			Parameters:  toolParams(listDeadStockArgs{}),
		}},
		{Type: "function", Function: llmclient.FunctionDef{
			Name:        "abc_classify",
			Description: "ABC classification of products by sales revenue: A=top 80%, B=next 15%, C=bottom 5%. Returns SKU count and cumulative revenue share per tier. Call this for ABC分类/帕累托 questions.",
			Parameters:  toolParams(noArgs{}),
		}},
		{Type: "function", Function: llmclient.FunctionDef{
			Name:        "recent_sales_top",
			Description: "Top-N products by revenue, margin, or quantity over recent N days. Call this for 畅销/爆款/排行/卖得最好 questions.",
			Parameters:  toolParams(recentSalesTopArgs{}),
		}},
		{Type: "function", Function: llmclient.FunctionDef{
			Name:        "gross_margin_summary",
			Description: "Overall gross margin over the past N days, plus top-10 highest margin and bottom-10 lowest margin products. Call this for 毛利/利润率最低/最高 questions.",
			Parameters:  toolParams(grossMarginSummaryArgs{}),
		}},
		{Type: "function", Function: llmclient.FunctionDef{
			Name:        "customer_recent_purchases",
			Description: "A customer's latest sale bills with every line (date, product, qty, unit price, amount), read from the books. Call this for 上次买了什么/买过什么/最近买了什么/老客户 又来了 questions about a named customer. Pass the customer as the user said it, including address forms like 老张/王老板/李总 — they are resolved by surname. If several customers match the name it returns the candidates instead of guessing — ask the user which one.",
			Parameters:  toolParams(customerRecentPurchasesArgs{}),
		}},
		{Type: "function", Function: llmclient.FunctionDef{
			Name:        "query_exchange_rate",
			Description: "Returns the current exchange rate from one currency to CNY (or another target).",
			Parameters:  toolParams(queryExchangeRateArgs{}),
		}},
		{Type: "function", Function: llmclient.FunctionDef{
			Name:        "propose_price_change",
			Description: "DESTRUCTIVE: Propose a bulk price change for matching products. Returns a plan_id for user confirmation. Does NOT execute immediately.",
			Parameters:  toolParams(proposePriceChangeArgs{}),
		}},
		{Type: "function", Function: llmclient.FunctionDef{
			Name:        "propose_create_purchase_draft",
			Description: "DESTRUCTIVE: Propose creation of a purchase order draft. Returns a plan_id for user confirmation. Does NOT execute immediately.",
			Parameters:  toolParams(proposeCreatePurchaseDraftArgs{}),
		}},
		{Type: "function", Function: llmclient.FunctionDef{
			Name:        "propose_bulk_stock_adjust",
			Description: "DESTRUCTIVE: Propose a bulk stock quantity adjustment for matching products. Returns a plan_id for user confirmation. Does NOT execute immediately.",
			Parameters:  toolParams(proposeBulkStockAdjustArgs{}),
		}},
	}
}

// DispatchResult holds the tool call result JSON and optional confirmation Plan.
type DispatchResult struct {
	ToolCallID string
	Name       string
	Content    string
	Plan       *domainai.Plan
}

// Dispatch executes a single tool call identified by its name and JSON arguments.
func (r *Registry) Dispatch(ctx context.Context, tenantID uuid.UUID, call llmclient.ToolCall) DispatchResult {
	res := DispatchResult{ToolCallID: call.ID, Name: call.Function.Name}
	var resultJSON string
	var plan *domainai.Plan
	var err error

	if costHidden(ctx) && (costToolNames[call.Function.Name] || isMarginTop(call)) {
		res.Content = costDeniedResult
		return res
	}

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
	case "customer_recent_purchases":
		resultJSON, err = r.customerRecentPurchases(ctx, tenantID, call.Function.Arguments)
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
	if costHidden(ctx) {
		resultJSON = trimCostResult(resultJSON)
	}
	res.Content = resultJSON
	res.Plan = plan
	return res
}

// isMarginTop reports whether a recent_sales_top call ranks by margin.
func isMarginTop(call llmclient.ToolCall) bool {
	if call.Function.Name != "recent_sales_top" {
		return false
	}
	var a struct {
		Metric string `json:"metric"`
	}
	_ = json.Unmarshal([]byte(call.Function.Arguments), &a)
	return a.Metric == "margin"
}

// jsonMarshal serialises v into a JSON string.
func jsonMarshal(v interface{}) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("json marshal: %w", err)
	}
	return string(b), nil
}

// jsonError formats msg as a JSON error payload.
func jsonError(msg string) string {
	b, _ := json.Marshal(map[string]string{"error": msg})
	return string(b)
}

// min returns the smaller of a and b.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
