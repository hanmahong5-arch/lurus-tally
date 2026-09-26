package main

// Customer attribute slots (LLM=1 SLOTS=1): does a changed value of a
// single-valued attribute replace the old one when the two statements share
// no wording (店在城东 → 搬到城南)? Frozen — dev and held-out written together,
// before remember_customer_fact was run against a model. The held-out set is
// quoted, never tuned on.
//
// The main metric is read from memorus itself (GET /memories: superseded_by),
// not from the model's answers. The answers to "接待 X 要注意什么" are dumped
// to SLOTS_JUDGE for an LLM judge (stale value presented as current).
//
// Control: the same scenario on the tree before remember_customer_fact (no
// tool; free-text notes + the wording rule), same memorus.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	repoai "github.com/hanmahong5-arch/lurus-tally/internal/adapter/repo/ai"
	"github.com/hanmahong5-arch/lurus-tally/internal/app/ai"
	domainai "github.com/hanmahong5-arch/lurus-tally/internal/domain/ai"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/llmclient"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/memorusclient"
)

// slotStatement is one thing the owner tells the drawer, and the attribute
// it is about.
type slotStatement struct {
	customer, text, slot string
}

// slotPair: the customer's value containing oldKey must end up replaced by
// the one containing newKey.
type slotPair struct {
	customer, slot, oldKey, newKey string
}

// slotKeep: a row that must stay current (multi-valued, or another slot).
type slotKeep struct {
	customer, key, why string
}

// slotChain (v2): a customer's values of one attribute, oldest → newest. Rows
// with the newest value must be current; every row with an older value must
// be superseded by a row with a later value of the chain.
type slotChain struct {
	customer, slot string
	keys           []string
}

type slotScenario struct {
	before, after []slotStatement
	pairs         []slotPair
	keep          []slotKeep
	chains        []slotChain // v2 only
}

func devSlots() slotScenario {
	return slotScenario{
		before: []slotStatement{
			{"王五", "王五的店在城东菜市场旁边", "delivery_address"},
			{"孙七", "孙七每周二来进货", "delivery_time"},
			{"李四", "李四只收现金", "payment_method"},
			{"张三", "张三的发票开普票就行", "invoice"},
			{"赵六", "赵六那边的对接人是他老婆", "contact"},
			{"赵六", "赵六对花生过敏", "allergy"},
			{"李四", "李四的货都要用塑料袋装", "packaging"},
		},
		after: []slotStatement{
			{"王五", "王五搬到城南了", "delivery_address"},
			{"孙七", "孙七以后改成周四来", "delivery_time"},
			{"李四", "李四改用微信了", "payment_method"},
			{"张三", "张三以后要开专票", "invoice"},
			{"赵六", "赵六那边以后找他儿子对接", "contact"},
			{"赵六", "赵六也对芒果过敏", "allergy"},
		},
		pairs: []slotPair{
			{"王五", "delivery_address", "城东", "城南"},
			{"孙七", "delivery_time", "周二", "周四"},
			{"李四", "payment_method", "现金", "微信"},
			{"张三", "invoice", "普票", "专票"},
			{"赵六", "contact", "老婆", "儿子"},
		},
		keep: []slotKeep{
			{"赵六", "花生", "multi-valued allergy"},
			{"赵六", "芒果", "multi-valued allergy"},
			{"李四", "塑料袋", "another slot of a customer whose payment changed"},
		},
	}
}

