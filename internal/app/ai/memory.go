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
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hanmahong5-arch/lurus-tally/internal/pkg/memorusclient"
)

// asyncMemoryWriteTimeout bounds a fire-and-forget memory write. It is detached
// from the request context (which ends when the HTTP response is sent) but must
// still be bounded so a hung memorus connection cannot pin the goroutine and its
// connection indefinitely.
const asyncMemoryWriteTimeout = 5 * time.Second

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
	if err != nil {
		return userMessage
	}
	memories = relevantMemories(memories)
	if len(memories) == 0 {
		return userMessage
	}
	return AugmentMessagesWithMemory(memories, userMessage)
}

// memoryRelativeFloor drops hits scoring below this fraction of the best hit.
// memorus always returns its top-k, relevant or not; unfiltered, 16 of the 20
// lines a replayed scenario injected into the prompt were unrelated to the
// question. 0.5 was fixed before measuring, not tuned on the scenario.
const memoryRelativeFloor = 0.5

// relevantMemories keeps what is worth putting in front of the LLM: not an
// older value that a later statement replaced (memorus marks it
// `superseded_by` and keeps it only as history), not an empty hit, and not a
// hit far below the best one.
func relevantMemories(ms []memorusclient.Memory) []memorusclient.Memory {
	var top float64
	for _, m := range ms {
		if m.Score > top {
			top = m.Score
		}
	}
	out := make([]memorusclient.Memory, 0, len(ms))
	for _, m := range ms {
		if strings.TrimSpace(m.Content) == "" {
			continue
		}
		if s, ok := m.Metadata["superseded_by"].(string); ok && s != "" {
			continue
		}
		if top > 0 && m.Score < memoryRelativeFloor*top {
			continue
		}
		out = append(out, m)
	}
	return out
}

// memoryTextMaxRunes bounds one remembered statement. Counted in runes: the
// old byte cut split Chinese characters and wrote invalid UTF-8.
const memoryTextMaxRunes = 500

// BuildMemorySummary returns what to remember from one chat turn: the user's
// own words, which is where the business facts live ("张三要求送货前一天电话
// 确认", "矿泉水进价调整为每箱 35 元"). memorus deduplicates repeats and lets a
// changed value supersede the old one, so the text is stored as said.
//
// Returns "" (nothing to remember) for a pure question — it states no fact.
// The tenant goes into the write's metadata, not into the text: in the text it
// polluted matching and was echoed into every prompt.
func BuildMemorySummary(_ uuid.UUID, userMsg, _ string) string {
	text := strings.TrimSpace(userMsg)
	if text == "" || isPureQuestion(text) {
		return ""
	}
	if r := []rune(text); len(r) > memoryTextMaxRunes {
		text = string(r[:memoryTextMaxRunes])
	}
	return text
}

func isPureQuestion(text string) bool {
	return strings.HasSuffix(text, "？") || strings.HasSuffix(text, "?")
}

// MemoryWriteMeta is the metadata attached to a turn written back to memorus.
func MemoryWriteMeta(tenantID uuid.UUID) map[string]any {
	return map[string]any{"source": "tally-ai", "tally_tenant_id": tenantID.String()}
}

// AsyncWriteMemory fires a goroutine to write a memory summary to memorus.
// It is a no-op when mc is nil or there is nothing to remember. Panics in the
// goroutine are recovered silently so a memorus failure can never crash the
// server.
func AsyncWriteMemory(mc MemoryClient, userID string, summary string, meta map[string]any) {
	if mc == nil || summary == "" {
		return
	}
	go func() {
		defer func() { _ = recover() }()
		// Detach from the request lifecycle (which ends when the HTTP response is
		// sent) but bound the write so a hung memorus cannot leak the goroutine.
		ctx, cancel := context.WithTimeout(context.Background(), asyncMemoryWriteTimeout)
		defer cancel()
		_, _ = mc.Add(ctx, userID, summary, meta)
	}()
}
