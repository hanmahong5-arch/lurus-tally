package ai

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/llmclient"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/memorusclient"
)

func costRegistry() *Registry {
	stock := &cxStockRepo{rows: []StockRow{{
		ProductID: uuid.New(), ProductName: "螺丝", Qty: decimal.NewFromInt(10),
		UnitCost: decimal.RequireFromString("2.5"), LastMovedAt: time.Now().Add(-200 * 24 * time.Hour),
	}}}
	sale := &cxSaleRepo{rows: []SaleRow{{ProductID: uuid.New(), ProductName: "螺丝", Qty: decimal.NewFromInt(1), Revenue: decimal.NewFromInt(5), Margin: decimal.RequireFromString("0.4")}}}
	return NewRegistry(&cxProductRepo{}, stock, sale, &cxExchangeRepo{})
}

func callHidden(r *Registry, name, args string) string {
	return r.Dispatch(WithCostHidden(context.Background()), uuid.New(), llmclient.ToolCall{
		ID: "c", Type: "function", Function: llmclient.ToolCallFunction{Name: name, Arguments: args},
	}).Content
}

func TestCostHidden_CostToolsRefused(t *testing.T) {
	r := costRegistry()
	for name, args := range map[string]string{
		"gross_margin_summary":          `{"days":30}`,
		"propose_create_purchase_draft": `{}`,
		"recent_sales_top":              `{"metric":"margin","days":7}`,
	} {
		if got := callHidden(r, name, args); !strings.Contains(got, "没有查看成本和毛利的权限") {
			t.Errorf("%s not refused: %s", name, got)
		}
	}
}

func TestCostHidden_StockValueRemovedButCountsKept(t *testing.T) {
	r := costRegistry()
	got := callHidden(r, "get_stock_summary", `{}`)
	if strings.Contains(got, "total_value_cny") || !strings.Contains(got, "total_skus") {
		t.Errorf("stock summary: %s", got)
	}
	got = callHidden(r, "list_dead_stock", `{"days":90}`)
	if strings.Contains(got, "value_cny") || !strings.Contains(got, "螺丝") {
		t.Errorf("dead stock: %s", got)
	}
	// Owner still sees the value.
	full := r.Dispatch(context.Background(), uuid.New(), llmclient.ToolCall{ID: "c", Function: llmclient.ToolCallFunction{Name: "get_stock_summary", Arguments: `{}`}}).Content
	if !strings.Contains(full, "total_value_cny") {
		t.Errorf("owner lost the value: %s", full)
	}
}

func TestCostHidden_RevenueTopStillWorks(t *testing.T) {
	got := callHidden(costRegistry(), "recent_sales_top", `{"metric":"revenue","days":7}`)
	if strings.Contains(got, "权限") {
		t.Errorf("revenue ranking must stay available: %s", got)
	}
}

func TestToolDefsFor_HidesCostTools(t *testing.T) {
	o := &Orchestrator{}
	names := func(ctx context.Context) string {
		var b strings.Builder
		for _, d := range o.toolDefsFor(ctx) {
			b.WriteString(d.Function.Name + ",")
		}
		return b.String()
	}
	if !strings.Contains(names(context.Background()), "gross_margin_summary") {
		t.Fatal("owner should be offered the margin tool")
	}
	hidden := names(WithCostHidden(context.Background()))
	if strings.Contains(hidden, "gross_margin_summary") || strings.Contains(hidden, "propose_create_purchase_draft") {
		t.Fatalf("cost tools offered to a cost-hidden caller: %s", hidden)
	}
	if !strings.Contains(hidden, "search_products") {
		t.Fatalf("ordinary tools must remain: %s", hidden)
	}
}

type searchCountMemory struct{ searches int }

func (m *searchCountMemory) Search(context.Context, string, string, int) ([]memorusclient.Memory, error) {
	m.searches++
	return []memorusclient.Memory{{Content: "螺丝进价 2.5 元", Score: 0.9}}, nil
}

func (m *searchCountMemory) Add(context.Context, string, string, map[string]any) (*memorusclient.Memory, error) {
	return nil, nil
}

// Shop-wide memory can hold purchase prices the owner said earlier, so a
// cost-hidden caller gets no recall at all.
func TestRecallMemories_SkippedForCostHidden(t *testing.T) {
	mem := &searchCountMemory{}
	out, n := recallMemories(mem, WithCostHidden(context.Background()), "shop", "螺丝多少钱", nil)
	if mem.searches != 0 || n != 0 || out != "螺丝多少钱" {
		t.Fatalf("searches=%d n=%d out=%q", mem.searches, n, out)
	}
	if _, n := recallMemories(mem, context.Background(), "shop", "螺丝多少钱", nil); mem.searches != 1 || n == 0 {
		t.Fatalf("cost viewers keep recall: searches=%d n=%d", mem.searches, n)
	}
}
