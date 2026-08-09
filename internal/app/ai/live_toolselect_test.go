package ai

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/llmclient"
)

// live_toolselect_test.go is an env-gated reproduction harness for the
// "tool-first fails on the default model" defect (breakpoint B).
//
// It calls the REAL newapi gateway with the production systemPrompt +
// ToolDefs() and records which tool (if any) the model selects on the FIRST
// turn for a fixed set of Chinese questions whose correct tool is known a
// priori (from the systemPrompt Chinese→tool mapping table). Tool *selection*
// happens before any tool executes, so this oracle does not need a seeded DB —
// it isolates exactly the reported failure mode ("调错工具 / 零调用").
//
// Skipped unless TALLY_LLM_LIVE=1, so the normal hermetic suite never touches
// the network. Model / key / base / repeat come from env:
//
//	TALLY_LLM_LIVE=1 TALLY_LLM_KEY=sk-... TALLY_LLM_MODEL=deepseek-v4-flash \
//	  TALLY_LLM_REPEAT=3 go test ./internal/app/ai/ -run TestLive_ToolSelection -v -count=1
func TestLive_ToolSelection(t *testing.T) {
	if os.Getenv("TALLY_LLM_LIVE") != "1" {
		t.Skip("set TALLY_LLM_LIVE=1 (+ TALLY_LLM_KEY) to run the live gateway tool-selection oracle")
	}
	key := os.Getenv("TALLY_LLM_KEY")
	if key == "" {
		t.Fatal("TALLY_LLM_KEY required")
	}
	base := os.Getenv("TALLY_LLM_BASE")
	if base == "" {
		base = "https://newapi.lurus.cn/v1"
	}
	model := os.Getenv("TALLY_LLM_MODEL")
	if model == "" {
		model = "deepseek-v4-flash"
	}
	repeat := 1
	if v := os.Getenv("TALLY_LLM_REPEAT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			repeat = n
		}
	}

	cli, err := llmclient.New(llmclient.Config{
		BaseURL:      base,
		APIKey:       key,
		DefaultModel: model,
		HTTPTimeout:  90 * time.Second,
	})
	if err != nil {
		t.Fatalf("llmclient.New: %v", err)
	}

	// 3 categories × 2 phrasings; want = the a-priori-correct first tool.
	// TALLY_LLM_CASESET=literal uses the *literal* systemPrompt example words
	// (the phrasing the 2026-07-07 ledger claimed still failed).
	type qc struct{ q, want string }
	cases := []qc{
		{"帮我算算A仓需要补多少货", "list_low_stock"},
		{"现在有哪些商品缺货需要补货", "list_low_stock"},
		{"毛利最低的商品有哪些", "gross_margin_summary"},
		{"利润率最差的几个产品是什么", "gross_margin_summary"},
		{"上月哪些SKU滞销了", "list_dead_stock"},
		{"有哪些库存积压卖不动的货", "list_dead_stock"},
	}
	if os.Getenv("TALLY_LLM_CASESET") == "literal" {
		cases = []qc{
			{"该进多少货", "list_low_stock"},
			{"帮我看下缺货预警", "list_low_stock"},
			{"有哪些呆滞库存", "list_dead_stock"},
			{"哪些商品库存积压了", "list_dead_stock"},
			{"利润率最低的是哪些", "gross_margin_summary"},
			{"最近卖得最好的商品", "recent_sales_top"},
		}
	}

	tools := ToolDefs()
	totalPass, totalRuns := 0, 0
	t.Logf("=== LIVE tool-selection oracle: model=%s repeat=%d ===", model, repeat)
	for r := 0; r < repeat; r++ {
		pass := 0
		for _, c := range cases {
			msgs := buildMessages(ChatInput{TenantID: uuid.New(), UserMessage: c.q})
			resp, cerr := cli.Chat(context.Background(), model, msgs, tools)
			first, extra := "NONE", ""
			switch {
			case cerr != nil:
				first = "ERR"
				extra = cerr.Error()
			case len(resp.Choices) > 0 && len(resp.Choices[0].Message.ToolCalls) > 0:
				tcs := resp.Choices[0].Message.ToolCalls
				first = tcs[0].Function.Name
				if len(tcs) > 1 {
					all := make([]string, 0, len(tcs))
					for _, tc := range tcs {
						all = append(all, tc.Function.Name)
					}
					extra = fmt.Sprintf("all=%v", all)
				}
			}
			ok := first == c.want
			mark := "FAIL"
			if ok {
				mark = "OK"
				pass++
				totalPass++
			}
			totalRuns++
			t.Logf("[run%d] %-4s want=%-22s got=%-30s q=%q %s", r, mark, c.want, first, c.q, extra)
		}
		t.Logf("[run%d] first-turn tool selection: %d/%d", r, pass, len(cases))
	}
	t.Logf("=== TOTAL %d/%d correct first-turn tool selection (model=%s) ===", totalPass, totalRuns, model)
}
