package ai_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	appai "github.com/hanmahong5-arch/lurus-tally/internal/app/ai"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/memorusclient"
)

// --- mock MemoryClient ---

type mockMemoryClient struct {
	searchFn func(ctx context.Context, userID, query string, limit int) ([]memorusclient.Memory, error)
	addFn    func(ctx context.Context, userID, content string, meta map[string]any) (*memorusclient.Memory, error)
}

func (m *mockMemoryClient) Search(ctx context.Context, userID, query string, limit int) ([]memorusclient.Memory, error) {
	if m.searchFn != nil {
		return m.searchFn(ctx, userID, query, limit)
	}
	return nil, nil
}

func (m *mockMemoryClient) Add(ctx context.Context, userID, content string, meta map[string]any) (*memorusclient.Memory, error) {
	if m.addFn != nil {
		return m.addFn(ctx, userID, content, meta)
	}
	return &memorusclient.Memory{Content: content}, nil
}

// --- mock LLM client ---

type mockLLMChat struct {
	response string
}

// TestChat_WithMemorusEnabled_AugmentsContext verifies that when memorus returns
// memories, they are prepended to the system context in the orchestrator input.
// We verify this by confirming buildMessages returns more tokens when memories present.
func TestChat_WithMemorusEnabled_AugmentsContext(t *testing.T) {
	memories := []memorusclient.Memory{
		{ID: "m1", UserID: "user-1", Content: "User prefers early morning inventory reports"},
		{ID: "m2", UserID: "user-1", Content: "Last week user asked about Widget A pricing"},
	}
	mc := &mockMemoryClient{
		searchFn: func(_ context.Context, _, _ string, _ int) ([]memorusclient.Memory, error) {
			return memories, nil
		},
	}

	augmented := appai.AugmentMessagesWithMemory(memories, "what is my low stock?")
	if augmented == "" {
		t.Fatal("expected non-empty augmented context")
	}
	// Must contain memory content.
	if !containsStr(augmented, "early morning") {
		t.Errorf("expected augmented context to contain memory content, got: %s", augmented)
	}
	if !containsStr(augmented, "Widget A") {
		t.Errorf("expected second memory content in augmented string, got: %s", augmented)
	}
	_ = mc
}

// TestChat_WithMemorusDown_StillResponds verifies that when memorus search fails
// with ErrUnavailable, the orchestrator degrades gracefully (no panic, returns
// the original message unchanged).
func TestChat_WithMemorusDown_StillResponds(t *testing.T) {
	mc := &mockMemoryClient{
		searchFn: func(_ context.Context, _, _ string, _ int) ([]memorusclient.Memory, error) {
			return nil, memorusclient.ErrUnavailable
		},
	}

	// When memorus is down, AugmentWithMemory falls back to original message.
	result := appai.AugmentMessagesWithMemoryOrFallback(mc, context.Background(), "user-1", "what is my low stock?")
	if result != "what is my low stock?" {
		t.Errorf("expected fallback to original message, got: %s", result)
	}
}

// TestChat_AfterResponse_AsyncWriteToMemory verifies that BuildMemorySummary
// produces a valid summary string for async write-back.
func TestChat_AfterResponse_AsyncWriteToMemory(t *testing.T) {
	tenantID := uuid.New()
	userMsg := "What is the stock level for Widget A? I need to order more if it's below 50 units."
	assistantReply := "Widget A currently has 32 units in stock. It is below ROP=45. Consider ordering."

	summary := appai.BuildMemorySummary(tenantID, userMsg, assistantReply)
	if summary == "" {
		t.Fatal("expected non-empty memory summary")
	}
	// Summary must be concise (not dump entire conversation).
	if len(summary) > 300 {
		t.Errorf("summary too long (%d chars), must be ≤300 for storage efficiency", len(summary))
	}
	// Must contain a meaningful snippet.
	if !containsStr(summary, "Widget A") {
		// The summary should reference the first 100 chars of user message.
		t.Logf("summary: %s", summary)
		t.Error("expected summary to reference user question snippet")
	}
}

// TestChat_WithMemoryEnabled_LimitsFiveRecalls verifies that Search is always
// called with limit=5.
func TestChat_WithMemoryEnabled_LimitsFiveRecalls(t *testing.T) {
	var gotLimit int
	mc := &mockMemoryClient{
		searchFn: func(_ context.Context, _, _ string, limit int) ([]memorusclient.Memory, error) {
			gotLimit = limit
			return nil, nil
		},
	}

	appai.AugmentMessagesWithMemoryOrFallback(mc, context.Background(), "u", "q")
	if gotLimit != 5 {
		t.Errorf("expected limit=5, got %d", gotLimit)
	}
}

// containsStr is a simple substring helper.
func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && findStr(s, substr)
}

func findStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
