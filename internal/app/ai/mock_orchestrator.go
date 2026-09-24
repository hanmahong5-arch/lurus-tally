package ai

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	domainai "github.com/hanmahong5-arch/lurus-tally/internal/domain/ai"
)

// MockOrchestrator is a stand-in used when NEWAPI_KEY is unset.
// It returns a canned, slowly-streamed reply so the AI Drawer UI is testable
// in environments without an LLM provider configured.
//
// Streaming pacing is deterministic so e2e tests get predictable timing,
// but small enough to not block requests for noticeable durations.
type MockOrchestrator struct {
	// reply is the full canned reply text streamed back chunk by chunk.
	reply string
	// chunkDelay is the pause between simulated chunks.
	chunkDelay time.Duration
}

// NewMockOrchestrator constructs the mock with the default disabled-message reply.
func NewMockOrchestrator() *MockOrchestrator {
	return &MockOrchestrator{
		reply:      "AI 服务未配置，请联系管理员。",
		chunkDelay: 30 * time.Millisecond,
	}
}

// StreamChat satisfies the handler ChatOrchestrator interface.
// It splits the canned reply into rune-sized chunks to mimic typewriter output.
func (m *MockOrchestrator) StreamChat(ctx context.Context, in ChatInput, onChunk func(string)) (*ChatOutput, error) {
	// Tokenise on rune boundaries so multi-byte CJK characters render cleanly.
	for _, r := range m.reply {
		select {
		case <-ctx.Done():
			return &ChatOutput{AssistantText: m.reply}, ctx.Err()
		case <-time.After(m.chunkDelay):
		}
		onChunk(string(r))
	}
	// Reference unused fields to keep the lint quiet and document the contract.
	_ = in.TenantID
	_ = strings.TrimSpace(in.UserMessage)
	return &ChatOutput{AssistantText: m.reply}, nil
}

// ConfirmPlan is a no-op for the mock — there are never any plans to confirm.
func (m *MockOrchestrator) ConfirmPlan(_ context.Context, _, _ uuid.UUID) (*domainai.Plan, error) {
	return nil, ErrPlanNotFound
}

// CancelPlan is a no-op for the mock.
func (m *MockOrchestrator) CancelPlan(_ context.Context, _, _ uuid.UUID) error {
	return ErrPlanNotFound
}
