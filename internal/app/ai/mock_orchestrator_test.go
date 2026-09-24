package ai_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	appai "github.com/hanmahong5-arch/lurus-tally/internal/app/ai"
)

// TestMockOrchestrator_StreamChat_EmitsCannedReply verifies the mock streams
// the configured "service not configured" message via onChunk.
func TestMockOrchestrator_StreamChat_EmitsCannedReply(t *testing.T) {
	m := appai.NewMockOrchestrator()

	var collected strings.Builder
	out, err := m.StreamChat(context.Background(), appai.ChatInput{
		TenantID:    uuid.New(),
		UserMessage: "hello",
	}, func(chunk string) {
		collected.WriteString(chunk)
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out == nil || out.AssistantText == "" {
		t.Fatalf("expected non-empty assistant text, got %+v", out)
	}
	if collected.Len() == 0 {
		t.Fatalf("expected onChunk to be called, got nothing")
	}
	if !strings.Contains(collected.String(), "AI 服务未配置") {
		t.Errorf("expected canned reply in chunks, got: %q", collected.String())
	}
}

// TestMockOrchestrator_StreamChat_RespectsContextCancel verifies the mock
// stops streaming when the context is cancelled (UI Drawer close → abort).
func TestMockOrchestrator_StreamChat_RespectsContextCancel(t *testing.T) {
	m := appai.NewMockOrchestrator()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before first chunk

	var calls int
	_, err := m.StreamChat(ctx, appai.ChatInput{UserMessage: "x"}, func(_ string) {
		calls++
	})
	if err == nil {
		t.Errorf("expected ctx.Err on cancel, got nil")
	}
	if calls != 0 {
		t.Errorf("expected no chunks on pre-cancel, got %d", calls)
	}
}

// TestMockOrchestrator_PlanOps_AlwaysNotFound documents the mock contract:
// plan ops always return ErrPlanNotFound because no plans are ever stored.
func TestMockOrchestrator_PlanOps_AlwaysNotFound(t *testing.T) {
	m := appai.NewMockOrchestrator()
	if _, err := m.ConfirmPlan(context.Background(), uuid.New(), uuid.New()); err == nil {
		t.Errorf("ConfirmPlan: expected ErrPlanNotFound, got nil")
	}
	if err := m.CancelPlan(context.Background(), uuid.New(), uuid.New()); err == nil {
		t.Errorf("CancelPlan: expected ErrPlanNotFound, got nil")
	}
}

// TestMockOrchestrator_StreamChat_ChunksMultipleRunes ensures CJK characters
// are emitted one at a time, not as a single payload (typewriter effect).
func TestMockOrchestrator_StreamChat_ChunksMultipleRunes(t *testing.T) {
	m := appai.NewMockOrchestrator()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var chunks int
	_, _ = m.StreamChat(ctx, appai.ChatInput{}, func(_ string) {
		chunks++
	})
	if chunks < 2 {
		t.Errorf("expected multiple chunks for canned reply, got %d", chunks)
	}
}

// TestBuildMessages_WithPageContext_InjectsSystemHint verifies the page hint
// becomes a second system message, separate from the cache-friendly base prompt.
func TestBuildMessages_WithPageContext_InjectsSystemHint(t *testing.T) {
	got := appai.BuildMessagesForTest(appai.ChatInput{
		UserMessage: "what's low?",
		PageContext: "/stock",
	})

	var systemCount int
	var hintFound bool
	for _, m := range got {
		if m.Role == "system" {
			systemCount++
			if strings.Contains(m.Content, "/stock") {
				hintFound = true
			}
		}
	}
	if systemCount != 2 {
		t.Errorf("expected 2 system messages with PageContext set, got %d", systemCount)
	}
	if !hintFound {
		t.Errorf("expected /stock hint in a system message; got: %+v", got)
	}
}

// TestBuildMessages_WithoutPageContext_OmitsHint verifies that when PageContext
// is empty the second system message is not emitted (preserving cache hits).
func TestBuildMessages_WithoutPageContext_OmitsHint(t *testing.T) {
	got := appai.BuildMessagesForTest(appai.ChatInput{
		UserMessage: "hi",
	})
	var systemCount int
	for _, m := range got {
		if m.Role == "system" {
			systemCount++
		}
	}
	if systemCount != 1 {
		t.Errorf("expected exactly 1 system message when PageContext empty, got %d", systemCount)
	}
}

// TestBuildMessages_PageContextWhitespaceOnly_OmitsHint covers the trim path.
func TestBuildMessages_PageContextWhitespaceOnly_OmitsHint(t *testing.T) {
	got := appai.BuildMessagesForTest(appai.ChatInput{
		UserMessage: "hi",
		PageContext: "   ",
	})
	var systemCount int
	for _, m := range got {
		if m.Role == "system" {
			systemCount++
		}
	}
	if systemCount != 1 {
		t.Errorf("expected exactly 1 system message for whitespace PageContext, got %d", systemCount)
	}
}
