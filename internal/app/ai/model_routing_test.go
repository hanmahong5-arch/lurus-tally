package ai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"
	appai "github.com/hanmahong5-arch/lurus-tally/internal/app/ai"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/llmclient"
)

// modelRecorder is a fake chat-completions endpoint that records the model of
// every request and answers with a plain final message.
type modelRecorder struct {
	mu     sync.Mutex
	models []string
}

func (m *modelRecorder) handler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model string `json:"model"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	m.mu.Lock()
	m.models = append(m.models, req.Model)
	m.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"id":"c","choices":[{"index":0,"message":{"role":"assistant","content":"好的"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
}

func newRoutingOrchestrator(t *testing.T, rec *modelRecorder) *appai.Orchestrator {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(rec.handler))
	t.Cleanup(srv.Close)
	cl, err := llmclient.New(llmclient.Config{BaseURL: srv.URL, APIKey: "k", DefaultModel: "test-model"})
	if err != nil {
		t.Fatal(err)
	}
	reg := appai.NewRegistry(&mockProductRepo{}, &mockStockRepo{}, &mockSaleRepo{}, nil)
	return appai.NewOrchestrator(cl, reg, newMockPlanStore(), "default-model")
}

func TestOrchestrator_NoRouter_UsesDefaultModel(t *testing.T) {
	rec := &modelRecorder{}
	o := newRoutingOrchestrator(t, rec)
	if _, err := o.Chat(context.Background(), appai.ChatInput{TenantID: uuid.New(), UserMessage: "你好"}); err != nil {
		t.Fatal(err)
	}
	if len(rec.models) != 1 || rec.models[0] != "default-model" {
		t.Fatalf("models=%v want [default-model]", rec.models)
	}
}

func TestOrchestrator_RouterWithoutChatRoute_UsesDefaultModel(t *testing.T) {
	rec := &modelRecorder{}
	o := newRoutingOrchestrator(t, rec).
		WithModelRouter(llmclient.NewRouter(map[llmclient.Task]string{llmclient.TaskExtract: "small"}))
	if _, err := o.Chat(context.Background(), appai.ChatInput{TenantID: uuid.New(), UserMessage: "你好"}); err != nil {
		t.Fatal(err)
	}
	if rec.models[0] != "default-model" {
		t.Fatalf("models=%v want default-model", rec.models)
	}
}

func TestOrchestrator_RouterChatRoute_UsesRoutedModel(t *testing.T) {
	rec := &modelRecorder{}
	o := newRoutingOrchestrator(t, rec).
		WithModelRouter(llmclient.NewRouter(map[llmclient.Task]string{llmclient.TaskChat: "chat-model"}))
	if _, err := o.Chat(context.Background(), appai.ChatInput{TenantID: uuid.New(), UserMessage: "你好"}); err != nil {
		t.Fatal(err)
	}
	if rec.models[0] != "chat-model" {
		t.Fatalf("models=%v want chat-model", rec.models)
	}
}