func holdoutSlots() slotScenario {
	return slotScenario{
		before: []slotStatement{
			{"刘二", "刘二每月25号来结一次账", "settlement"},
			{"周八", "周八的货送到她店门口就行", "delivery_address"},
			{"吴九", "吴九付款用支付宝", "payment_method"},
			{"陈一鸣", "陈一鸣要下午三点以后送货", "delivery_time"},
			{"郑十", "郑十的货要装纸箱", "packaging"},
			{"陈一", "陈一爱吃辣", "taste"},
			{"郑十", "郑十那边结账找他老婆", "contact"},
		},
		after: []slotStatement{
			{"刘二", "刘二说以后月底一起结", "settlement"},
			{"周八", "周八以后要直接送上楼到她家", "delivery_address"},
			{"吴九", "吴九以后刷卡", "payment_method"},
			{"陈一鸣", "陈一鸣改成上午送", "delivery_time"},
			{"郑十", "郑十以后都用编织袋", "packaging"},
			{"陈一", "陈一也喜欢吃酸的", "taste"},
		},
		pairs: []slotPair{
			{"刘二", "settlement", "25号", "月底"},
			{"周八", "delivery_address", "店门口", "她家"},
			{"吴九", "payment_method", "支付宝", "刷卡"},
			{"陈一鸣", "delivery_time", "下午", "上午"},
			{"郑十", "packaging", "纸箱", "编织袋"},
		},
		keep: []slotKeep{
			{"陈一", "辣", "multi-valued taste"},
			{"陈一", "酸", "multi-valued taste"},
			{"郑十", "老婆", "another slot of a customer whose packaging changed"},
		},
	}
}

// storedMemory is a row of GET /api/v1/memories.
type storedMemory struct {
	ID           string         `json:"id"`
	Content      string         `json:"content"`
	SupersededBy *string        `json:"superseded_by"`
	Metadata     map[string]any `json:"metadata"`
}

func (m storedMemory) meta(k string) string {
	s, _ := m.Metadata[k].(string)
	return s
}

func (m storedMemory) mentions(key string) bool {
	return strings.Contains(m.Content, key) || strings.Contains(m.meta("slot_value"), key)
}

func (m storedMemory) current() bool { return m.SupersededBy == nil || *m.SupersededBy == "" }

// judgeItem is one answer for the stale-value judge.
type judgeItem struct {
	Run     string   `json:"run"`
	Q       string   `json:"q"`
	A       string   `json:"a"`
	Stale   []string `json:"stale"`
	Current []string `json:"current"`
}

