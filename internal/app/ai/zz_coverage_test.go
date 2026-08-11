package ai

// zz_coverage_test.go is an internal test file (package ai, not ai_test) so it
// can exercise the tool-dispatch and orchestrator internals that the existing
// external tests (package ai_test) cannot reach without an exported seam.
//
// Fakes in this file are prefixed "cx" to avoid any accidental name clash with
// fakes declared in the sibling ai_test package (different package namespace,
// so collisions are impossible, but the prefix keeps intent obvious).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	domainai "github.com/hanmahong5-arch/lurus-tally/internal/domain/ai"
	llmobs "github.com/hanmahong5-arch/lurus-tally/internal/observability/llm"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/llmclient"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/memorusclient"
)

// ============================================================================
// Shared fakes
// ============================================================================

type cxProductRepo struct {
	rows []ProductRow
	err  error
}

func (f *cxProductRepo) SearchProducts(_ context.Context, _ uuid.UUID, _ string) ([]ProductRow, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.rows, nil
}
func (f *cxProductRepo) ListAllProducts(_ context.Context, _ uuid.UUID) ([]ProductRow, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.rows, nil
}

type cxStockRepo struct {
	rows []StockRow
	err  error
}

func (f *cxStockRepo) ListStockSnapshots(_ context.Context, _ uuid.UUID) ([]StockRow, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.rows, nil
}

type cxSaleRepo struct {
	rows []SaleRow
	err  error
}

func (f *cxSaleRepo) ListRecentSaleLines(_ context.Context, _ uuid.UUID, _ int) ([]SaleRow, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.rows, nil
}

type cxExchangeRepo struct {
	rate decimal.Decimal
	err  error
}

func (f *cxExchangeRepo) GetRate(_ context.Context, _ uuid.UUID, _, _ string) (decimal.Decimal, error) {
	if f.err != nil {
		return decimal.Zero, f.err
	}
	return f.rate, nil
}

func callTool(r *Registry, tenantID uuid.UUID, name, args string) DispatchResult {
	return r.Dispatch(context.Background(), tenantID, llmclient.ToolCall{
		ID: "call-1", Type: "function",
		Function: llmclient.ToolCallFunction{Name: name, Arguments: args},
	})
}

func decodeJSON(t *testing.T, s string) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("dispatch result is not valid JSON: %v\nbody: %s", err, s)
	}
	return m
}

// ============================================================================
// tools.go — Dispatch coverage
// ============================================================================

func TestDispatch_SearchProducts_HappyAndErrors(t *testing.T) {
	pid := uuid.New()
	registry := NewRegistry(&cxProductRepo{rows: []ProductRow{{ID: pid, Name: "Widget A", Code: "W1", Brand: "ACME"}}}, &cxStockRepo{}, &cxSaleRepo{}, &cxExchangeRepo{})

	res := callTool(registry, uuid.New(), "search_products", `{"query":"Widget"}`)
	body := decodeJSON(t, res.Content)
	if body["count"].(float64) != 1 {
		t.Errorf("count=%v, want 1", body["count"])
	}

	// invalid JSON args.
	res = callTool(registry, uuid.New(), "search_products", `{bad json`)
	errBody := decodeJSON(t, res.Content)
	if errBody["error"] == nil {
		t.Errorf("expected error field for invalid args, got %s", res.Content)
	}

	// repo error.
	registryErr := NewRegistry(&cxProductRepo{err: errors.New("db down")}, &cxStockRepo{}, &cxSaleRepo{}, &cxExchangeRepo{})
	res = callTool(registryErr, uuid.New(), "search_products", `{"query":"x"}`)
	errBody = decodeJSON(t, res.Content)
	if errBody["error"] == nil {
		t.Errorf("expected error field for repo error, got %s", res.Content)
	}
}

func TestDispatch_GetStockSummary_ComputesTotalsAndCounts(t *testing.T) {
	now := time.Now()
	rows := []StockRow{
		// not low stock (qty 10 > rop ~6.1), not dead (moved now).
		{ProductID: uuid.New(), Qty: decimal.NewFromInt(10), UnitCost: decimal.NewFromInt(5), AvgDailySales: decimal.NewFromInt(1), LeadTimeDays: 5, LastMovedAt: now},
		// low stock (qty 1 < rop ~6.1) and dead (moved 100d ago).
		{ProductID: uuid.New(), Qty: decimal.NewFromInt(1), UnitCost: decimal.NewFromInt(2), AvgDailySales: decimal.NewFromInt(1), LeadTimeDays: 5, LastMovedAt: now.Add(-100 * 24 * time.Hour)},
	}
	registry := NewRegistry(&cxProductRepo{}, &cxStockRepo{rows: rows}, &cxSaleRepo{}, &cxExchangeRepo{})

	res := callTool(registry, uuid.New(), "get_stock_summary", `{}`)
	body := decodeJSON(t, res.Content)
	if body["total_skus"].(float64) != 2 {
		t.Errorf("total_skus=%v, want 2", body["total_skus"])
	}
	if body["total_value_cny"] != "52.00" {
		t.Errorf("total_value_cny=%v, want 52.00", body["total_value_cny"])
	}
	if body["low_stock_count"].(float64) != 1 {
		t.Errorf("low_stock_count=%v, want 1", body["low_stock_count"])
	}
	if body["dead_stock_count"].(float64) != 1 {
		t.Errorf("dead_stock_count=%v, want 1", body["dead_stock_count"])
	}

	// repo error.
	registryErr := NewRegistry(&cxProductRepo{}, &cxStockRepo{err: errors.New("boom")}, &cxSaleRepo{}, &cxExchangeRepo{})
	res = callTool(registryErr, uuid.New(), "get_stock_summary", `{}`)
	if decodeJSON(t, res.Content)["error"] == nil {
		t.Error("expected error field on repo failure")
	}
}

func TestDispatch_ListLowStock_ThresholdBranches(t *testing.T) {
	// RowA: AvgDailySales>0 branch. lt=4, avgDaily=2 -> rop=9.98 exactly. qty=10 -> d=5.
	rowA := StockRow{ProductName: "A", Qty: decimal.NewFromInt(10), AvgDailySales: decimal.NewFromInt(2), LeadTimeDays: 4}
	// RowB: AvgDailySales==0 branch. rop=0 (computeROP zero-guard). qty=-1 < 0 -> included as N/A.
	rowB := StockRow{ProductName: "B", Qty: decimal.NewFromInt(-1), AvgDailySales: decimal.Zero, LeadTimeDays: 4}
	registry := NewRegistry(&cxProductRepo{}, &cxStockRepo{rows: []StockRow{rowA, rowB}}, &cxSaleRepo{}, &cxExchangeRepo{})

	// default threshold (7 days): both included (A: d=5<=7, B: qty<rop via zero branch).
	res := callTool(registry, uuid.New(), "list_low_stock", `{}`)
	body := decodeJSON(t, res.Content)
	if body["count"].(float64) != 2 {
		t.Errorf("default threshold count=%v, want 2", body["count"])
	}
	items := body["low_stock_items"].([]interface{})
	var gotA, gotB bool
	for _, it := range items {
		m := it.(map[string]interface{})
		if m["name"] == "A" {
			gotA = true
			if m["rop"] != "9.98" {
				t.Errorf("A rop=%v, want 9.98", m["rop"])
			}
			if m["days_of_supply"] != "5.0" {
				t.Errorf("A days_of_supply=%v, want 5.0", m["days_of_supply"])
			}
		}
		if m["name"] == "B" {
			gotB = true
			if m["days_of_supply"] != "N/A" {
				t.Errorf("B days_of_supply=%v, want N/A", m["days_of_supply"])
			}
		}
	}
	if !gotA || !gotB {
		t.Errorf("expected both A and B in default-threshold result, got %+v", items)
	}

	// custom threshold=3: A's d=5 no longer <=3 -> excluded; B still included via zero branch.
	res = callTool(registry, uuid.New(), "list_low_stock", `{"threshold_days":3}`)
	body = decodeJSON(t, res.Content)
	if body["count"].(float64) != 1 {
		t.Errorf("threshold=3 count=%v, want 1", body["count"])
	}

	// repo error.
	registryErr := NewRegistry(&cxProductRepo{}, &cxStockRepo{err: errors.New("x")}, &cxSaleRepo{}, &cxExchangeRepo{})
	res = callTool(registryErr, uuid.New(), "list_low_stock", `{}`)
	if decodeJSON(t, res.Content)["error"] == nil {
		t.Error("expected error field on repo failure")
	}
}

