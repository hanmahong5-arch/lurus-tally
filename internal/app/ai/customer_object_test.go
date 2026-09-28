package ai

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/llmclient"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/memorusclient"
)

// objectMemory is filteringMemory plus the object view.
type objectMemory struct {
	filteringMemory
	view     *memorusclient.ObjectView
	viewErr  error
	gotAsOf  *time.Time
	gotSubj  string
	viewRead int
}

func (m *objectMemory) GetObject(_ context.Context, subject string, asOf *time.Time) (*memorusclient.ObjectView, error) {
	m.viewRead++
	m.gotSubj, m.gotAsOf = subject, asOf
	return m.view, m.viewErr
}

func fact(id, content, value string) memorusclient.Fact {
	return memorusclient.Fact{ID: id, Content: content, SlotValue: value, EffectiveFrom: time.Now(), CreatedAt: time.Now()}
}

func zhangView() *memorusclient.ObjectView {
	pay := fact("p2", "张三改用微信", "微信")
	return &memorusclient.ObjectView{
		SubjectID: CustomerSubject(zhangID),
		Slots: []memorusclient.SlotView{
			{Slot: "payment_method", Single: true, Current: &pay, Values: []memorusclient.Fact{pay}},
			{Slot: "taste", Single: false, Values: []memorusclient.Fact{
				fact("t1", "张三爱喝冰美式", "冰美式"), fact("t2", "张三也喝热拿铁", "热拿铁"),
			}},
		},
		Untyped: []memorusclient.Fact{fact("u1", "张三人很好", "")},
	}
}

// With the object view, the typed facts in the prompt are the values in
// force per slot (labelled with the slot), and the filtered search still
// contributes the untyped notes — each fact once.
func TestRecall_TypedFactsComeFromTheObjectView(t *testing.T) {
	zhang := CustomerSubject(zhangID)
	mc := &objectMemory{
		filteringMemory: filteringMemory{own: []memorusclient.Memory{
			hit("p1", "张三只收现金", 0.9, zhang, true), // history: replaced by p2
			hit("p2", "张三改用微信", 0.8, zhang, false),
			hit("u1", "张三人很好", 0.7, zhang, false),
		}},
		view: zhangView(),
	}
	out := AugmentWithCustomerMemory(mc, context.Background(), "u", "接待张三要注意什么", &CustomerRef{ID: zhangID, Name: "张三"})
	if mc.gotSubj != zhang || mc.gotAsOf != nil {
		t.Fatalf("view read for %q at %v", mc.gotSubj, mc.gotAsOf)
	}
	for _, want := range []string{"（客户 张三·付款方式）张三改用微信", "（客户 张三·口味偏好）张三爱喝冰美式", "（客户 张三·口味偏好）张三也喝热拿铁", "（客户 张三）张三人很好"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "现金") {
		t.Errorf("history must not be injected:\n%s", out)
	}
	if strings.Count(out, "张三改用微信") != 1 {
		t.Errorf("a fact in both the view and the search must appear once:\n%s", out)
	}
}

// A server without the object view (404 → ErrUnsupported) or a failing read
// leaves recall exactly as it was: the filtered search alone.
func TestRecall_FallsBackWithoutObjectView(t *testing.T) {
	zhang := CustomerSubject(zhangID)
	own := []memorusclient.Memory{hit("p2", "张三改用微信", 0.8, zhang, false)}
	for _, verr := range []error{memorusclient.ErrUnsupported, memorusclient.ErrUnavailable} {
		mc := &objectMemory{filteringMemory: filteringMemory{own: own}, viewErr: verr}
		out := AugmentWithCustomerMemory(mc, context.Background(), "u", "接待张三要注意什么", &CustomerRef{ID: zhangID, Name: "张三"})
		if mc.viewRead != 1 {
			t.Fatalf("%v: view read %d times", verr, mc.viewRead)
		}
		if !strings.Contains(out, "（客户 张三）张三改用微信") {
			t.Errorf("%v: fallback lost the customer's note:\n%s", verr, out)
		}
	}
}