func runSlots(ctx context.Context, appDB dbHandle, sales *repoai.SQLSaleRepo, tenant uuid.UUID, label string, partners map[string]uuid.UUID) {
	sc := devSlots()
	switch label {
	case "holdout":
		sc = holdoutSlots()
	case "v2-dev":
		sc = devSlotsV2()
	case "v2-holdout":
		sc = holdoutSlotsV2()
	}
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

	// 1. The owner's statements, one fresh conversation each.
	called, slotRight, total := 0, 0, 0
	for _, s := range append(append([]slotStatement{}, sc.before...), sc.after...) {
		total++
		out, err := o.Chat(ctx, ai.ChatInput{TenantID: tenant, UserMessage: s.text})
		if err != nil {
			fmt.Printf("STATEMENT err=%v q=%q\n", err, s.text)
			continue
		}
		var attrs []string
		for _, t := range out.ToolCalls {
			if t.ToolName != "remember_customer_fact" {
				continue
			}
			var args struct {
				Attribute string `json:"attribute"`
			}
			_ = json.Unmarshal([]byte(t.ArgsJSON), &args)
			attrs = append(attrs, args.Attribute)
		}
		right := false
		for _, a := range attrs {
			// "a|b" (v2): the attribute is genuinely unclear, either is right.
			for _, want := range strings.Split(s.slot, "|") {
				right = right || a == want
			}
		}
		if len(attrs) > 0 {
			called++
		}
		if right {
			slotRight++
		}
		fmt.Printf("STATEMENT tool=%v slot_ok=%v want=%s got=%v q=%q\n  a=%q\n", len(attrs) > 0, right, s.slot, attrs, s.text, oneLine(out.AssistantText))
		for _, t := range out.ToolCalls {
			fmt.Printf("  call %s %s -> %s\n", t.ToolName, t.ArgsJSON, t.ResultJSON)
		}
		time.Sleep(2 * time.Second) // any async write lands before the next turn
	}

	// 2. What memorus holds.
	rows := listMemories(tenant.String())
	bySubject := map[string][]storedMemory{}
	for _, r := range rows {
		bySubject[r.meta("subject_id")] = append(bySubject[r.meta("subject_id")], r)
		sup := "-"
		if !r.current() {
			sup = *r.SupersededBy
		}
		fmt.Printf("ROW id=%s subject=%s slot=%s superseded_by=%s %q\n", r.ID, r.meta("subject_id"), r.meta("slot"), sup, r.Content)
	}
	subjectOf := func(name string) string { return ai.CustomerSubject(partners[name]) }

	replaced := 0
	expectedOld := map[string]bool{} // row ids that should be superseded
	for _, p := range sc.pairs {
		mine := bySubject[subjectOf(p.customer)]
		newIDs := map[string]bool{}
		var olds, news []storedMemory
		for _, r := range mine {
			switch {
			case r.mentions(p.newKey):
				news = append(news, r)
				newIDs[r.ID] = true
			case r.mentions(p.oldKey):
				olds = append(olds, r)
				expectedOld[r.ID] = true
			}
		}
		ok := len(olds) > 0 && len(news) > 0
		for _, r := range olds {
			ok = ok && !r.current() && newIDs[*r.SupersededBy]
		}
		for _, r := range news {
			ok = ok && r.current()
		}
		if ok {
			replaced++
		}
		fmt.Printf("PAIR ok=%v %s %s: %s → %s (old rows %d, new rows %d)\n", ok, p.customer, p.slot, p.oldKey, p.newKey, len(olds), len(news))
	}
	chainsOK := 0
	for _, ch := range sc.chains {
		// Each row belongs to the newest value it mentions.
		byValue := make([][]storedMemory, len(ch.keys))
		for _, r := range bySubject[subjectOf(ch.customer)] {
			for i := len(ch.keys) - 1; i >= 0; i-- {
				if r.mentions(ch.keys[i]) {
					byValue[i] = append(byValue[i], r)
					break
				}
			}
		}
		ok, counts := true, make([]int, len(ch.keys))
		later := map[string]bool{} // ids of rows holding a later value
		for i := len(ch.keys) - 1; i >= 0; i-- {
			counts[i] = len(byValue[i])
			ok = ok && len(byValue[i]) > 0
			for _, r := range byValue[i] {
				if i == len(ch.keys)-1 {
					ok = ok && r.current()
					continue
				}
				expectedOld[r.ID] = true
				ok = ok && !r.current() && later[*r.SupersededBy]
			}
			for _, r := range byValue[i] {
				later[r.ID] = true
			}
		}
		if ok {
			chainsOK++
		}
		fmt.Printf("CHAIN ok=%v %s %s: %s (rows per value %v)\n", ok, ch.customer, ch.slot, strings.Join(ch.keys, " → "), counts)
	}
	wrong := 0
	for _, r := range rows {
		if !r.current() && !expectedOld[r.ID] {
			wrong++
			fmt.Printf("WRONG-SUPERSEDE %q (subject %s)\n", r.Content, r.meta("subject_id"))
		}
	}
	keepOK := 0
	for _, k := range sc.keep {
		found, cur := false, true
		for _, r := range bySubject[subjectOf(k.customer)] {
			if r.mentions(k.key) {
				found = true
				cur = cur && r.current()
			}
		}
		if found && cur {
			keepOK++
		}
		fmt.Printf("KEEP ok=%v %s %s (%s)\n", found && cur, k.customer, k.key, k.why)
	}

	// 3. Answers, for the judge.
	var items []judgeItem
	seen := map[string]bool{}
	for _, p := range sc.pairs {
		if seen[p.customer] {
			continue
		}
		seen[p.customer] = true
		it := judgeItem{Run: label, Q: "接待" + p.customer + "要注意什么？"}
		for _, s := range sc.before {
			if s.customer == p.customer && pairedSlot(sc.pairs, s) {
				it.Stale = append(it.Stale, s.text)
			}
		}
		for _, s := range sc.after {
			if s.customer == p.customer {
				it.Current = append(it.Current, s.text)
			}
		}
		for _, s := range sc.before {
			if s.customer == p.customer && !pairedSlot(sc.pairs, s) {
				it.Current = append(it.Current, s.text)
			}
		}
		out, err := o.Chat(ctx, ai.ChatInput{TenantID: tenant, UserMessage: it.Q})
		if err != nil {
			fmt.Printf("QUESTION err=%v q=%q\n", err, it.Q)
			continue
		}
		it.A = out.AssistantText
		items = append(items, it)
		fmt.Printf("QUESTION q=%q\n  a=%q\n", it.Q, oneLine(it.A))
	}
	for _, ch := range sc.chains {
		if seen[ch.customer] {
			continue
		}
		seen[ch.customer] = true
		it := judgeItem{Run: label, Q: "接待" + ch.customer + "要注意什么？"}
		for _, s := range append(append([]slotStatement{}, sc.before...), sc.after...) {
			switch {
			case s.customer != ch.customer:
			case chainStale(sc.chains, s):
				it.Stale = append(it.Stale, s.text)
			default:
				it.Current = append(it.Current, s.text)
			}
		}
		out, err := o.Chat(ctx, ai.ChatInput{TenantID: tenant, UserMessage: it.Q})
		if err != nil {
			fmt.Printf("QUESTION err=%v q=%q\n", err, it.Q)
			continue
		}
		it.A = out.AssistantText
		items = append(items, it)
		fmt.Printf("QUESTION q=%q\n  a=%q\n", it.Q, oneLine(it.A))
	}
	if path := os.Getenv("SLOTS_JUDGE"); path != "" {
		b, _ := json.MarshalIndent(items, "", " ")
		must(os.WriteFile(path, b, 0o644), "write judge items")
	}

	fmt.Printf("SCORE slots scenario=%s model=%s | replaced %d/%d | wrong_supersedes %d | keep %d/%d | tool_called %d/%d slot_right %d/%d | rows %d\n",
		label, modelName(model), replaced, len(sc.pairs), wrong, keepOK, len(sc.keep), called, total, slotRight, total, len(rows))
	if len(sc.chains) > 0 {
		fmt.Printf("SCORE slot-chains scenario=%s model=%s | chains_replaced %d/%d\n", label, modelName(model), chainsOK, len(sc.chains))
	}
}