func TestDispatch_ListDeadStock_DefaultAndCustomDays(t *testing.T) {
	now := time.Now()
	rowX := StockRow{ProductName: "X", Qty: decimal.NewFromInt(5), UnitCost: decimal.NewFromInt(10), LastMovedAt: now.Add(-100 * 24 * time.Hour)}
	rowY := StockRow{ProductName: "Y", Qty: decimal.NewFromInt(1), UnitCost: decimal.NewFromInt(1), LastMovedAt: now.Add(-10 * 24 * time.Hour)}
	registry := NewRegistry(&cxProductRepo{}, &cxStockRepo{rows: []StockRow{rowX, rowY}}, &cxSaleRepo{}, &cxExchangeRepo{})

	// default 90 days: X (100d ago) is dead, Y (10d ago) is not.
	res := callTool(registry, uuid.New(), "list_dead_stock", `{}`)
	body := decodeJSON(t, res.Content)
	if body["count"].(float64) != 1 {
		t.Errorf("default days count=%v, want 1", body["count"])
	}
	items := body["dead_stock_items"].([]interface{})
	if len(items) != 1 || items[0].(map[string]interface{})["value_cny"] != "50.00" {
		t.Errorf("unexpected dead stock items: %+v", items)
	}

	// custom days=200: X (100d ago) no longer before cutoff (200d ago) -> excluded.
	res = callTool(registry, uuid.New(), "list_dead_stock", `{"days":200}`)
	body = decodeJSON(t, res.Content)
	if body["count"].(float64) != 0 {
		t.Errorf("days=200 count=%v, want 0", body["count"])
	}

	registryErr := NewRegistry(&cxProductRepo{}, &cxStockRepo{err: errors.New("x")}, &cxSaleRepo{}, &cxExchangeRepo{})
	res = callTool(registryErr, uuid.New(), "list_dead_stock", `{}`)
	if decodeJSON(t, res.Content)["error"] == nil {
		t.Error("expected error field on repo failure")
	}
}

func TestDispatch_ABCClassify_TiersAndEmpty(t *testing.T) {
	// Exact decimal split: A=800 (80%), B=150 (15%), C=50 (5%) of total 1000.
	pA, pB, pC := uuid.New(), uuid.New(), uuid.New()
	rows := []SaleRow{
		{ProductID: pA, ProductName: "A", Revenue: decimal.NewFromInt(800)},
		{ProductID: pB, ProductName: "B", Revenue: decimal.NewFromInt(150)},
		{ProductID: pC, ProductName: "C", Revenue: decimal.NewFromInt(50)},
	}
	registry := NewRegistry(&cxProductRepo{}, &cxStockRepo{}, &cxSaleRepo{rows: rows}, &cxExchangeRepo{})
	res := callTool(registry, uuid.New(), "abc_classify", `{}`)
	body := decodeJSON(t, res.Content)
	a := body["a"].(map[string]interface{})
	b := body["b"].(map[string]interface{})
	c := body["c"].(map[string]interface{})
	if a["sku_count"].(float64) != 1 || a["revenue_share"] != "80.0%" {
		t.Errorf("tier a=%+v, want 1/80.0%%", a)
	}
	if b["sku_count"].(float64) != 1 || b["revenue_share"] != "15.0%" {
		t.Errorf("tier b=%+v, want 1/15.0%%", b)
	}
	if c["sku_count"].(float64) != 1 || c["revenue_share"] != "5.0%" {
		t.Errorf("tier c=%+v, want 1/5.0%%", c)
	}

	// empty sales -> zero tiers, safeShare returns "0%" via total.IsZero() guard.
	emptyRegistry := NewRegistry(&cxProductRepo{}, &cxStockRepo{}, &cxSaleRepo{}, &cxExchangeRepo{})
	res = callTool(emptyRegistry, uuid.New(), "abc_classify", `{}`)
	body = decodeJSON(t, res.Content)
	if body["total_skus"].(float64) != 0 {
		t.Errorf("total_skus=%v, want 0", body["total_skus"])
	}
	a = body["a"].(map[string]interface{})
	if a["revenue_share"] != "0%" {
		t.Errorf("empty a revenue_share=%v, want 0%%", a["revenue_share"])
	}

	// repo error.
	registryErr := NewRegistry(&cxProductRepo{}, &cxStockRepo{}, &cxSaleRepo{err: errors.New("x")}, &cxExchangeRepo{})
	res = callTool(registryErr, uuid.New(), "abc_classify", `{}`)
	if decodeJSON(t, res.Content)["error"] == nil {
		t.Error("expected error field on repo failure")
	}
}

func TestDispatch_RecentSalesTop_MetricsAndLimitAndErrors(t *testing.T) {
	p1, p2 := uuid.New(), uuid.New()
	rows := []SaleRow{
		{ProductID: p1, ProductName: "P1", Revenue: decimal.NewFromInt(100), Qty: decimal.NewFromInt(3), Margin: decimal.NewFromFloat(0.5)},
		{ProductID: p2, ProductName: "P2", Revenue: decimal.NewFromInt(50), Qty: decimal.NewFromInt(9), Margin: decimal.NewFromFloat(0.1)},
	}
	registry := NewRegistry(&cxProductRepo{}, &cxStockRepo{}, &cxSaleRepo{rows: rows}, &cxExchangeRepo{})

	cases := []struct {
		metric    string
		wantFirst string
	}{
		{"revenue", "P1"}, // 100 > 50
		{"margin", "P1"},  // 0.5 > 0.1
		{"qty", "P2"},     // 9 > 3
		{"", "P1"},        // default clause uses revenue
	}
	for _, c := range cases {
		res := callTool(registry, uuid.New(), "recent_sales_top", fmt.Sprintf(`{"metric":%q}`, c.metric))
		body := decodeJSON(t, res.Content)
		top := body["top_products"].([]interface{})
		if len(top) == 0 {
			t.Fatalf("metric=%q: empty top_products", c.metric)
		}
		got := top[0].(map[string]interface{})["name"]
		if got != c.wantFirst {
			t.Errorf("metric=%q: first=%v, want %s", c.metric, got, c.wantFirst)
		}
	}

	// limit truncation.
	res := callTool(registry, uuid.New(), "recent_sales_top", `{"metric":"revenue","limit":1}`)
	body := decodeJSON(t, res.Content)
	if len(body["top_products"].([]interface{})) != 1 {
		t.Errorf("expected limit=1 to truncate results")
	}

	// invalid args.
	res = callTool(registry, uuid.New(), "recent_sales_top", `{bad`)
	if decodeJSON(t, res.Content)["error"] == nil {
		t.Error("expected error for invalid args")
	}

	// repo error.
	registryErr := NewRegistry(&cxProductRepo{}, &cxStockRepo{}, &cxSaleRepo{err: errors.New("x")}, &cxExchangeRepo{})
	res = callTool(registryErr, uuid.New(), "recent_sales_top", `{"metric":"revenue"}`)
	if decodeJSON(t, res.Content)["error"] == nil {
		t.Error("expected error on repo failure")
	}
}

func TestDispatch_GrossMarginSummary_SlicingAndEmpty(t *testing.T) {
	// 12 products, avg margin i/100 for i in 1..12 (one sale row each => avg==margin).
	var rows []SaleRow
	for i := 1; i <= 12; i++ {
		rows = append(rows, SaleRow{
			ProductID: uuid.New(), ProductName: fmt.Sprintf("P%d", i),
			Revenue: decimal.NewFromInt(10), Margin: decimal.NewFromFloat(float64(i) / 100),
		})
	}
	registry := NewRegistry(&cxProductRepo{}, &cxStockRepo{}, &cxSaleRepo{rows: rows}, &cxExchangeRepo{})
	res := callTool(registry, uuid.New(), "gross_margin_summary", `{}`)
	body := decodeJSON(t, res.Content)
	top10 := body["top10"].([]interface{})
	bottom10 := body["bottom10"].([]interface{})
	if len(top10) != 10 || len(bottom10) != 10 {
		t.Fatalf("top10=%d bottom10=%d, want 10/10", len(top10), len(bottom10))
	}
	// top10 must exclude the two lowest-margin products (P1, P2).
	for _, it := range top10 {
		name := it.(map[string]interface{})["name"]
		if name == "P1" || name == "P2" {
			t.Errorf("top10 must exclude lowest-margin products, found %v", name)
		}
	}
	// bottom10 must exclude the two highest-margin products (P11, P12).
	for _, it := range bottom10 {
		name := it.(map[string]interface{})["name"]
		if name == "P11" || name == "P12" {
			t.Errorf("bottom10 must exclude highest-margin products, found %v", name)
		}
	}
	// overallMargin = sum(0.01..0.12)/12 = 0.78/12 = 0.065 -> 6.5%
	if body["overall_margin"] != "6.5%" {
		t.Errorf("overall_margin=%v, want 6.5%%", body["overall_margin"])
	}

	// empty rows -> overallMargin zero, no division by zero.
	emptyRegistry := NewRegistry(&cxProductRepo{}, &cxStockRepo{}, &cxSaleRepo{}, &cxExchangeRepo{})
	res = callTool(emptyRegistry, uuid.New(), "gross_margin_summary", `{"days":10}`)
	body = decodeJSON(t, res.Content)
	if body["overall_margin"] != "0.0%" {
		t.Errorf("empty overall_margin=%v, want 0.0%%", body["overall_margin"])
	}

	registryErr := NewRegistry(&cxProductRepo{}, &cxStockRepo{}, &cxSaleRepo{err: errors.New("x")}, &cxExchangeRepo{})
	res = callTool(registryErr, uuid.New(), "gross_margin_summary", `{}`)
	if decodeJSON(t, res.Content)["error"] == nil {
		t.Error("expected error on repo failure")
	}
}

