package ai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	appai "github.com/hanmahong5-arch/lurus-tally/internal/app/ai"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/llmclient"
)

// draftLLM answers the chat loop with one purchase-draft tool call and then a
// final message; a request on the extract model gets extractReply.
type draftLLM struct {
	mu           sync.Mutex
	extractCalls int
	extractReply string
	draftArgs    string
}

func (d *draftLLM) handler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model    string `json:"model"`
		Messages []struct {
			Role string `json:"role"`
		} `json:"messages"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	w.Header().Set("Content-Type", "application/json")
	if req.Model == "small" {
		d.mu.Lock()
		d.extractCalls++
		d.mu.Unlock()
		b, _ := json.Marshal(map[string]interface{}{"id": "e", "choices": []interface{}{map[string]interface{}{
			"index": 0, "message": map[string]string{"role": "assistant", "content": d.extractReply}, "finish_reason": "stop"}}})
		_, _ = w.Write(b)
		return
	}
	last := req.Messages[len(req.Messages)-1].Role
	if last == "tool" {
		_, _ = w.Write([]byte(`{"id":"c","choices":[{"index":0,"message":{"role":"assistant","content":"已生成"},"finish_reason":"stop"}]}`))
		return
	}
	b, _ := json.Marshal(map[string]interface{}{"id": "c", "choices": []interface{}{map[string]interface{}{
		"index": 0, "finish_reason": "tool_calls", "message": map[string]interface{}{
			"role": "assistant", "content": "",
			"tool_calls": []interface{}{map[string]interface{}{
				"id": "t1", "type": "function",
				"function": map[string]string{"name": "propose_create_purchase_draft", "arguments": d.draftArgs},
			}},
		}}}})
	_, _ = w.Write(b)
}

func runDraftTurn(t *testing.T, d *draftLLM, router *llmclient.Router, withExtractor bool, history []llmclient.Message, said string) []map[string]interface{} {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(d.handler))
	t.Cleanup(srv.Close)
	cl, err := llmclient.New(llmclient.Config{BaseURL: srv.URL, APIKey: "k", DefaultModel: "test-model"})
	if err != nil {
		t.Fatal(err)
	}
	reg := appai.NewRegistry(&mockProductRepo{}, &mockStockRepo{}, &mockSaleRepo{}, nil)
	o := appai.NewOrchestrator(cl, reg, newMockPlanStore(), "default-model")
	if router != nil {
		o.WithModelRouter(router)
	}
	if withExtractor {
		o.WithExtractor(appai.NewExtractor(cl, router, "default-model"))
	}
	out, err := o.Chat(context.Background(), appai.ChatInput{TenantID: uuid.New(), History: history, UserMessage: said})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Plans) != 1 {
		t.Fatalf("plans=%d", len(out.Plans))
	}
	flags, _ := out.Plans[0].Payload["flags"].([]map[string]interface{})
	return flags
}

func TestOrchestrator_Extractor_DisagreementFlagsDraftLine(t *testing.T) {
	d := &draftLLM{
		draftArgs:    `{"items":[{"product_name":"樱花","qty":20},{"product_name":"桃树","qty":10}]}`,
		extractReply: `{"lines":[{"product_name":"樱花","qty":"10"},{"product_name":"桃树","qty":"10"}]}`,
	}
	router := llmclient.NewRouter(map[llmclient.Task]string{llmclient.TaskExtract: "small"})
	// The draft matches the words (樱花二十棵 桃树十棵); only the second
	// reading disagrees about 樱花.
	flags := runDraftTurn(t, d, router, true, nil, "进樱花二十棵桃树十棵")
	if d.extractCalls != 1 {
		t.Fatalf("extract calls=%d want 1", d.extractCalls)
	}
	if len(flags) != 1 || flags[0]["line"] != 0 || flags[0]["code"] != appai.FlagQtyMismatch {
		t.Fatalf("flags=%+v", flags)
	}
}

func TestOrchestrator_NoExtractor_NoExtraCallNoFlags(t *testing.T) {
	d := &draftLLM{
		draftArgs:    `{"items":[{"product_name":"樱花","qty":20},{"product_name":"桃树","qty":10}]}`,
		extractReply: `{"lines":[{"product_name":"樱花","qty":"10"}]}`,
	}
	flags := runDraftTurn(t, d, nil, false, nil, "进樱花二十棵桃树十棵")
	if d.extractCalls != 0 || len(flags) != 0 {
		t.Fatalf("calls=%d flags=%+v", d.extractCalls, flags)
	}
}

func TestOrchestrator_Extractor_SkippedWhenNothingDictated(t *testing.T) {
	d := &draftLLM{
		draftArgs:    `{"items":[{"product_name":"樱花","qty":37}]}`,
		extractReply: `{"lines":[{"product_name":"樱花","qty":"1"}]}`,
	}
	router := llmclient.NewRouter(map[llmclient.Task]string{llmclient.TaskExtract: "small"})
	flags := runDraftTurn(t, d, router, true, nil, "帮我补货")
	if d.extractCalls != 0 || len(flags) != 0 {
		t.Fatalf("calls=%d flags=%+v", d.extractCalls, flags)
	}
}

func TestOrchestrator_MultiTurn_QuantityFromEarlierTurnIsChecked(t *testing.T) {
	d := &draftLLM{draftArgs: `{"items":[{"product_name":"樱花","qty":20}]}`}
	hist := []llmclient.Message{{Role: "user", Content: "樱花十棵"}, {Role: "assistant", Content: "好的，要建单吗？"}}
	flags := runDraftTurn(t, d, nil, false, hist, "对，建吧")
	if len(flags) != 1 || flags[0]["code"] != appai.FlagQtyMismatch {
		t.Fatalf("a draft contradicting an earlier turn must be flagged: %+v", flags)
	}
	d2 := &draftLLM{draftArgs: `{"items":[{"product_name":"樱花","qty":10}]}`}
	if flags := runDraftTurn(t, d2, nil, false, hist, "对，建吧"); len(flags) != 0 {
		t.Fatalf("a draft matching an earlier turn must pass: %+v", flags)
	}
	if !strings.Contains(hist[0].Content.(string), "樱花") {
		t.Fatal("history must stay untouched")
	}
}
