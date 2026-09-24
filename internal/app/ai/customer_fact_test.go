package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/llmclient"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/memorusclient"
)

type memWrite struct {
	content string
	meta    map[string]any
}

// recordingMemory records every Add, including the fire-and-forget ones.
type recordingMemory struct {
	mu     sync.Mutex
	writes []memWrite
}

func (m *recordingMemory) Search(context.Context, string, string, int) ([]memorusclient.Memory, error) {
	return nil, nil
}

func (m *recordingMemory) Add(_ context.Context, _, content string, meta map[string]any) (*memorusclient.Memory, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writes = append(m.writes, memWrite{content, meta})
	return &memorusclient.Memory{Content: content}, nil
}

// settled returns the writes once the async write-back had time to land.
func (m *recordingMemory) settled() []memWrite {
	time.Sleep(150 * time.Millisecond)
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]memWrite(nil), m.writes...)
}

// scriptedLLM answers request i with replies[i] (the last one repeats) and
// records every request.
func scriptedLLM(t *testing.T, replies ...llmclient.Message) (*httptest.Server, func() []llmclient.ChatRequest) {
	t.Helper()
	var mu sync.Mutex
	var reqs []llmclient.ChatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req llmclient.ChatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		mu.Lock()
		reqs = append(reqs, req)
		n := len(reqs)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(chatRespJSON(t, replies[min(n, len(replies))-1]))
	}))
	t.Cleanup(srv.Close)
	return srv, func() []llmclient.ChatRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]llmclient.ChatRequest(nil), reqs...)
	}
}

func factCall(args string) llmclient.Message {
	return llmclient.Message{Role: "assistant", ToolCalls: []llmclient.ToolCall{{
		ID: "f1", Type: "function",
		Function: llmclient.ToolCallFunction{Name: rememberCustomerFactTool, Arguments: args},
	}}}
}

func factOrchestrator(t *testing.T, cands []CustomerRef, mc MemoryClient, replies ...llmclient.Message) (*Orchestrator, func() []llmclient.ChatRequest) {
	srv, reqs := scriptedLLM(t, replies...)
	reg := NewRegistry(&cxProductRepo{}, &cxStockRepo{}, &custSaleRepo{cands: cands}, &cxExchangeRepo{})
	o := NewOrchestrator(newCxLLMClient(t, srv), reg, newCxPlanStore(), "m")
	if mc != nil {
		o = o.WithMemory(mc)
	}
	return o, reqs
}

// toolResult is the content of the tool message the model got back.
func toolResult(t *testing.T, reqs []llmclient.ChatRequest) string {
	t.Helper()
	last := reqs[len(reqs)-1].Messages
	for i := len(last) - 1; i >= 0; i-- {
		if last[i].Role == "tool" {
			s, _ := last[i].Content.(string)
			return s
		}
	}
	t.Fatalf("no tool result sent back: %+v", last)
	return ""
}

const zhangPaysWeChat = `{"customer":"张三","attribute":"payment_method","value":"微信","quote":"张三以后改用微信付款"}`

func TestRememberCustomerFact_WritesTheSlotAndSkipsTheFreeTextNote(t *testing.T) {
	for _, stream := range []bool{false, true} {
		mc := &recordingMemory{}
		o, reqs := factOrchestrator(t, []CustomerRef{{ID: zhangID, Name: "张三"}}, mc,
			factCall(zhangPaysWeChat), llmclient.Message{Role: "assistant", Content: "记住了"})
		in := ChatInput{TenantID: uuid.New(), UserMessage: "张三以后改用微信付款"}
		var err error
		if stream {
			_, err = o.StreamChat(context.Background(), in, func(string) {})
		} else {
			_, err = o.Chat(context.Background(), in)
		}
		if err != nil {
			t.Fatal(err)
		}
		writes := mc.settled()
		if len(writes) != 1 {
			t.Fatalf("stream=%v: want exactly the tool's write, got %+v", stream, writes)
		}
		w := writes[0]
		if w.content != "张三以后改用微信付款" {
			t.Errorf("content = %q", w.content)
		}
		want := map[string]any{
			MemorySubjectKey:    CustomerSubject(zhangID),
			MemorySlotKey:       "payment_method",
			MemorySlotSingleKey: true,
			MemorySlotValueKey:  "微信",
			"tally_tenant_id":   in.TenantID.String(),
		}
		for k, v := range want {
			if w.meta[k] != v {
				t.Errorf("meta[%s] = %v, want %v (all: %v)", k, w.meta[k], v, w.meta)
			}
		}
		if res := toolResult(t, reqs()); !strings.Contains(res, `"saved":true`) {
			t.Errorf("model must learn the fact was saved: %s", res)
		}
	}
}