func TestDispatch_QueryExchangeRate_DefaultToCNYAndErrors(t *testing.T) {
	registry := NewRegistry(&cxProductRepo{}, &cxStockRepo{}, &cxSaleRepo{}, &cxExchangeRepo{rate: decimal.NewFromFloat(7.123456)})

	res := callTool(registry, uuid.New(), "query_exchange_rate", `{"from":"USD"}`)
	body := decodeJSON(t, res.Content)
	if body["to"] != "CNY" {
		t.Errorf("to=%v, want default CNY", body["to"])
	}
	if body["rate"] != "7.123456" {
		t.Errorf("rate=%v, want 7.123456", body["rate"])
	}

	res = callTool(registry, uuid.New(), "query_exchange_rate", `{"from":"USD","to":"EUR"}`)
	body = decodeJSON(t, res.Content)
	if body["to"] != "EUR" {
		t.Errorf("to=%v, want EUR", body["to"])
	}

	res = callTool(registry, uuid.New(), "query_exchange_rate", `{bad`)
	if decodeJSON(t, res.Content)["error"] == nil {
		t.Error("expected error for invalid args")
	}

	registryErr := NewRegistry(&cxProductRepo{}, &cxStockRepo{}, &cxSaleRepo{}, &cxExchangeRepo{err: errors.New("no rate")})
	res = callTool(registryErr, uuid.New(), "query_exchange_rate", `{"from":"USD"}`)
	if decodeJSON(t, res.Content)["error"] == nil {
		t.Error("expected error on repo failure")
	}
}

func manyProducts(n int) []ProductRow {
	rows := make([]ProductRow, n)
	for i := range rows {
		rows[i] = ProductRow{ID: uuid.New(), Name: fmt.Sprintf("P%d", i)}
	}
	return rows
}

func TestDispatch_ProposePriceChange_CreatesPlanAndCapsSamples(t *testing.T) {
	// small count (<10): min(a,b) returns a branch.
	registry := NewRegistry(&cxProductRepo{rows: manyProducts(2)}, &cxStockRepo{}, &cxSaleRepo{}, &cxExchangeRepo{})
	res := callTool(registry, uuid.New(), "propose_price_change", `{"filter":"brand-x","action":"+5%"}`)
	body := decodeJSON(t, res.Content)
	if body["affected_count"].(float64) != 2 {
		t.Errorf("affected_count=%v, want 2", body["affected_count"])
	}

	// large count (>10): min(a,b) returns b branch; samples capped at 10.
	registryBig := NewRegistry(&cxProductRepo{rows: manyProducts(12)}, &cxStockRepo{}, &cxSaleRepo{}, &cxExchangeRepo{})
	res = callTool(registryBig, uuid.New(), "propose_price_change", `{"filter":"all","action":"-10%"}`)
	body = decodeJSON(t, res.Content)
	if body["affected_count"].(float64) != 12 {
		t.Errorf("affected_count=%v, want 12", body["affected_count"])
	}
	if body["requires_confirmation"] != true {
		t.Errorf("requires_confirmation=%v, want true", body["requires_confirmation"])
	}

	res = callTool(registryBig, uuid.New(), "propose_price_change", `{bad`)
	if decodeJSON(t, res.Content)["error"] == nil {
		t.Error("expected error for invalid args")
	}

	registryErr := NewRegistry(&cxProductRepo{err: errors.New("x")}, &cxStockRepo{}, &cxSaleRepo{}, &cxExchangeRepo{})
	res = callTool(registryErr, uuid.New(), "propose_price_change", `{"filter":"x","action":"+1%"}`)
	if decodeJSON(t, res.Content)["error"] == nil {
		t.Error("expected error on repo failure")
	}
}

func TestDispatch_ProposeCreatePurchaseDraft_CreatesPlan(t *testing.T) {
	registry := NewRegistry(&cxProductRepo{}, &cxStockRepo{}, &cxSaleRepo{}, &cxExchangeRepo{})
	res := callTool(registry, uuid.New(), "propose_create_purchase_draft", `{"items":[{"product_name":"A","qty":2},{"product_name":"B","qty":3}]}`)
	body := decodeJSON(t, res.Content)
	if body["item_count"].(float64) != 2 {
		t.Errorf("item_count=%v, want 2", body["item_count"])
	}

	res = callTool(registry, uuid.New(), "propose_create_purchase_draft", `{bad`)
	if decodeJSON(t, res.Content)["error"] == nil {
		t.Error("expected error for invalid args")
	}
}

func TestDispatch_ProposeBulkStockAdjust_CreatesPlanAndCapsSamples(t *testing.T) {
	registry := NewRegistry(&cxProductRepo{rows: manyProducts(12)}, &cxStockRepo{}, &cxSaleRepo{}, &cxExchangeRepo{})
	res := callTool(registry, uuid.New(), "propose_bulk_stock_adjust", `{"filter":"x","delta":-3.5}`)
	body := decodeJSON(t, res.Content)
	if body["affected_count"].(float64) != 12 {
		t.Errorf("affected_count=%v, want 12", body["affected_count"])
	}

	res = callTool(registry, uuid.New(), "propose_bulk_stock_adjust", `{bad`)
	if decodeJSON(t, res.Content)["error"] == nil {
		t.Error("expected error for invalid args")
	}

	registryErr := NewRegistry(&cxProductRepo{err: errors.New("x")}, &cxStockRepo{}, &cxSaleRepo{}, &cxExchangeRepo{})
	res = callTool(registryErr, uuid.New(), "propose_bulk_stock_adjust", `{"filter":"x","delta":1}`)
	if decodeJSON(t, res.Content)["error"] == nil {
		t.Error("expected error on repo failure")
	}
}

func TestDispatch_UnknownTool_ReturnsErrorJSON(t *testing.T) {
	registry := NewRegistry(&cxProductRepo{}, &cxStockRepo{}, &cxSaleRepo{}, &cxExchangeRepo{})
	res := callTool(registry, uuid.New(), "does_not_exist", `{}`)
	if res.Content != `{"error":"unknown tool"}` {
		t.Errorf("content=%q, want unknown-tool error JSON", res.Content)
	}
	if res.Plan != nil {
		t.Error("unknown tool must not produce a plan")
	}
}

// ============================================================================
// executor.go — additional edge cases
// ============================================================================

type cxDraftCreator struct {
	gotLines []DraftLine
	billID   uuid.UUID
	billNo   string
	err      error
}

func (f *cxDraftCreator) CreatePurchaseDraft(_ context.Context, _, _ uuid.UUID, lines []DraftLine) (uuid.UUID, string, error) {
	f.gotLines = lines
	return f.billID, f.billNo, f.err
}

type cxPriceChanger struct {
	affected  int
	err       error
	gotIDs    []uuid.UUID
	gotAction string
}

func (f *cxPriceChanger) ApplyPriceChange(_ context.Context, _ uuid.UUID, ids []uuid.UUID, action string) (int, error) {
	f.gotIDs = ids
	f.gotAction = action
	return f.affected, f.err
}

type cxStockAdjuster struct {
	affected int
	err      error
	calls    int
}

func (f *cxStockAdjuster) AdjustStockBatch(_ context.Context, _, _, _ uuid.UUID, lines []StockAdjustLine) (int, error) {
	f.calls++
	if f.err != nil {
		return 0, f.err
	}
	return f.affected, nil
}

type cxCapturer struct {
	called     bool
	calledWith []uuid.UUID
	entries    []PriceBeforeEntry
	err        error
}

func (f *cxCapturer) CaptureBeforePrices(_ context.Context, _ uuid.UUID, ids []uuid.UUID) ([]PriceBeforeEntry, error) {
	f.called = true
	f.calledWith = ids
	if f.err != nil {
		return nil, f.err
	}
	return f.entries, nil
}

type cxSnapStore struct {
	saved   map[string][]PriceBeforeEntry
	saveErr error
	getErr  error
}

func newCxSnapStore() *cxSnapStore { return &cxSnapStore{saved: map[string][]PriceBeforeEntry{}} }

func (f *cxSnapStore) SaveSnapshot(_ context.Context, _, planID uuid.UUID, entries []PriceBeforeEntry) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved[planID.String()] = entries
	return nil
}

func (f *cxSnapStore) GetSnapshot(_ context.Context, _, planID uuid.UUID) ([]PriceBeforeEntry, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	e, ok := f.saved[planID.String()]
	if !ok {
		return nil, nil
	}
	return e, nil
}

