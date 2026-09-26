package ai

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/llmclient"
)

// TestLive_AsOfDates is an env-gated oracle for relative-time questions about
// a customer's past (「上个月李四怎么付款」). It sends the production prompt and
// tools to the real gateway and checks, on the first turn, that the model
// calls recall_customer_facts_as_of with an as_of inside the period the
// question names — resolved by parseAsOf exactly as the tool would.
//
// Cases frozen 2026-09-26 before the first run. Prediction for the prompt
// without today's date: the model has no anchor for 上个月/去年/上周/前天 and
// guesses from its training data, so those cases mostly fail; 「三月份」 can
// pass through parseAsOf's month-only rule. With the date: all dated cases
// pass, and 「现在」 never calls the tool.
//
// Measured (deepseek-v4-flash, 3 runs): dated cases 3/15 → 14/15 (the miss
// read 上周三 as this week's Wednesday); 「李四现在怎么付款」 avoided the tool 2/3
// → 0/3, calling it with today's date instead.
//
//	TALLY_LLM_LIVE=1 TALLY_LLM_KEY=sk-... TALLY_LLM_REPEAT=3 \
//	  go test ./internal/app/ai/ -run TestLive_AsOfDates -v -count=1
func TestLive_AsOfDates(t *testing.T) {
	if os.Getenv("TALLY_LLM_LIVE") != "1" {
		t.Skip("set TALLY_LLM_LIVE=1 (+ TALLY_LLM_KEY) to run the live as-of date oracle")
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
	if v, err := strconv.Atoi(os.Getenv("TALLY_LLM_REPEAT")); err == nil && v > 0 {
		repeat = v
	}
	cli, err := llmclient.New(llmclient.Config{BaseURL: base, APIKey: key, DefaultModel: model, HTTPTimeout: 90 * time.Second})
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().In(shopZone)
	day := func(t time.Time) time.Time { return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, shopZone) }
	today := day(now)
	thisMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, shopZone)
	// [from, to): the as_of must fall inside; zero window = tool must NOT be called.
	type window struct{ from, to time.Time }
	// 上周三 = Wednesday of the previous Monday-first week.
	monday := today.AddDate(0, 0, -int((today.Weekday()+6)%7))
	lastWednesday := monday.AddDate(0, 0, -5)
	cases := []struct {
		q string
		w window
	}{
		{"上个月李四是怎么付款的？", window{thisMonth.AddDate(0, -1, 0), thisMonth}},
		{"去年这个时候张三的货送到哪？", window{thisMonth.AddDate(-1, -1, 0), thisMonth.AddDate(-1, 2, 0)}},
		{"三月份的时候李四怎么结账的？", window{time.Date(now.Year(), 3, 1, 0, 0, 0, 0, shopZone), time.Date(now.Year(), 4, 1, 0, 0, 0, 0, shopZone)}},
		{"上周三王五要的是什么包装？", window{lastWednesday, lastWednesday.AddDate(0, 0, 1)}},
		{"前天赵六说的送货时间是几点？", window{today.AddDate(0, 0, -2), today.AddDate(0, 0, -1)}},
		{"李四现在怎么付款？", window{}},
		// Added after the first run with the date showed 「现在」 calling the
		// tool with today's date. That answer is still right (as_of = today
		// reads the current facts) but costs one extra model round; a
		// stricter tool description did not change it (2/9), so it was not kept.
		{"张三目前的送货地址是哪？", window{}},
		{"王五最近要什么包装？", window{}},
	}

	o := &Orchestrator{memory: &asOfMemory{}}
	tools := o.toolDefs()
	pass, runs := 0, 0
	t.Logf("=== LIVE as-of dates: model=%s repeat=%d today=%s ===", model, repeat, today.Format("2006-01-02 Mon"))
	for r := 0; r < repeat; r++ {
		for _, c := range cases {
			msgs := o.withMemoryPolicy(buildMessages(ChatInput{TenantID: uuid.New(), UserMessage: c.q}))
			resp, cerr := cli.Chat(context.Background(), model, msgs, tools)
			got, asOf := "NONE", ""
			if cerr != nil {
				got = "ERR " + cerr.Error()
			} else if len(resp.Choices) > 0 {
				for _, tc := range resp.Choices[0].Message.ToolCalls {
					if tc.Function.Name == recallAsOfTool {
						got = recallAsOfTool
						var a struct {
							AsOf string `json:"as_of"`
						}
						_ = json.Unmarshal([]byte(tc.Function.Arguments), &a)
						asOf = a.AsOf
					}
				}
			}
			ok := false
			if c.w.from.IsZero() {
				ok = got == "NONE"
			} else if got == recallAsOfTool {
				at, perr := parseAsOf(asOf, now)
				ok = perr == nil && !at.Before(c.w.from) && at.Before(c.w.to)
			}
			runs++
			mark := "FAIL"
			if ok {
				pass++
				mark = "OK"
			}
			t.Logf("[run%d] %-4s got=%s as_of=%q want=[%s,%s) q=%q", r, mark, got, asOf,
				c.w.from.Format("2006-01-02"), c.w.to.Format("2006-01-02"), c.q)
		}
	}
	t.Logf("=== TOTAL %d/%d (model=%s) ===", pass, runs, model)
}
