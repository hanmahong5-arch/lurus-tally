package ai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	appai "github.com/hanmahong5-arch/lurus-tally/internal/app/ai"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/llmclient"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/llmgateway"
)

// billOracleSink counts the usage reports fanned out by llmgateway.RecordUsage.
// It is the "counter" side of the double-billing oracle: one completed LLM call
// must produce exactly one usage report.
type billOracleSink struct{ n atomic.Int64 }

func (s *billOracleSink) Record(_ context.Context, _ string, _, _ int) { s.n.Add(1) }

// TestStreamChat_SimpleQA_BillsExactlyOnce is the billing oracle for the
// double-charge defect in Orchestrator.StreamChat.
//
// A simple (no-tool) question must cost exactly ONE LLM API call and emit
// exactly ONE usage report — not two. Before the fix, StreamChat called the
// non-streaming Chat to detect tool calls and then, on the no-tool path,
// re-inferred the same prompt with Stream, so every simple Q&A was billed
// twice (2 HTTP calls to the gateway, 2 llmgateway.RecordUsage fan-outs).
//
// The mock LLM here is a real *llmclient.Client pointed at an httptest server
// (Client has unexported fields and cannot be faked directly); the httptest
// request counter is the ground-truth "number of LLM API calls", and the
// installed UsageSink is the ground-truth "number of usage reports".
func TestStreamChat_SimpleQA_BillsExactlyOnce(t *testing.T) {
	var httpCalls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpCalls.Add(1)
		var req llmclient.ChatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Stream {
			// A streamed answer plus a terminal usage chunk. Reaching this
			// branch at all means the caller issued a *second* inference.
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: " + `{"choices":[{"delta":{"content":"库存充足"},"finish_reason":"stop"}]}` + "\n\n"))
			_, _ = w.Write([]byte("data: " + `{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}` + "\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
			return
		}
		// Non-stream request with no tool calls: this response IS the final answer.
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(llmclient.ChatResponse{
			Choices: []llmclient.Choice{{Message: llmclient.Message{Content: "库存充足，无需补货。"}}},
			Usage:   llmclient.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		})
	}))
	defer srv.Close()

	sink := &billOracleSink{}
	llmgateway.SetUsageSink(sink)
	usageBefore := sink.n.Load()

	cli, err := llmclient.New(llmclient.Config{BaseURL: srv.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("llmclient.New: %v", err)
	}
	// registry / planStore are unused on the no-tool path.
	o := appai.NewOrchestrator(cli, nil, nil, "test-model")

	var chunks []string
	out, err := o.StreamChat(context.Background(), appai.ChatInput{
		TenantID:    uuid.New(),
		UserMessage: "现在库存怎么样",
	}, func(s string) { chunks = append(chunks, s) })
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}

	gotCalls := httpCalls.Load()
	gotUsage := sink.n.Load() - usageBefore

	if gotCalls != 1 {
		t.Errorf("LLM API calls = %d, want exactly 1 (a simple Q&A must not be inferred twice)", gotCalls)
	}
	if gotUsage != 1 {
		t.Errorf("usage reports = %d, want exactly 1 (a simple Q&A must not be billed twice)", gotUsage)
	}
	if strings.TrimSpace(out.AssistantText) == "" {
		t.Error("AssistantText is empty; the single-call answer must reach the caller")
	}
	if len(chunks) == 0 {
		t.Error("onChunk was never called; the answer must still stream to the client")
	}
}