func TestExecutor_Execute_UnsupportedPlanType(t *testing.T) {
	ex := NewPlanExecutor(&cxProductRepo{}, &cxDraftCreator{}, &cxPriceChanger{}, &cxStockAdjuster{})
	plan := &domainai.Plan{ID: uuid.New(), TenantID: uuid.New(), Type: domainai.PlanType("bogus_type")}
	_, err := ex.Execute(context.Background(), uuid.New(), plan)
	if err == nil || !strings.Contains(err.Error(), "unsupported plan type") {
		t.Fatalf("err=%v, want 'unsupported plan type'", err)
	}
}

func TestExecutor_DecodePayload_MarshalAndUnmarshalErrors(t *testing.T) {
	ex := NewPlanExecutor(&cxProductRepo{}, &cxDraftCreator{}, &cxPriceChanger{}, &cxStockAdjuster{})

	// marshal error: payload contains an unmarshalable Go value.
	planMarshalErr := &domainai.Plan{
		ID: uuid.New(), TenantID: uuid.New(), Type: domainai.PlanTypeCreatePurchase,
		Payload: map[string]interface{}{"items": make(chan int)},
	}
	_, err := ex.Execute(context.Background(), uuid.New(), planMarshalErr)
	if err == nil || !strings.Contains(err.Error(), "decode purchase payload") || !strings.Contains(err.Error(), "marshal payload") {
		t.Fatalf("err=%v, want decode+marshal payload error", err)
	}

	// unmarshal error: filter field has wrong JSON type.
	planUnmarshalErr := &domainai.Plan{
		ID: uuid.New(), TenantID: uuid.New(), Type: domainai.PlanTypePriceChange,
		Payload: map[string]interface{}{"filter": 123, "action": "+5%"},
	}
	_, err = ex.Execute(context.Background(), uuid.New(), planUnmarshalErr)
	if err == nil || !strings.Contains(err.Error(), "decode price payload") {
		t.Fatalf("err=%v, want decode price payload error", err)
	}
}

func TestExecutor_ExecPurchase_EmptyItems_Errors(t *testing.T) {
	ex := NewPlanExecutor(&cxProductRepo{}, &cxDraftCreator{}, &cxPriceChanger{}, &cxStockAdjuster{})
	plan := &domainai.Plan{
		ID: uuid.New(), TenantID: uuid.New(), Type: domainai.PlanTypeCreatePurchase,
		Payload: map[string]interface{}{"items": []interface{}{}},
	}
	_, err := ex.Execute(context.Background(), uuid.New(), plan)
	if err == nil || !strings.Contains(err.Error(), "purchase plan has no items") {
		t.Fatalf("err=%v, want 'purchase plan has no items'", err)
	}
}

func TestExecutor_ResolveByName_SearchError(t *testing.T) {
	d := &cxDraftCreator{}
	ex := NewPlanExecutor(&cxProductRepo{err: errors.New("search down")}, d, &cxPriceChanger{}, &cxStockAdjuster{})
	plan := &domainai.Plan{
		ID: uuid.New(), TenantID: uuid.New(), Type: domainai.PlanTypeCreatePurchase,
		Payload: map[string]interface{}{"items": []map[string]interface{}{{"product_name": "X", "qty": 1.0}}},
	}
	_, err := ex.Execute(context.Background(), uuid.New(), plan)
	if err == nil || !strings.Contains(err.Error(), "resolve product") {
		t.Fatalf("err=%v, want 'resolve product' wrap", err)
	}
	if d.gotLines != nil {
		t.Error("draft creator must not be called when resolution errors")
	}
}

func TestExecutor_ResolveByFilter_SearchError_PriceAndStock(t *testing.T) {
	ex := NewPlanExecutor(&cxProductRepo{err: errors.New("search down")}, &cxDraftCreator{}, &cxPriceChanger{}, &cxStockAdjuster{})

	pricePlan := &domainai.Plan{ID: uuid.New(), TenantID: uuid.New(), Type: domainai.PlanTypePriceChange, Payload: map[string]interface{}{"filter": "x", "action": "+1%"}}
	_, err := ex.Execute(context.Background(), uuid.New(), pricePlan)
	if err == nil || !strings.Contains(err.Error(), "resolve filter") {
		t.Fatalf("price change err=%v, want 'resolve filter'", err)
	}

	stockPlan := &domainai.Plan{ID: uuid.New(), TenantID: uuid.New(), Type: domainai.PlanTypeBulkStockAdjust, Payload: map[string]interface{}{"filter": "x", "delta": 1.0}}
	_, err = ex.Execute(context.Background(), uuid.New(), stockPlan)
	if err == nil || !strings.Contains(err.Error(), "resolve filter") {
		t.Fatalf("stock adjust err=%v, want 'resolve filter'", err)
	}
}

func TestExecutor_WithPriceSnapshot_CapturesAndSaves(t *testing.T) {
	rows := []ProductRow{{ID: uuid.New(), Name: "A"}, {ID: uuid.New(), Name: "B"}}
	priceChanger := &cxPriceChanger{affected: 2}
	capturer := &cxCapturer{entries: []PriceBeforeEntry{{SKUID: uuid.New(), OldPrice: decimal.NewFromInt(100)}}}
	snap := newCxSnapStore()
	ex := NewPlanExecutor(&cxProductRepo{rows: rows}, &cxDraftCreator{}, priceChanger, &cxStockAdjuster{}).WithPriceSnapshot(capturer, snap)

	plan := &domainai.Plan{ID: uuid.New(), TenantID: uuid.New(), Type: domainai.PlanTypePriceChange, Payload: map[string]interface{}{"filter": "x", "action": "+10%"}}
	res, err := ex.Execute(context.Background(), uuid.New(), plan)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.AffectedCount != 2 {
		t.Errorf("affected=%d, want 2", res.AffectedCount)
	}
	if !capturer.called || len(capturer.calledWith) != 2 {
		t.Errorf("expected capturer called with 2 ids, got called=%v ids=%d", capturer.called, len(capturer.calledWith))
	}
	saved, ok := snap.saved[plan.ID.String()]
	if !ok || len(saved) != 1 || !saved[0].OldPrice.Equal(decimal.NewFromInt(100)) {
		t.Errorf("unexpected saved snapshot: %+v", saved)
	}
}

func TestExecutor_WithPriceSnapshot_SkippedWhenNoIDsMatch(t *testing.T) {
	capturer := &cxCapturer{}
	snap := newCxSnapStore()
	ex := NewPlanExecutor(&cxProductRepo{rows: nil}, &cxDraftCreator{}, &cxPriceChanger{affected: 0}, &cxStockAdjuster{}).WithPriceSnapshot(capturer, snap)

	plan := &domainai.Plan{ID: uuid.New(), TenantID: uuid.New(), Type: domainai.PlanTypePriceChange, Payload: map[string]interface{}{"filter": "nomatch", "action": "+10%"}}
	_, err := ex.Execute(context.Background(), uuid.New(), plan)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if capturer.called {
		t.Error("capturer must not be called when zero products match")
	}
}

func TestExecutor_WithPriceSnapshot_CaptureErrIsBestEffort(t *testing.T) {
	rows := []ProductRow{{ID: uuid.New(), Name: "A"}}
	capturer := &cxCapturer{err: errors.New("capture down")}
	snap := newCxSnapStore()
	priceChanger := &cxPriceChanger{affected: 1}
	ex := NewPlanExecutor(&cxProductRepo{rows: rows}, &cxDraftCreator{}, priceChanger, &cxStockAdjuster{}).WithPriceSnapshot(capturer, snap)

	plan := &domainai.Plan{ID: uuid.New(), TenantID: uuid.New(), Type: domainai.PlanTypePriceChange, Payload: map[string]interface{}{"filter": "x", "action": "+10%"}}
	res, err := ex.Execute(context.Background(), uuid.New(), plan)
	if err != nil {
		t.Fatalf("Execute must succeed even when snapshot capture fails (best-effort): %v", err)
	}
	if res.AffectedCount != 1 {
		t.Errorf("affected=%d, want 1", res.AffectedCount)
	}
	if len(snap.saved) != 0 {
		t.Error("nothing should be saved when capture errors")
	}
}

// ============================================================================
// revert.go — additional edge cases
// ============================================================================

type cxPlanStore struct {
	mu                 sync.Mutex
	plans              map[string]*domainai.Plan
	getErr             error
	saveErr            error
	updateErr          error
	failUpdateFromCall int
	updateCalls        int
	listErr            error
}

func newCxPlanStore() *cxPlanStore { return &cxPlanStore{plans: map[string]*domainai.Plan{}} }

func (f *cxPlanStore) SavePlan(_ context.Context, plan *domainai.Plan) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.plans[plan.ID.String()] = plan
	return nil
}

func (f *cxPlanStore) GetPlan(_ context.Context, _, planID uuid.UUID) (*domainai.Plan, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.plans[planID.String()]
	if !ok {
		return nil, nil
	}
	return p, nil
}

