package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	repoai "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/ai"
	"github.com/hanmahong5-arch/lurus-tally/internal/app/ai"
	domainai "github.com/hanmahong5-arch/lurus-tally/internal/domain/ai"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/llmclient"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/memorusclient"
)

// memPlans keeps proposed plans in memory; nothing here confirms them.
type memPlans struct {
	mu sync.Mutex
	m  map[uuid.UUID]*domainai.Plan
}

func (p *memPlans) SavePlan(_ context.Context, plan *domainai.Plan) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.m[plan.ID] = plan
	return nil
}

func (p *memPlans) GetPlan(_ context.Context, _, id uuid.UUID) (*domainai.Plan, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.m[id], nil
}

func (p *memPlans) UpdatePlan(ctx context.Context, plan *domainai.Plan) error {
	return p.SavePlan(ctx, plan)
}

func (p *memPlans) ListByTenant(context.Context, uuid.UUID, string) ([]*domainai.Plan, error) {
	return nil, nil
}

// runLLM drives tally's real Orchestrator.Chat (LLM=1).
func runLLM(ctx context.Context, appDB dbHandle, sales *repoai.SQLSaleRepo, tenant uuid.UUID) {
	llm, err := llmclient.New(llmclient.Config{
		BaseURL: os.Getenv("NEWAPI_BASE_URL"), APIKey: os.Getenv("NEWAPI_API_KEY"),
		HTTPTimeout: 120 * time.Second,
	})
	must(err, "llm client")
	mc, err := memorusclient.New(memorusclient.Config{BaseURL: os.Getenv("MEMORUS_URL"), APIKey: "probekey"})
	if err != nil || mc == nil {
		panic(fmt.Sprint("memorus client: ", err))
	}
	reg := ai.NewRegistry(repoai.NewSQLProductRepo(appDB.db), repoai.NewSQLStockRepo(appDB.db), sales,
		repoai.NewSQLExchangeRateRepo(appDB.db))
	model := os.Getenv("MODEL")
	o := ai.NewOrchestrator(llm, reg, &memPlans{m: map[uuid.UUID]*domainai.Plan{}}, model).
		WithMemory(mc).WithCustomerResolver(sales)

	stmtOK := 0
	for _, s := range llmStatements {
		out, err := o.Chat(ctx, ai.ChatInput{TenantID: tenant, UserMessage: s})
		if err != nil {
			fmt.Printf("STATEMENT err=%v q=%q\n", err, s)
			continue
		}
		bad := ""
		for _, m := range llmStatementMustNot {
			if strings.Contains(out.AssistantText, m) {
				bad = m
				break
			}
		}
		if bad == "" {
			stmtOK++
		}
		fmt.Printf("STATEMENT ok=%v q=%q denies=%q\n  a=%q\n", bad == "", s, bad, oneLine(out.AssistantText))
		time.Sleep(2 * time.Second) // the async memory write lands before the next turn
	}

	passed, total := 0, 0
	perCheck := make([]int, len(llmChecks))
	for rep := 0; rep < llmRepeats; rep++ {
		for i, c := range llmChecks {
			total++
			out, err := o.Chat(ctx, ai.ChatInput{TenantID: tenant, UserMessage: c.q})
			if err != nil {
				fmt.Printf("CHECK rep=%d ok=false q=%q err=%v\n", rep, c.q, err)
				continue
			}
			var tools []string
			for _, t := range out.ToolCalls {
				tools = append(tools, t.ToolName+t.ArgsJSON)
			}
			why := judge(c, out)
			ok := why == ""
			if ok {
				passed++
				perCheck[i]++
			}
			fmt.Printf("CHECK rep=%d ok=%v q=%q fail=%q\n  tools=%v\n  a=%q\n", rep, ok, c.q, why, tools, oneLine(out.AssistantText))
		}
	}
	for i, c := range llmChecks {
		fmt.Printf("PER-CHECK %d/%d q=%q\n", perCheck[i], llmRepeats, c.q)
	}
	fmt.Printf("SCORE llm model=%s | statements_acknowledged %d/%d | checks passed %d/%d\n",
		modelName(model), stmtOK, len(llmStatements), passed, total)
}

func judge(c llmCheck, out *ai.ChatOutput) string {
	a := out.AssistantText
	if c.tool != "" {
		called := false
		for _, t := range out.ToolCalls {
			called = called || t.ToolName == c.tool
		}
		if !called {
			return "tool " + c.tool + " not called"
		}
	}
	for _, s := range c.mustHave {
		if !strings.Contains(a, s) {
			return "missing " + s
		}
	}
	for _, s := range c.mustNot {
		if strings.Contains(a, s) {
			return "contains " + s
		}
	}
	for _, s := range llmAnswerMustNot {
		if strings.Contains(a, s) {
			return "distrusts memory: " + s
		}
	}
	if len(c.anyOf) > 0 {
		hit := false
		for _, s := range c.anyOf {
			hit = hit || strings.Contains(a, s)
		}
		if !hit {
			return fmt.Sprintf("none of %v", c.anyOf)
		}
	}
	return ""
}

func oneLine(s string) string { return strings.ReplaceAll(s, "\n", " ⏎ ") }

func modelName(m string) string {
	if m == "" {
		return "(orchestrator default)"
	}
	return m
}