func TestRememberCustomerFact_AmbiguousCustomerIsNotWritten(t *testing.T) {
	mc := &recordingMemory{}
	o, reqs := factOrchestrator(t,
		[]CustomerRef{{ID: zhangID, Name: "张三"}, {ID: zhangfeng, Name: "张三丰"}}, mc,
		factCall(`{"customer":"张","attribute":"payment_method","value":"微信","quote":"老张改用微信"}`),
		llmclient.Message{Role: "assistant", Content: "是哪位张先生？"})
	if _, err := o.Chat(context.Background(), ChatInput{TenantID: uuid.New(), UserMessage: "老张改用微信"}); err != nil {
		t.Fatal(err)
	}
	if w := mc.settled(); len(w) != 0 {
		t.Fatalf("an ambiguous customer must not be written (nor the free-text note): %+v", w)
	}
	res := toolResult(t, reqs())
	if !strings.Contains(res, `"ambiguous":true`) || !strings.Contains(res, "nothing was saved") {
		t.Fatalf("want candidates + not saved, got %s", res)
	}
}

func TestRememberCustomerFact_RejectsWhatItCannotFile(t *testing.T) {
	cases := map[string]struct {
		cands []CustomerRef
		args  string
		want  string
	}{
		"walk-in name":      {[]CustomerRef{{Name: "王五"}}, `{"customer":"王五","attribute":"taste","value":"少糖","quote":"王五要少糖"}`, `"saved":false`},
		"unknown attribute": {[]CustomerRef{{ID: zhangID, Name: "张三"}}, `{"customer":"张三","attribute":"birthday","value":"5月","quote":"张三5月生日"}`, "unknown attribute"},
		"no value":          {[]CustomerRef{{ID: zhangID, Name: "张三"}}, `{"customer":"张三","attribute":"taste","value":" ","quote":"张三"}`, "required"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			mc := &recordingMemory{}
			o, _ := factOrchestrator(t, c.cands, mc)
			var remembered bool
			res := o.dispatch(context.Background(), uuid.New(), factCall(c.args).ToolCalls[0], &remembered)
			if !strings.Contains(res.Content, c.want) {
				t.Fatalf("got %s, want %s", res.Content, c.want)
			}
			if w := mc.settled(); len(w) != 0 {
				t.Fatalf("nothing may be written: %+v", w)
			}
		})
	}
}

func TestRememberCustomerFact_MultiValuedSlotIsMarkedSo(t *testing.T) {
	mc := &recordingMemory{}
	o, _ := factOrchestrator(t, []CustomerRef{{ID: liID, Name: "李四"}}, mc)
	var remembered bool
	o.dispatch(context.Background(), uuid.New(),
		factCall(`{"customer":"李四","attribute":"allergy","value":"芒果","quote":"李四也对芒果过敏"}`).ToolCalls[0], &remembered)
	w := mc.settled()
	if len(w) != 1 || w[0].meta[MemorySlotSingleKey] != false || w[0].meta[MemorySlotKey] != "allergy" {
		t.Fatalf("writes = %+v", w)
	}
}

func TestChat_WithoutTheToolTheStatementIsStillRemembered(t *testing.T) {
	mc := &recordingMemory{}
	o, _ := factOrchestrator(t, nil, mc, llmclient.Message{Role: "assistant", Content: "好的"})
	if _, err := o.Chat(context.Background(), ChatInput{TenantID: uuid.New(), UserMessage: "周末不安排送货"}); err != nil {
		t.Fatal(err)
	}
	if w := mc.settled(); len(w) != 1 || w[0].content != "周末不安排送货" {
		t.Fatalf("fallback free-text write lost: %+v", w)
	}
}

func TestToolDefs_RememberCustomerFactOnlyWithMemory(t *testing.T) {
	has := func(defs []llmclient.Tool) bool {
		for _, d := range defs {
			if d.Function.Name == rememberCustomerFactTool {
				return true
			}
		}
		return false
	}
	if has((&Orchestrator{}).toolDefs()) {
		t.Fatal("without memory there is nothing to save to")
	}
	on := (&Orchestrator{memory: &recordingMemory{}}).toolDefs()
	if !has(on) || len(on) != len(ToolDefs())+1 {
		t.Fatalf("with memory: the Registry's tools plus remember_customer_fact, got %d", len(on))
	}
	for _, s := range customerSlots {
		if !strings.Contains(string(rememberCustomerFactDef().Function.Parameters), `"`+s.ID+`"`) {
			t.Errorf("attribute enum lacks %s", s.ID)
		}
	}
}

func TestAugmentWithCustomerMemory_LabelsTheSlot(t *testing.T) {
	zhang := CustomerSubject(zhangID)
	h := hit("z1", "张三以后改用微信付款", 0.8, zhang, false)
	h.Metadata["metadata"].(map[string]any)[MemorySlotKey] = "payment_method"
	mc := &filteringMemory{own: []memorusclient.Memory{h}}
	out := AugmentWithCustomerMemory(mc, context.Background(), "u", "接待张三要注意什么", &CustomerRef{ID: zhangID, Name: "张三"})
	if !strings.Contains(out, "（客户 张三·付款方式）张三以后改用微信付款") {
		t.Fatalf("slot label missing:\n%s", out)
	}
}