func (f *cxPlanStore) UpdatePlan(_ context.Context, plan *domainai.Plan) error {
	f.mu.Lock()
	f.updateCalls++
	calls := f.updateCalls
	f.mu.Unlock()
	if f.failUpdateFromCall > 0 && calls >= f.failUpdateFromCall {
		if f.updateErr != nil {
			return f.updateErr
		}
		return errors.New("update failed")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.plans[plan.ID.String()] = plan
	return nil
}

func (f *cxPlanStore) ListByTenant(_ context.Context, tenantID uuid.UUID, statusFilter string) ([]*domainai.Plan, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*domainai.Plan
	for _, p := range f.plans {
		if p.TenantID != tenantID {
			continue
		}
		if statusFilter != "" && string(p.Status) != statusFilter {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

type cxStockReverter struct {
	affected int
	err      error
}

func (f *cxStockReverter) RevertStockAdjust(_ context.Context, _, _, _ uuid.UUID) (int, error) {
	return f.affected, f.err
}

type cxPriceReverter struct {
	affected int
	err      error
}

func (f *cxPriceReverter) RestorePrices(_ context.Context, _ uuid.UUID, _ []PriceBeforeEntry) (int, error) {
	return f.affected, f.err
}

func cxConfirmedPlan(planType domainai.PlanType) *domainai.Plan {
	return &domainai.Plan{
		ID: uuid.New(), TenantID: uuid.New(), Type: planType,
		Status: domainai.PlanStatusConfirmed, CreatedAt: time.Now(), ExpiresAt: time.Now().Add(30 * time.Minute),
	}
}

func TestRevertPlan_GetPlanError_Wrapped(t *testing.T) {
	store := &cxPlanStore{getErr: errors.New("db down")}
	r := NewReverter(store, &cxStockReverter{}, &cxPriceReverter{}, newCxSnapStore())
	_, err := r.RevertPlan(context.Background(), uuid.New(), uuid.New(), uuid.New())
	if err == nil || !strings.Contains(err.Error(), "revert plan: get") {
		t.Fatalf("err=%v, want 'revert plan: get' wrap", err)
	}
}

// TestRevertPlan_StatusMatrix_NonConfirmedNonCancelled covers the business
// invariant table: Pending/Expired/Failed x {bulk_stock_adjust, price_change}
// all fall into the generic "expected confirmed" branch (not the
// ErrAlreadyReverted / ErrRevertWindowClosed / ErrPlanNotRevertible sentinels).
func TestRevertPlan_StatusMatrix_NonConfirmedNonCancelled(t *testing.T) {
	statuses := []domainai.PlanStatus{domainai.PlanStatusPending, domainai.PlanStatusExpired, domainai.PlanStatusFailed}
	types := []domainai.PlanType{domainai.PlanTypeBulkStockAdjust, domainai.PlanTypePriceChange}
	for _, st := range statuses {
		for _, ty := range types {
			t.Run(string(st)+"_"+string(ty), func(t *testing.T) {
				store := newCxPlanStore()
				plan := cxConfirmedPlan(ty)
				plan.Status = st
				_ = store.SavePlan(context.Background(), plan)
				r := NewReverter(store, &cxStockReverter{}, &cxPriceReverter{}, newCxSnapStore()).WithUndoTTL(30 * time.Second)
				_, err := r.RevertPlan(context.Background(), plan.TenantID, uuid.New(), plan.ID)
				if err == nil || !strings.Contains(err.Error(), "expected confirmed") {
					t.Fatalf("status=%s type=%s: err=%v, want 'expected confirmed'", st, ty, err)
				}
				if errors.Is(err, ErrAlreadyReverted) || errors.Is(err, ErrRevertWindowClosed) || errors.Is(err, ErrPlanNotRevertible) {
					t.Errorf("status=%s: must not match any sentinel, got %v", st, err)
				}
			})
		}
	}
}

func TestRevertPlan_LockFlipUpdateError_Wrapped(t *testing.T) {
	store := newCxPlanStore()
	plan := cxConfirmedPlan(domainai.PlanTypeBulkStockAdjust)
	_ = store.SavePlan(context.Background(), plan)
	store.failUpdateFromCall = 1

	r := NewReverter(store, &cxStockReverter{}, &cxPriceReverter{}, newCxSnapStore()).WithUndoTTL(30 * time.Second)
	_, err := r.RevertPlan(context.Background(), plan.TenantID, uuid.New(), plan.ID)
	if err == nil || !strings.Contains(err.Error(), "lock status flip") {
		t.Fatalf("err=%v, want 'lock status flip' wrap", err)
	}
}

func TestRevertPlan_StockReverterError_RollsBackToConfirmed(t *testing.T) {
	store := newCxPlanStore()
	plan := cxConfirmedPlan(domainai.PlanTypeBulkStockAdjust)
	_ = store.SavePlan(context.Background(), plan)

	r := NewReverter(store, &cxStockReverter{err: errors.New("reverse failed")}, &cxPriceReverter{}, newCxSnapStore()).WithUndoTTL(30 * time.Second)
	_, err := r.RevertPlan(context.Background(), plan.TenantID, uuid.New(), plan.ID)
	if err == nil || !strings.Contains(err.Error(), "reverse stock movements") {
		t.Fatalf("err=%v, want 'reverse stock movements' wrap", err)
	}
	persisted, _ := store.GetPlan(context.Background(), plan.TenantID, plan.ID)
	if persisted.Status != domainai.PlanStatusConfirmed {
		t.Errorf("status=%s after failed revert, want Confirmed (rollback of the lock)", persisted.Status)
	}
}

func TestRevertPlan_PriceSnapshotGetError_RollsBackToConfirmed(t *testing.T) {
	store := newCxPlanStore()
	plan := cxConfirmedPlan(domainai.PlanTypePriceChange)
	_ = store.SavePlan(context.Background(), plan)

	snap := newCxSnapStore()
	snap.getErr = errors.New("snapshot store down")
	r := NewReverter(store, &cxStockReverter{}, &cxPriceReverter{}, snap).WithUndoTTL(30 * time.Second)
	_, err := r.RevertPlan(context.Background(), plan.TenantID, uuid.New(), plan.ID)
	if err == nil || !strings.Contains(err.Error(), "get price snapshot") {
		t.Fatalf("err=%v, want 'get price snapshot' wrap", err)
	}
	persisted, _ := store.GetPlan(context.Background(), plan.TenantID, plan.ID)
	if persisted.Status != domainai.PlanStatusConfirmed {
		t.Errorf("status=%s after failed revert, want Confirmed (rollback of the lock)", persisted.Status)
	}
}

func TestRevertPlan_PriceRestoreError_RollsBackToConfirmed(t *testing.T) {
	store := newCxPlanStore()
	plan := cxConfirmedPlan(domainai.PlanTypePriceChange)
	_ = store.SavePlan(context.Background(), plan)

	snap := newCxSnapStore()
	_ = snap.SaveSnapshot(context.Background(), plan.TenantID, plan.ID, []PriceBeforeEntry{{SKUID: uuid.New(), OldPrice: decimal.NewFromInt(50)}})
	r := NewReverter(store, &cxStockReverter{}, &cxPriceReverter{err: errors.New("restore failed")}, snap).WithUndoTTL(30 * time.Second)
	_, err := r.RevertPlan(context.Background(), plan.TenantID, uuid.New(), plan.ID)
	if err == nil || !strings.Contains(err.Error(), "restore prices") {
		t.Fatalf("err=%v, want 'restore prices' wrap", err)
	}
	persisted, _ := store.GetPlan(context.Background(), plan.TenantID, plan.ID)
	if persisted.Status != domainai.PlanStatusConfirmed {
		t.Errorf("status=%s after failed revert, want Confirmed (rollback of the lock)", persisted.Status)
	}
}

// ============================================================================
// orchestrator.go — ConfirmPlan / CancelPlan / ListPlans edge cases
// ============================================================================

type cxExecutor struct {
	err    error
	result *ExecutionResult
	calls  int32
}

func (e *cxExecutor) Execute(_ context.Context, _ uuid.UUID, plan *domainai.Plan) (*ExecutionResult, error) {
	atomic.AddInt32(&e.calls, 1)
	if e.err != nil {
		return nil, e.err
	}
	if e.result != nil {
		return e.result, nil
	}
	return &ExecutionResult{Type: plan.Type, AffectedCount: 1}, nil
}

func cxPendingPlan(tenantID, planID uuid.UUID, typ domainai.PlanType) *domainai.Plan {
	return &domainai.Plan{
		ID: planID, TenantID: tenantID, Type: typ, Status: domainai.PlanStatusPending,
		CreatedAt: time.Now(), ExpiresAt: time.Now().Add(10 * time.Minute),
	}
}

func TestConfirmPlan_GetPlanError_Wrapped(t *testing.T) {
	store := &cxPlanStore{getErr: errors.New("db down")}
	o := NewOrchestrator(nil, nil, store, "")
	_, _, err := o.ConfirmPlan(context.Background(), uuid.New(), uuid.New(), uuid.New())
	if err == nil || !strings.Contains(err.Error(), "confirm plan: get") {
		t.Fatalf("err=%v, want 'confirm plan: get' wrap", err)
	}
}

func TestConfirmPlan_LockFlipUpdateError_Wrapped(t *testing.T) {
	store := newCxPlanStore()
	tenantID, planID := uuid.New(), uuid.New()
	_ = store.SavePlan(context.Background(), cxPendingPlan(tenantID, planID, domainai.PlanTypePriceChange))
	store.failUpdateFromCall = 1

	o := NewOrchestrator(nil, nil, store, "")
	_, _, err := o.ConfirmPlan(context.Background(), tenantID, uuid.New(), planID)
	if err == nil || !strings.Contains(err.Error(), "confirm plan: update") {
		t.Fatalf("err=%v, want 'confirm plan: update' wrap", err)
	}
}

func TestConfirmPlan_ExecFailAndMarkFailedAlsoFails_DoubleWrapped(t *testing.T) {
	store := newCxPlanStore()
	tenantID, actorID, planID := uuid.New(), uuid.New(), uuid.New()
	_ = store.SavePlan(context.Background(), cxPendingPlan(tenantID, planID, domainai.PlanTypeCreatePurchase))
	// call#1 = flip to Confirmed (succeeds), call#2 = mark Failed (fails).
	store.failUpdateFromCall = 2

	ex := &cxExecutor{err: errors.New("exec boom")}
	o := NewOrchestrator(nil, nil, store, "").WithExecutor(ex)
	_, _, err := o.ConfirmPlan(context.Background(), tenantID, actorID, planID)
	if err == nil || !strings.Contains(err.Error(), "execute failed") || !strings.Contains(err.Error(), "mark-failed") {
		t.Fatalf("err=%v, want double-wrapped execute-failed + mark-failed", err)
	}
}

func TestCancelPlan_AllBranches(t *testing.T) {
	t.Run("get error wrapped", func(t *testing.T) {
		store := &cxPlanStore{getErr: errors.New("db down")}
		o := NewOrchestrator(nil, nil, store, "")
		err := o.CancelPlan(context.Background(), uuid.New(), uuid.New())
		if err == nil || !strings.Contains(err.Error(), "cancel plan: get") {
			t.Fatalf("err=%v, want 'cancel plan: get' wrap", err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		store := newCxPlanStore()
		o := NewOrchestrator(nil, nil, store, "")
		err := o.CancelPlan(context.Background(), uuid.New(), uuid.New())
		if !errors.Is(err, ErrPlanNotFound) {
			t.Fatalf("err=%v, want ErrPlanNotFound", err)
		}
	})

	t.Run("already resolved is idempotent no-op", func(t *testing.T) {
		store := newCxPlanStore()
		tenantID, planID := uuid.New(), uuid.New()
		plan := cxPendingPlan(tenantID, planID, domainai.PlanTypePriceChange)
		plan.Status = domainai.PlanStatusConfirmed
		_ = store.SavePlan(context.Background(), plan)

		o := NewOrchestrator(nil, nil, store, "")
		err := o.CancelPlan(context.Background(), tenantID, planID)
		if err != nil {
			t.Fatalf("expected nil (idempotent), got %v", err)
		}
		if store.updateCalls != 0 {
			t.Errorf("UpdatePlan must not be called for an already-resolved plan, got %d calls", store.updateCalls)
		}
	})

	t.Run("pending is cancelled", func(t *testing.T) {
		store := newCxPlanStore()
		tenantID, planID := uuid.New(), uuid.New()
		_ = store.SavePlan(context.Background(), cxPendingPlan(tenantID, planID, domainai.PlanTypePriceChange))

		o := NewOrchestrator(nil, nil, store, "")
		if err := o.CancelPlan(context.Background(), tenantID, planID); err != nil {
			t.Fatalf("CancelPlan: %v", err)
		}
		persisted, _ := store.GetPlan(context.Background(), tenantID, planID)
		if persisted.Status != domainai.PlanStatusCancelled {
			t.Errorf("status=%s, want Cancelled", persisted.Status)
		}
	})

	t.Run("update error propagated directly", func(t *testing.T) {
		store := newCxPlanStore()
		tenantID, planID := uuid.New(), uuid.New()
		_ = store.SavePlan(context.Background(), cxPendingPlan(tenantID, planID, domainai.PlanTypePriceChange))
		wantErr := errors.New("update boom")
		store.failUpdateFromCall = 1
		store.updateErr = wantErr

		o := NewOrchestrator(nil, nil, store, "")
		err := o.CancelPlan(context.Background(), tenantID, planID)
		if !errors.Is(err, wantErr) {
			t.Fatalf("err=%v, want %v (unwrapped passthrough)", err, wantErr)
		}
	})
}

func TestListPlans_ErrorAndHappyFiltering(t *testing.T) {
	t.Run("error wrapped", func(t *testing.T) {
		store := &cxPlanStore{listErr: errors.New("list boom")}
		o := NewOrchestrator(nil, nil, store, "")
		_, err := o.ListPlans(context.Background(), uuid.New(), "")
		if err == nil || !strings.Contains(err.Error(), "list plans:") {
			t.Fatalf("err=%v, want 'list plans:' wrap", err)
		}
	})

	t.Run("happy filtering", func(t *testing.T) {
		store := newCxPlanStore()
		tenantID := uuid.New()
		p1 := cxPendingPlan(tenantID, uuid.New(), domainai.PlanTypePriceChange)
		p2 := cxConfirmedPlan(domainai.PlanTypeBulkStockAdjust)
		p2.TenantID = tenantID
		_ = store.SavePlan(context.Background(), p1)
		_ = store.SavePlan(context.Background(), p2)

		o := NewOrchestrator(nil, nil, store, "")
		all, err := o.ListPlans(context.Background(), tenantID, "")
		if err != nil || len(all) != 2 {
			t.Fatalf("all=%d err=%v, want 2 plans", len(all), err)
		}
		pending, err := o.ListPlans(context.Background(), tenantID, string(domainai.PlanStatusPending))
		if err != nil || len(pending) != 1 {
			t.Fatalf("pending=%d err=%v, want 1", len(pending), err)
		}
	})
}

// ============================================================================
// orchestrator.go — Chat / StreamChat via a real *llmclient.Client pointed at
// an httptest server (Client has unexported fields so it cannot be faked
// directly; we exercise the real HTTP path end-to-end instead).
// ============================================================================

func newCxLLMClient(t *testing.T, srv *httptest.Server) *llmclient.Client {
	t.Helper()
	cli, err := llmclient.New(llmclient.Config{BaseURL: srv.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("llmclient.New: %v", err)
	}
	return cli
}

func chatRespJSON(t *testing.T, msg llmclient.Message) []byte {
	t.Helper()
	b, err := json.Marshal(llmclient.ChatResponse{Choices: []llmclient.Choice{{Message: msg}}})
	if err != nil {
		t.Fatalf("marshal chat response: %v", err)
	}
	return b
}

type cxMemClient struct {
	searchResult []memorusclient.Memory
	searchErr    error
	addCh        chan struct{}
	addErr       error
}

func (m *cxMemClient) Search(_ context.Context, _, _ string, _ int) ([]memorusclient.Memory, error) {
	return m.searchResult, m.searchErr
}
func (m *cxMemClient) Add(_ context.Context, _, content string, _ map[string]any) (*memorusclient.Memory, error) {
	if m.addCh != nil {
		defer func() { m.addCh <- struct{}{} }()
	}
	if m.addErr != nil {
		return nil, m.addErr
	}
	return &memorusclient.Memory{Content: content}, nil
}

func TestChat_HappyPath_ToolRoundThenFinalAnswer_WithMemory(t *testing.T) {
	var mu sync.Mutex
	var captured []llmclient.ChatRequest
	var nonStream int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req llmclient.ChatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		mu.Lock()
		captured = append(captured, req)
		mu.Unlock()

		n := atomic.AddInt32(&nonStream, 1)
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			w.Write(chatRespJSON(t, llmclient.Message{ToolCalls: []llmclient.ToolCall{{
				ID: "tc1", Type: "function",
				Function: llmclient.ToolCallFunction{Name: "propose_bulk_stock_adjust", Arguments: `{"filter":"x","delta":5}`},
			}}}))
			return
		}
		w.Write(chatRespJSON(t, llmclient.Message{Content: "Done, adjustment proposed."}))
	}))
	defer srv.Close()

	store := newCxPlanStore()
	mc := &cxMemClient{searchResult: []memorusclient.Memory{{Content: "past ctx"}}, addCh: make(chan struct{}, 1)}
	registry := NewRegistry(&cxProductRepo{rows: manyProducts(2)}, &cxStockRepo{}, &cxSaleRepo{}, &cxExchangeRepo{})
	o := NewOrchestrator(newCxLLMClient(t, srv), registry, store, "test-model").WithMemory(mc)

	out, err := o.Chat(context.Background(), ChatInput{TenantID: uuid.New(), UserMessage: "adjust stock"})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if out.AssistantText != "Done, adjustment proposed." {
		t.Errorf("AssistantText=%q", out.AssistantText)
	}
	if len(out.Plans) != 1 || out.Plans[0].Type != domainai.PlanTypeBulkStockAdjust {
		t.Fatalf("Plans=%+v, want 1 bulk_stock_adjust plan", out.Plans)
	}
	if len(out.ToolCalls) != 1 || out.ToolCalls[0].ToolName != "propose_bulk_stock_adjust" {
		t.Errorf("ToolCalls=%+v", out.ToolCalls)
	}
	persisted, _ := store.GetPlan(context.Background(), out.Plans[0].TenantID, out.Plans[0].ID)
	if persisted == nil {
		t.Error("plan was not persisted to the store")
	}

	mu.Lock()
	firstReq := captured[0]
	mu.Unlock()
	lastMsg := firstReq.Messages[len(firstReq.Messages)-1]
	content, _ := lastMsg.Content.(string)
	if !strings.Contains(content, "历史记忆") {
		t.Errorf("expected memory-augmented user message reaching the LLM, got %q", content)
	}

	select {
	case <-mc.addCh:
	case <-time.After(2 * time.Second):
		t.Fatal("AsyncWriteMemory (Add) was not called after the final answer")
	}
}

func TestChat_LLMChatError_Wrapped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(llmclient.ChatResponse{Error: &llmclient.APIErr{Code: "invalid_request", Message: "bad"}})
	}))
	defer srv.Close()

	registry := NewRegistry(&cxProductRepo{}, &cxStockRepo{}, &cxSaleRepo{}, &cxExchangeRepo{})
	o := NewOrchestrator(newCxLLMClient(t, srv), registry, newCxPlanStore(), "m")
	_, err := o.Chat(context.Background(), ChatInput{TenantID: uuid.New(), UserMessage: "hi"})
	if err == nil || !strings.Contains(err.Error(), "orchestrator: llm chat") {
		t.Fatalf("err=%v, want 'orchestrator: llm chat' wrap", err)
	}
}

