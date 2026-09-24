// Command memscenario replays realistic AI-drawer usage through tally's own
// memory code path (BuildMemorySummary / AsyncWriteMemory /
// AugmentMessagesWithMemoryOrFallback + memorusclient) against a running
// memorus, and prints exactly what the memory layer hands the LLM on later
// turns — no LLM is called, so the result is deterministic and free.
//
//	memorus-server serve --port 18767 --api-key probekey --config <sqlite config>
//	MEMORUS_URL=http://127.0.0.1:18767/api/v1 go run ./cmd/memscenario            # dev scenario
//	SCENARIO=holdout MEMORUS_URL=... go run ./cmd/memscenario                      # frozen held-out
//
// Use a fresh memorus store per run. The last line is the score:
// correct probes, injected lines, lines unrelated to the question, and how
// many times a superseded (stale) value was injected.
//
// The dev scenario was looked at while the memory-layer rules were written;
// quote the held-out one. Both are small (5 facts each): they show whether
// the mechanism works, not production-scale precision.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/app/ai"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/memorusclient"
)

type probe struct {
	q     string
	want  string // must appear in the injected memory block
	stale string // must NOT rank above `want` (empty = no stale value)
}

func main() {
	if os.Getenv("CUSTOMERS") == "1" {
		runCustomers()
		return
	}
	base := os.Getenv("MEMORUS_URL")
	c, err := memorusclient.New(memorusclient.Config{BaseURL: base, APIKey: "probekey"})
	if err != nil || c == nil {
		panic(fmt.Sprint("client: ", err))
	}
	tenant := uuid.MustParse("7d3f2a10-5b6c-4e21-9a8b-0c1d2e3f4a5b")
	user := "user-8f14e45f"

	// What tally's orchestrator does after each chat turn.
	turn := func(msg string) {
		summary := ai.BuildMemorySummary(tenant, msg, "（助手回答略）")
		ai.AsyncWriteMemory(c, user, summary, map[string]any{"source": "tally-ai"})
		time.Sleep(400 * time.Millisecond) // the next user turn comes later
	}

	session1, session2, probes := devScenario()
	if os.Getenv("SCENARIO") == "holdout" {
		session1, session2, probes = holdoutScenario()
	}
	for _, m := range session1 {
		turn(m)
	}
	for _, m := range session2 {
		turn(m)
	}
	ctx := context.Background()
	pass, lines, noise, staleSeen := 0, 0, 0, 0
	for _, p := range probes {
		// Raw hits with scores and supersession, for inspection.
		hits, _ := c.Search(ctx, user, p.q, 5)
		for _, h := range hits {
			_, sup := h.Metadata["superseded_by"]
			fmt.Printf("    hit score=%.3f superseded=%v %q\n", h.Score, sup, h.Content)
		}
		block := ai.AugmentMessagesWithMemoryOrFallback(c, ctx, user, p.q)
		injected := strings.TrimSuffix(block, p.q)
		iw := strings.Index(injected, p.want)
		ok := iw >= 0
		if ok && p.stale != "" {
			if is := strings.Index(injected, p.stale); is >= 0 && is < iw {
				ok = false
			}
		}
		if ok {
			pass++
		}
		for _, l := range strings.Split(injected, "\n") {
			if !strings.HasPrefix(l, "• ") {
				continue
			}
			lines++
			if !strings.Contains(l, p.want) {
				noise++
			}
			if p.stale != "" && strings.Contains(l, p.stale) {
				staleSeen++
			}
		}
		fmt.Printf("PROBE ok=%v q=%q\n%s\n", ok, p.q, indent(injected))
	}
	fmt.Printf("SCORE correct=%d/%d injected_lines=%d irrelevant_lines=%d stale_values_injected=%d\n",
		pass, len(probes), lines, noise, staleSeen)
}

func indent(s string) string {
	if strings.TrimSpace(s) == "" {
		return "    (nothing injected)"
	}
	return "    " + strings.ReplaceAll(strings.TrimRight(s, "\n"), "\n", "\n    ")
}

// The scenario the tally-side changes were looked at while being made.
func devScenario() ([]string, []string, []probe) {
	return []string{
			"客户张三要求每次送货前一天电话确认",
			"今天有哪些订单待发货？",
			"矿泉水 550ml 的进价是每箱 32 元",
			"李四的账期是 30 天",
			"上周销售额是多少？",
			"以后库存预警阈值按 50 箱算",
		}, []string{
			"矿泉水 550ml 的进价调整为每箱 35 元",
			"客户张三要求每次送货前一天电话确认",
		}, []probe{
			{q: "给张三安排明天送货，要注意什么？", want: "电话确认"},
			{q: "矿泉水现在进价多少？", want: "35 元", stale: "32 元"},
			{q: "李四的账期多久？", want: "30 天"},
			{q: "库存预警阈值是多少？", want: "50 箱"},
		}
}

// Written and frozen before any tally-side change was run against it.
func holdoutScenario() ([]string, []string, []probe) {
	return []string{
			"供应商宏达的最小起订量是 200 件",
			"昨天退货了几单？",
			"给老客户赵六的折扣是九五折",
			"周末不安排送货",
			"哪个商品库存最少？",
			"仓库盘点每月最后一个周五做",
		}, []string{
			"供应商宏达的最小起订量改为 300 件",
			"给老客户赵六的折扣是九五折",
		}, []probe{
			{q: "向宏达下单最少要订多少？", want: "300 件", stale: "200 件"},
			{q: "赵六买东西打几折？", want: "九五折"},
			{q: "这周六能送货吗？", want: "周末不安排送货"},
			{q: "盘点一般哪天做？", want: "最后一个周五"},
		}
}
