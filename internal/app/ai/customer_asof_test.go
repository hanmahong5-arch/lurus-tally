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

func TestParseAsOf_EndOfTheNamedPeriodInShopTime(t *testing.T) {
	now := time.Date(2026, 9, 26, 10, 0, 0, 0, shopZone)
	cases := map[string]string{
		"2026-03-15":           "2026-03-15T23:59:59.999999999+08:00",
		"2026-03":              "2026-03-31T23:59:59.999999999+08:00",
		"03":                   "2026-03-31T23:59:59.999999999+08:00",
		"3月":                   "2026-03-31T23:59:59.999999999+08:00",
		"11":                   "2025-11-30T23:59:59.999999999+08:00", // November has not come yet this year
		"2026-02-01T08:00:00Z": "2026-02-01T08:00:00Z",
	}
	for in, want := range cases {
		got, err := parseAsOf(in, now)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		w, _ := time.Parse(time.RFC3339Nano, want)
		if !got.Equal(w) {
			t.Errorf("%q = %s, want %s", in, got.Format(time.RFC3339Nano), want)
		}
	}
	for _, bad := range []string{"", "上个月", "13", "2026/03"} {
		if _, err := parseAsOf(bad, now); err == nil {
			t.Errorf("%q must be rejected, not guessed", bad)
		}
	}
}

// asOfMemory answers SearchAsOf from a fixed script and records the call.
type asOfMemory struct {
	recordingMemory
	gotAt   time.Time
	gotMeta map[string]string
	answer  []memorusclient.Memory
}

func (m *asOfMemory) SearchAsOf(_ context.Context, _, _ string, _ int, metaEq map[string]string, asOf time.Time) ([]memorusclient.Memory, error) {
	m.gotAt, m.gotMeta = asOf, metaEq
	return m.answer, nil
}

func TestRecallAsOf_ReadsTheCustomersNotesAtThatTime(t *testing.T) {
	mc := &asOfMemory{answer: []memorusclient.Memory{{Content: "张三只收现金"}}}
	call := llmclient.Message{Role: "assistant", ToolCalls: []llmclient.ToolCall{{
		ID: "a1", Type: "function",
		Function: llmclient.ToolCallFunction{Name: recallAsOfTool, Arguments: `{"customer":"张三","as_of":"2026-03"}`},
	}}}
	o, reqs := factOrchestrator(t, []CustomerRef{{ID: zhangID, Name: "张三"}}, mc,
		call, llmclient.Message{Role: "assistant", Content: "三月时张三只收现金"})
	if _, err := o.Chat(context.Background(), ChatInput{TenantID: uuid.New(), UserMessage: "三月时张三怎么付款"}); err != nil {
		t.Fatal(err)
	}
	if mc.gotMeta[MemorySubjectKey] != CustomerSubject(zhangID) {
		t.Errorf("must search only this customer's notes: %v", mc.gotMeta)
	}
	want := time.Date(2026, 4, 1, 0, 0, 0, 0, shopZone).Add(-time.Nanosecond)
	if !mc.gotAt.Equal(want) {
		t.Errorf("as_of = %s, want end of March %s", mc.gotAt, want)
	}
	res := toolResult(t, reqs())
	if !strings.Contains(res, "张三只收现金") || !strings.Contains(res, `"as_of":"2026-03-31 23:59"`) {
		t.Errorf("tool result: %s", res)
	}
	var offered bool
	for _, tool := range reqs()[0].Tools {
		offered = offered || tool.Function.Name == recallAsOfTool
	}
	if !offered {
		t.Error("the tool must be offered when the memory client can search by time")
	}
}

func TestRecallAsOf_NotOfferedWithoutTimeSearch(t *testing.T) {
	o, reqs := factOrchestrator(t, nil, &recordingMemory{}, llmclient.Message{Role: "assistant", Content: "好"})
	if _, err := o.Chat(context.Background(), ChatInput{TenantID: uuid.New(), UserMessage: "你好"}); err != nil {
		t.Fatal(err)
	}
	for _, tool := range reqs()[0].Tools {
		if tool.Function.Name == recallAsOfTool {
			t.Fatal("a client without SearchAsOf must not advertise the tool")
		}
	}
}