func TestChat_NoChoices_Errors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(llmclient.ChatResponse{Choices: []llmclient.Choice{}})
	}))
	defer srv.Close()

	registry := NewRegistry(&cxProductRepo{}, &cxStockRepo{}, &cxSaleRepo{}, &cxExchangeRepo{})
	o := NewOrchestrator(newCxLLMClient(t, srv), registry, newCxPlanStore(), "m")
	_, err := o.Chat(context.Background(), ChatInput{TenantID: uuid.New(), UserMessage: "hi"})
	if err == nil || !strings.Contains(err.Error(), "no choices in response") {
		t.Fatalf("err=%v, want 'no choices in response'", err)
	}
}

func TestChat_ExceedsMaxToolRounds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(chatRespJSON(t, llmclient.Message{ToolCalls: []llmclient.ToolCall{{
			ID: "tc", Type: "function", Function: llmclient.ToolCallFunction{Name: "get_stock_summary", Arguments: `{}`},
		}}}))
	}))
	defer srv.Close()

	registry := NewRegistry(&cxProductRepo{}, &cxStockRepo{}, &cxSaleRepo{}, &cxExchangeRepo{})
	o := NewOrchestrator(newCxLLMClient(t, srv), registry, newCxPlanStore(), "m")
	_, err := o.Chat(context.Background(), ChatInput{TenantID: uuid.New(), UserMessage: "hi"})
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("exceeded %d tool rounds", maxToolRounds)) {
		t.Fatalf("err=%v, want exceeded-tool-rounds error", err)
	}
}

