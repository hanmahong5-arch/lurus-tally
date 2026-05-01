// Package ai — memory integration helpers for the AI Drawer.
//
// These functions implement the recall+write pattern:
//   - Before sending to LLM: search memorus for relevant past context
//   - After LLM responds: asynchronously write a summary back to memorus
//
// Degradation contract: if memorus is unavailable, both paths silently skip —
// the AI Drawer continues working without memory context.
package ai

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/memorusclient"
)

// MemoryClient is the interface the orchestrator uses for memory operations.
// memorusclient.Client satisfies this interface; tests supply a mock.
type MemoryClient interface {
	Search(ctx context.Context, userID, query string, limit int) ([]memorusclient.Memory, error)
	Add(ctx context.Context, userID, content string, meta map[string]any) (*memorusclient.Memory, error)
}

const memorySearchLimit = 5

// AugmentMessagesWithMemory formats retrieved memories into a context prefix
// that is prepended to the user message before sending to the LLM.
//
// The returned string is the user message possibly prefixed with a
// "--- 历史记忆 ---" block containing relevant past context.
func AugmentMessagesWithMemory(memories []memorusclient.Memory, userMessage string) string {
	if len(memories) == 0 {
		return userMessage
	}
	var b strings.Builder
	b.WriteString("--- 历史记忆（仅供参考）---\n")
	for _, m := range memories {
		b.WriteString("• ")
		b.WriteString(m.Content)
		b.WriteByte('\n')
	}
	b.WriteString("---\n")
	b.WriteString(userMessage)
	return b.String()
}

// AugmentMessagesWithMemoryOrFallback searches memorus for memories relevant
// to userMessage and returns the augmented message. On any error (including
// ErrUnavailable) it returns the original userMessage unchanged.
func AugmentMessagesWithMemoryOrFallback(mc MemoryClient, ctx context.Context, userID, userMessage string) string {
	if mc == nil {
		return userMessage
	}
	memories, err := mc.Search(ctx, userID, userMessage, memorySearchLimit)
	if err != nil || len(memories) == 0 {
		return userMessage
	}
	return AugmentMessagesWithMemory(memories, userMessage)
}

// BuildMemorySummary builds a short, storage-efficient summary of a single
// conversation turn for async write-back to memorus.
//
// Summary format: "用户问了：<first 100 chars of question>"
// This is intentionally minimal to keep storage low and stay privacy-safe.
func BuildMemorySummary(tenantID uuid.UUID, userMsg, _ string) string {
	snippet := userMsg
	if len(snippet) > 100 {
		snippet = snippet[:100]
	}
	return fmt.Sprintf("用户问了：%s (tenant=%s)", snippet, tenantID.String())
}

// AsyncWriteMemory fires a goroutine to write a memory summary to memorus.
// It is a no-op when mc is nil. Panics in the goroutine are recovered silently
// so a memorus failure can never crash the server.
func AsyncWriteMemory(mc MemoryClient, userID string, summary string, meta map[string]any) {
	if mc == nil {
		return
	}
	go func() {
		defer func() { _ = recover() }()
		// Use a background context so the HTTP response lifecycle does not cancel this.
		ctx := context.Background()
		_, _ = mc.Add(ctx, userID, summary, meta)
	}()
}
