package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/llmclient"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/memorusclient"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// brokenMemory fails (or panics on) every call.
type brokenMemory struct{ panics bool }

func (m brokenMemory) Search(context.Context, string, string, int) ([]memorusclient.Memory, error) {
	return nil, errors.New("memorus down")
}

func (m brokenMemory) Add(context.Context, string, string, map[string]any) (*memorusclient.Memory, error) {
	if m.panics {
		panic("boom")
	}
	return nil, errors.New("memorus down")
}

func memOpCount(op, outcome string) float64 {
	return testutil.ToFloat64(memoryOps.WithLabelValues(op, outcome))
}

// waitCount polls the counter the async write goroutine bumps.
func waitCount(t *testing.T, op, outcome string, want float64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for memOpCount(op, outcome) < want {
		if time.Now().After(deadline) {
			t.Fatalf("%s/%s = %v, want %v", op, outcome, memOpCount(op, outcome), want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestMemoryOps_FailuresAreCounted(t *testing.T) {
	before := memOpCount(memOpWrite, "error")
	if !AsyncWriteMemory(brokenMemory{}, "u", "张三要发票", nil) {
		t.Fatal("a write with content must start")
	}
	waitCount(t, memOpWrite, "error", before+1)

	AsyncWriteMemory(brokenMemory{panics: true}, "u", "张三要发票", nil)
	waitCount(t, memOpWrite, "error", before+2)

	if AsyncWriteMemory(brokenMemory{}, "u", "", nil) {
		t.Fatal("nothing to remember starts no write")
	}

	recallBefore := memOpCount(memOpRecall, "error")
	if got := AugmentWithCustomerMemory(brokenMemory{}, context.Background(), "u", "张三要发票", nil); got != "张三要发票" {
		t.Fatalf("a failed recall must leave the message as is, got %q", got)
	}
	if memOpCount(memOpRecall, "error") != recallBefore+1 {
		t.Fatal("failed recall not counted")
	}

	okBefore := memOpCount(memOpWrite, "ok")
	AsyncWriteMemory(&recordingMemory{}, "u", "张三要发票", nil)
	waitCount(t, memOpWrite, "ok", okBefore+1)
}

func TestRememberCustomerFact_FailedWriteIsCounted(t *testing.T) {
	before := memOpCount(memOpFactWrite, "error")
	o, _ := factOrchestrator(t, []CustomerRef{{ID: zhangID, Name: "张三"}}, brokenMemory{})
	var remembered bool
	o.dispatch(context.Background(), uuid.New(), factCall(zhangPaysWeChat).ToolCalls[0], &remembered)
	if memOpCount(memOpFactWrite, "error") != before+1 {
		t.Fatal("failed fact write not counted")
	}
}

// One line per turn says what happened, without the message text.
func TestChat_LogsOneLinePerTurn(t *testing.T) {
	for _, m := range chatModes {
		var buf bytes.Buffer
		mc := &recordingMemory{}
		o, _ := factOrchestrator(t, nil, mc,
			llmclient.Message{Role: "assistant", ToolCalls: []llmclient.ToolCall{{
				ID: "t1", Type: "function", Function: llmclient.ToolCallFunction{Name: "get_stock_summary", Arguments: `{}`},
			}}},
			llmclient.Message{Role: "assistant", Content: "好的"})
		o = o.WithCustomerResolver(&aliasRepo{all: []CustomerRef{{ID: liID, Name: "李四"}}})
		o.log = slog.New(slog.NewJSONHandler(&buf, nil))
		if _, err := m.run(o, context.Background(), ChatInput{TenantID: uuid.New(), UserMessage: "老李以后都要塑料袋"}); err != nil {
			t.Fatal(err)
		}
		var line map[string]any
		if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
			t.Fatalf("%s: want exactly one JSON line, got %q", m.name, buf.String())
		}
		tools, _ := line["tools"].([]any)
		if line["msg"] != "ai turn" || len(tools) != 1 || tools[0] != "get_stock_summary" ||
			line["customer"] != "alias" || line["note_queued"] != true || line["fact_tool"] != false {
			t.Errorf("%s: line = %v", m.name, line)
		}
		if bytes.Contains(buf.Bytes(), []byte("塑料袋")) {
			t.Errorf("%s: the log line must not carry the message text", m.name)
		}
		mc.settled()
	}
}