func TestChat_SavePlanError_IsNonFatal(t *testing.T) {
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if atomic.AddInt32(&n, 1) == 1 {
			w.Write(chatRespJSON(t, llmclient.Message{ToolCalls: []llmclient.ToolCall{{
				ID: "tc1", Type: "function",
				Function: llmclient.ToolCallFunction{Name: "propose_price_change", Arguments: `{"filter":"x","action":"+5%"}`},
			}}}))
			return
		}
		w.Write(chatRespJSON(t, llmclient.Message{Content: "ok"}))
	}))
	defer srv.Close()

	store := &cxPlanStore{plans: map[string]*domainai.Plan{}, saveErr: errors.New("save down")}
	registry := NewRegistry(&cxProductRepo{rows: manyProducts(2)}, &cxStockRepo{}, &cxSaleRepo{}, &cxExchangeRepo{})
	o := NewOrchestrator(newCxLLMClient(t, srv), registry, store, "m")

	out, err := o.Chat(context.Background(), ChatInput{TenantID: uuid.New(), UserMessage: "hi"})
	if err != nil {
		t.Fatalf("Chat must not fail when SavePlan errors (non-fatal): %v", err)
	}
	if len(out.Plans) != 0 {
		t.Errorf("Plans=%+v, want none (save failed)", out.Plans)
	}
	if len(out.ToolCalls) != 1 || out.ToolCalls[0].Error == nil {
		t.Errorf("expected ToolCalls[0].Error set, got %+v", out.ToolCalls)
	}
}

func TestChat_ExtractContent_NonStringAndNilBranches(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(chatRespJSON(t, llmclient.Message{Content: []interface{}{"part1", "part2"}}))
	}))
	defer srv.Close()

	registry := NewRegistry(&cxProductRepo{}, &cxStockRepo{}, &cxSaleRepo{}, &cxExchangeRepo{})
	o := NewOrchestrator(newCxLLMClient(t, srv), registry, newCxPlanStore(), "m")
	out, err := o.Chat(context.Background(), ChatInput{TenantID: uuid.New(), UserMessage: "hi"})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if out.AssistantText == "" {
		t.Error("expected non-empty AssistantText from the default fmt.Sprintf formatting branch")
	}
}

func TestChat_WithTracer_RecordsSpansAndPropagatesTraceID(t *testing.T) {
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if atomic.AddInt32(&n, 1) == 1 {
			w.Write(chatRespJSON(t, llmclient.Message{ToolCalls: []llmclient.ToolCall{{
				ID: "tc1", Type: "function",
				Function: llmclient.ToolCallFunction{Name: "propose_price_change", Arguments: `{"filter":"x","action":"+5%"}`},
			}}}))
			return
		}
		w.Write(chatRespJSON(t, llmclient.Message{Content: "ok"}))
	}))
	defer srv.Close()

	tracer := &cxTracer{traceID: "trace-abc123"}
	store := newCxPlanStore()
	registry := NewRegistry(&cxProductRepo{rows: manyProducts(1)}, &cxStockRepo{}, &cxSaleRepo{}, &cxExchangeRepo{})
	o := NewOrchestrator(newCxLLMClient(t, srv), registry, store, "m").WithTracer(tracer)

	out, err := o.Chat(context.Background(), ChatInput{TenantID: uuid.New(), UserMessage: "hi"})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if len(tracer.spans) != 2 {
		t.Fatalf("spans=%d, want 2 (one per tool round)", len(tracer.spans))
	}
	if len(tracer.spans[0].toolCalls) != 1 || tracer.spans[0].toolCalls[0].name != "propose_price_change" {
		t.Errorf("span0 toolCalls=%+v", tracer.spans[0].toolCalls)
	}
	if !tracer.spans[1].ended || tracer.spans[1].endOutput != "ok" {
		t.Errorf("span1=%+v, want ended with output 'ok'", tracer.spans[1])
	}
	if len(out.Plans) != 1 || out.Plans[0].TraceID != "trace-abc123" {
		t.Fatalf("Plans=%+v, want TraceID propagated from the span", out.Plans)
	}
}

type cxSpan struct {
	ended     bool
	endOutput string
	endErr    error
	toolCalls []struct{ name, args, result string }
	traceID   string
}

func (s *cxSpan) End(output string, _ llmobs.TokenCount, err error) {
	s.ended = true
	s.endOutput = output
	s.endErr = err
}
func (s *cxSpan) AttachToolCall(name, argsJSON, resultJSON string) {
	s.toolCalls = append(s.toolCalls, struct{ name, args, result string }{name, argsJSON, resultJSON})
}
func (s *cxSpan) TraceID() string { return s.traceID }

type cxTracer struct {
	spans   []*cxSpan
	traceID string
}

func (tr *cxTracer) StartLLMSpan(ctx context.Context, _, _, _ string) (llmobs.Span, context.Context) {
	s := &cxSpan{traceID: tr.traceID}
	tr.spans = append(tr.spans, s)
	return s, ctx
}

// --- StreamChat ---