// chainStale: s states an older value of one of the chains.
func chainStale(chains []slotChain, s slotStatement) bool {
	for _, c := range chains {
		if c.customer != s.customer || c.slot != s.slot {
			continue
		}
		n := len(c.keys)
		for _, k := range c.keys[:n-1] {
			if strings.Contains(s.text, k) && !strings.Contains(s.text, c.keys[n-1]) {
				return true
			}
		}
	}
	return false
}

// pairedSlot: s is the old value of one of the pairs.
func pairedSlot(pairs []slotPair, s slotStatement) bool {
	for _, p := range pairs {
		if p.customer == s.customer && p.slot == s.slot {
			return true
		}
	}
	return false
}

func listMemories(userID string) []storedMemory {
	u := strings.TrimSuffix(os.Getenv("MEMORUS_URL"), "/") + "/memories?limit=1000&user_id=" + url.QueryEscape(userID)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	must(err, "list request")
	req.Header.Set("X-API-Key", "probekey")
	resp, err := http.DefaultClient.Do(req)
	must(err, "list memories")
	defer resp.Body.Close() //nolint:errcheck
	var body struct {
		Memories []storedMemory `json:"memories"`
	}
	must(json.NewDecoder(resp.Body).Decode(&body), "decode memories")
	return body.Memories
}