// Two rows in force for one single-valued slot are shown as a disagreement
// the model must raise, not as a value it may choose.
func TestRecall_ConflictIsShownNotChosen(t *testing.T) {
	zhang := CustomerSubject(zhangID)
	cash, monthly := fact("c1", "张三只收现金", "现金"), fact("c2", "张三改月结", "月结")
	view := &memorusclient.ObjectView{
		SubjectID: zhang,
		Slots: []memorusclient.SlotView{{
			Slot: "payment_method", Single: true, Current: &monthly, Values: []memorusclient.Fact{monthly, cash},
		}},
		Conflicts: []memorusclient.Conflict{{Slot: "payment_method", IDs: []string{"c1", "c2"}, Reason: "multiple_current"}},
	}
	mc := &objectMemory{view: view}
	out := AugmentWithCustomerMemory(mc, context.Background(), "u", "张三怎么付款", &CustomerRef{ID: zhangID, Name: "张三"})
	line := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "记录不一致") {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("no conflict line:\n%s", out)
	}
	if !strings.Contains(line, "（客户 张三·付款方式）") || !strings.Contains(line, "张三只收现金") || !strings.Contains(line, "张三改月结") || !strings.Contains(line, "不要自行选一个") {
		t.Errorf("the conflict line must name both values and say to ask:\n%s", line)
	}
}

// As-of questions read the typed values from the object view at that moment
// and only the untyped notes from the time search.
func TestRecallAsOf_ObjectView(t *testing.T) {
	zhang := CustomerSubject(zhangID)
	cash := fact("c1", "张三只收现金", "现金")
	view := &memorusclient.ObjectView{
		SubjectID: zhang,
		Slots:     []memorusclient.SlotView{{Slot: "payment_method", Single: true, Current: &cash, Values: []memorusclient.Fact{cash}}},
	}
	mc := &asOfObjectMemory{
		asOfMemory: asOfMemory{answer: []memorusclient.Memory{
			hit("c1", "张三只收现金", 0.9, zhang, false),
			{ID: "s1", Content: "张三三月还没搬家", Metadata: map[string]any{"metadata": map[string]any{MemorySubjectKey: zhang, MemorySlotKey: "delivery_address"}}},
			hit("u1", "张三人很好", 0.5, zhang, false),
		}},
		view: view,
	}
	call := llmclient.Message{Role: "assistant", ToolCalls: []llmclient.ToolCall{{
		ID: "a1", Type: "function",
		Function: llmclient.ToolCallFunction{Name: recallAsOfTool, Arguments: `{"customer":"张三","as_of":"2026-03"}`},
	}}}
	o, reqs := factOrchestrator(t, []CustomerRef{{ID: zhangID, Name: "张三"}}, mc,
		call, llmclient.Message{Role: "assistant", Content: "三月时张三只收现金"})
	if _, err := o.Chat(context.Background(), ChatInput{TenantID: uuid.New(), UserMessage: "三月时张三怎么付款"}); err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 4, 1, 0, 0, 0, 0, shopZone).Add(-time.Nanosecond)
	if mc.gotAsOf == nil || !mc.gotAsOf.Equal(want) || mc.gotSubj != zhang {
		t.Errorf("view read at %v for %q, want end of March for 张三", mc.gotAsOf, mc.gotSubj)
	}
	res := toolResult(t, reqs())
	if !strings.Contains(res, "付款方式：张三只收现金") || !strings.Contains(res, "张三人很好") {
		t.Errorf("typed value labelled + untyped note: %s", res)
	}
	if strings.Count(res, "张三只收现金") != 1 || strings.Contains(res, "搬家") {
		t.Errorf("no duplicate, no typed row the view did not hold then: %s", res)
	}
}

type asOfObjectMemory struct {
	asOfMemory
	view    *memorusclient.ObjectView
	gotSubj string
	gotAsOf *time.Time
}

func (m *asOfObjectMemory) GetObject(_ context.Context, subject string, asOf *time.Time) (*memorusclient.ObjectView, error) {
	m.gotSubj, m.gotAsOf = subject, asOf
	return m.view, nil
}