func TestStreamChat_HappyPath(t *testing.T) {
	// After a tool round, the model's follow-up Chat response (no tool calls)
	// IS the final answer and is emitted directly via onChunk. A second
	// streaming inference must NOT be issued — re-inferring the same prompt was
	// the P1 double-billing defect. streamCalls guards against its return.
	var nonStream, streamCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req llmclient.ChatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Stream {
			atomic.AddInt32(&streamCalls, 1)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		n := atomic.AddInt32(&nonStream, 1)
		if n == 1 {
			w.Write(chatRespJSON(t, llmclient.Message{ToolCalls: []llmclient.ToolCall{{
				ID: "tc1", Type: "function", Function: llmclient.ToolCallFunction{Name: "get_stock_summary", Arguments: `{}`},
			}}}))
			return
		}
		w.Write(chatRespJSON(t, llmclient.Message{Content: "final-answer"}))
	}))
	defer srv.Close()

	registry := NewRegistry(&cxProductRepo{}, &cxStockRepo{}, &cxSaleRepo{}, &cxExchangeRepo{})
	o := NewOrchestrator(newCxLLMClient(t, srv), registry, newCxPlanStore(), "m")

	var chunks []string
	out, err := o.StreamChat(context.Background(), ChatInput{TenantID: uuid.New(), UserMessage: "hi"}, func(s string) {
		chunks = append(chunks, s)
	})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	if out.AssistantText != "final-answer" {
		t.Errorf("AssistantText=%q, want 'final-answer'", out.AssistantText)
	}
	if len(chunks) != 1 {
		t.Errorf("onChunk called %d times, want 1 (single-inference direct emit)", len(chunks))
	}
	if atomic.LoadInt32(&streamCalls) != 0 {
		t.Errorf("second streaming inference issued %d time(s) — double-billing regression", streamCalls)
	}
	if len(out.ToolCalls) != 1 || out.ToolCalls[0].ToolName != "get_stock_summary" {
		t.Errorf("ToolCalls=%+v", out.ToolCalls)
	}
}

func TestStreamChat_PreToolChatError_Wrapped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(llmclient.ChatResponse{Error: &llmclient.APIErr{Code: "invalid_request", Message: "bad"}})
	}))
	defer srv.Close()

	registry := NewRegistry(&cxProductRepo{}, &cxStockRepo{}, &cxSaleRepo{}, &cxExchangeRepo{})
	o := NewOrchestrator(newCxLLMClient(t, srv), registry, newCxPlanStore(), "m")
	_, err := o.StreamChat(context.Background(), ChatInput{TenantID: uuid.New(), UserMessage: "hi"}, func(string) {})
	if err == nil || !strings.Contains(err.Error(), "stream pre-tool chat") {
		t.Fatalf("err=%v, want 'stream pre-tool chat' wrap", err)
	}
}

func TestStreamChat_NoChoices_Errors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(llmclient.ChatResponse{Choices: []llmclient.Choice{}})
	}))
	defer srv.Close()

	registry := NewRegistry(&cxProductRepo{}, &cxStockRepo{}, &cxSaleRepo{}, &cxExchangeRepo{})
	o := NewOrchestrator(newCxLLMClient(t, srv), registry, newCxPlanStore(), "m")
	_, err := o.StreamChat(context.Background(), ChatInput{TenantID: uuid.New(), UserMessage: "hi"}, func(string) {})
	if err == nil || !strings.Contains(err.Error(), "no choices") {
		t.Fatalf("err=%v, want 'no choices'", err)
	}
}

func TestStreamChat_NoTool_EmitsChatContentWithoutSecondInference(t *testing.T) {
	// A question the model answers without any tool: the first Chat response is
	// the final answer and is emitted directly. The former code re-requested
	// with stream=true (a second inference → the P1 double-billing defect);
	// that stream step is gone, so streamCalls must stay 0 and no error occurs.
	var streamCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req llmclient.ChatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Stream {
			atomic.AddInt32(&streamCalls, 1)
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"code":"server_error","message":"boom"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(chatRespJSON(t, llmclient.Message{Content: "final"}))
	}))
	defer srv.Close()

	registry := NewRegistry(&cxProductRepo{}, &cxStockRepo{}, &cxSaleRepo{}, &cxExchangeRepo{})
	o := NewOrchestrator(newCxLLMClient(t, srv), registry, newCxPlanStore(), "m")
	var chunks []string
	out, err := o.StreamChat(context.Background(), ChatInput{TenantID: uuid.New(), UserMessage: "hi"}, func(s string) { chunks = append(chunks, s) })
	if err != nil {
		t.Fatalf("StreamChat: unexpected err=%v", err)
	}
	if out.AssistantText != "final" {
		t.Errorf("AssistantText=%q, want 'final'", out.AssistantText)
	}
	if len(chunks) != 1 || chunks[0] != "final" {
		t.Errorf("chunks=%v, want single 'final'", chunks)
	}
	if atomic.LoadInt32(&streamCalls) != 0 {
		t.Errorf("second streaming inference issued %d time(s) — double-billing regression", streamCalls)
	}
}

func TestStreamChat_ExceedsMaxToolRounds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(chatRespJSON(t, llmclient.Message{ToolCalls: []llmclient.ToolCall{{
			ID: "tc", Type: "function", Function: llmclient.ToolCallFunction{Name: "get_stock_summary", Arguments: `{}`},
		}}}))
	}))
	defer srv.Close()

	registry := NewRegistry(&cxProductRepo{}, &cxStockRepo{}, &cxSaleRepo{}, &cxExchangeRepo{})
	o := NewOrchestrator(newCxLLMClient(t, srv), registry, newCxPlanStore(), "m")
	_, err := o.StreamChat(context.Background(), ChatInput{TenantID: uuid.New(), UserMessage: "hi"}, func(string) {})
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("exceeded %d tool rounds", maxToolRounds)) {
		t.Fatalf("err=%v, want exceeded-tool-rounds error", err)
	}
}

// ============================================================================
// memory.go — remaining branches
// ============================================================================

type cxSearchOnlyMemClient struct {
	result []memorusclient.Memory
	err    error
}

func (m *cxSearchOnlyMemClient) Search(_ context.Context, _, _ string, _ int) ([]memorusclient.Memory, error) {
	return m.result, m.err
}
func (m *cxSearchOnlyMemClient) Add(_ context.Context, _, _ string, _ map[string]any) (*memorusclient.Memory, error) {
	return nil, nil
}

func TestAugmentMessagesWithMemoryOrFallback_NilClient(t *testing.T) {
	got := AugmentMessagesWithMemoryOrFallback(nil, context.Background(), "u", "original")
	if got != "original" {
		t.Errorf("got %q, want unchanged original message", got)
	}
}

func TestAugmentMessagesWithMemoryOrFallback_EmptyMemories_ReturnsOriginal(t *testing.T) {
	mc := &cxSearchOnlyMemClient{result: nil, err: nil}
	got := AugmentMessagesWithMemoryOrFallback(mc, context.Background(), "u", "original")
	if got != "original" {
		t.Errorf("got %q, want unchanged original message", got)
	}
}

func TestAugmentMessagesWithMemoryOrFallback_HappyPath_PrependsContext(t *testing.T) {
	mc := &cxSearchOnlyMemClient{result: []memorusclient.Memory{{Content: "remembered fact"}}}
	got := AugmentMessagesWithMemoryOrFallback(mc, context.Background(), "u", "original")
	if !strings.Contains(got, "历史记忆") || !strings.Contains(got, "remembered fact") || !strings.Contains(got, "original") {
		t.Errorf("got %q, want augmented with memory + original message", got)
	}
}

func TestBuildMemorySummary_TruncatesOver100Chars(t *testing.T) {
	userMsg := strings.Repeat("a", 99) + "Z" + strings.Repeat("q", 50) // char 100 is 'Z', chars after are 'q's.
	summary := BuildMemorySummary(uuid.New(), userMsg, "reply")

	tenantIdx := strings.LastIndex(summary, " (tenant=")
	if tenantIdx < 0 {
		t.Fatalf("summary missing tenant suffix: %q", summary)
	}
	beforeTenant := summary[:tenantIdx] // "用户问了：<snippet>"

	if strings.Contains(beforeTenant, "q") {
		t.Errorf("snippet must truncate at 100 chars (no trailing 'q's), got %q", beforeTenant)
	}
	if !strings.HasSuffix(beforeTenant, "Z") {
		t.Errorf("snippet must end at the 100th char 'Z', got %q", beforeTenant)
	}
}

func TestAsyncWriteMemory_PanicIsRecovered(t *testing.T) {
	done := make(chan struct{})
	pm := &cxPanicMemClient{done: done}
	AsyncWriteMemory(pm, "u", "s", nil)

	select {
	case <-done:
		// panic occurred inside Add; AsyncWriteMemory's recover() must absorb it
		// without crashing the test process.
	case <-time.After(2 * time.Second):
		t.Fatal("Add was never called")
	}
	// give the deferred recover a moment to run before the test exits.
	time.Sleep(50 * time.Millisecond)
}

type cxPanicMemClient struct {
	done chan struct{}
}

func (p *cxPanicMemClient) Search(_ context.Context, _, _ string, _ int) ([]memorusclient.Memory, error) {
	return nil, nil
}
func (p *cxPanicMemClient) Add(_ context.Context, _, _ string, _ map[string]any) (*memorusclient.Memory, error) {
	close(p.done)
	panic("simulated memorus panic")
}