func TestParseSince_StartOfThePeriodNeverInTheFuture(t *testing.T) {
	now := time.Date(2026, 9, 26, 10, 0, 0, 0, shopZone)
	cases := map[string]string{
		"2026-03-15": "2026-03-15T00:00:00+08:00",
		"2026-03":    "2026-03-01T00:00:00+08:00",
		"3月":         "2026-03-01T00:00:00+08:00",
		"11":         "2025-11-01T00:00:00+08:00",
	}
	for in, want := range cases {
		got, err := parseSince(in, now)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		w, _ := time.Parse(time.RFC3339, want)
		if !got.Equal(w) {
			t.Errorf("%q = %s, want %s", in, got.Format(time.RFC3339), want)
		}
	}
	for _, bad := range []string{"", "2027-01", "2026-10-01", "上个月"} {
		if _, err := parseSince(bad, now); err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
}

// A fact with `since` / `replaces` goes through AddWithOptions; a plain one
// keeps going through Add.
type optionsMemory struct {
	recordingMemory
	opts     []memorusclient.AddOptions
	unmatch  bool
	optsUsed int
}

func (m *optionsMemory) AddWithOptions(_ context.Context, _, content string, meta map[string]any, opts memorusclient.AddOptions) (*memorusclient.AddResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.optsUsed++
	m.opts = append(m.opts, opts)
	m.writes = append(m.writes, memWrite{content, meta})
	return &memorusclient.AddResult{ID: "n", Event: "ADD", UnmatchedReplacesValue: m.unmatch}, nil
}

func TestRememberCustomerFact_SinceAndReplacesGoThroughAddWithOptions(t *testing.T) {
	mc := &optionsMemory{}
	o, reqs := factOrchestrator(t, []CustomerRef{{ID: zhangID, Name: "张三"}}, mc,
		factCall(`{"customer":"张三","attribute":"taste","value":"澳白","quote":"张三从三月起改喝澳白，不喝冰美式了","since":"2026-03","replaces":"冰美式"}`),
		llmclient.Message{Role: "assistant", Content: "记住了"})
	if _, err := o.Chat(context.Background(), ChatInput{TenantID: uuid.New(), UserMessage: "张三从三月起改喝澳白，不喝冰美式了"}); err != nil {
		t.Fatal(err)
	}
	if mc.optsUsed != 1 || len(mc.opts) != 1 {
		t.Fatalf("AddWithOptions used %d times", mc.optsUsed)
	}
	got := mc.opts[0]
	if got.ReplacesValue != "冰美式" || got.ValidFrom == nil || got.ValidFrom.In(shopZone).Format("2006-01-02") != "2026-03-01" {
		t.Errorf("opts = %+v", got)
	}
	res := toolResult(t, reqs())
	if !strings.Contains(res, `"since":"2026-03-01"`) || !strings.Contains(res, `"replaced":true`) {
		t.Errorf("tool result: %s", res)
	}

	// Unmatched old value: saved alongside, and the model is told.
	mc = &optionsMemory{unmatch: true}
	o, reqs = factOrchestrator(t, []CustomerRef{{ID: zhangID, Name: "张三"}}, mc,
		factCall(`{"customer":"张三","attribute":"taste","value":"澳白","quote":"张三改喝澳白","replaces":"冰拿铁"}`),
		llmclient.Message{Role: "assistant", Content: "记住了"})
	if _, err := o.Chat(context.Background(), ChatInput{TenantID: uuid.New(), UserMessage: "张三改喝澳白"}); err != nil {
		t.Fatal(err)
	}
	res = toolResult(t, reqs())
	if !strings.Contains(res, `"replaced":false`) || !strings.Contains(res, "冰拿铁") {
		t.Errorf("tool result: %s", res)
	}

	// A single-valued slot ignores `replaces` and a plain fact still uses Add.
	mc = &optionsMemory{}
	o, reqs = factOrchestrator(t, []CustomerRef{{ID: zhangID, Name: "张三"}}, mc,
		factCall(zhangPaysWeChat), llmclient.Message{Role: "assistant", Content: "记住了"})
	if _, err := o.Chat(context.Background(), ChatInput{TenantID: uuid.New(), UserMessage: "张三以后改用微信付款"}); err != nil {
		t.Fatal(err)
	}
	if mc.optsUsed != 0 || len(mc.settled()) != 1 {
		t.Errorf("a plain fact must go through Add: optsUsed=%d writes=%d", mc.optsUsed, len(mc.settled()))
	}
	_ = reqs
}
